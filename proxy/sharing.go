package proxy

import (
	"context"
	"log/slog"
	"time"

	"github.com/flashbots/go-utils/rpcclient"
	"github.com/flashbots/go-utils/signature"
)

var (
	ShareWorkerQueueSize = 10000
	requestTimeout       = time.Second * 10
)

type ShareQueue struct {
	name         string
	log          *slog.Logger
	queue        chan *ParsedRequest
	updatePeers  chan []ConfighubBuilder
	localBuilder rpcclient.RPCClient
	signer       *signature.Signer
	// if > 0 share queue will spawn multiple senders per peer
	workersPerPeer int
}

type shareQueuePeer struct {
	ch     chan *ParsedRequest
	name   string
	client rpcclient.RPCClient
}

func newShareQueuePeer(name string, client rpcclient.RPCClient) shareQueuePeer {
	return shareQueuePeer{
		ch:     make(chan *ParsedRequest, ShareWorkerQueueSize),
		name:   name,
		client: client,
	}
}

func (p *shareQueuePeer) Close() {
	close(p.ch)
}

func (p *shareQueuePeer) SendRequest(log *slog.Logger, request *ParsedRequest) {
	select {
	case p.ch <- request:
	default:
		log.Error("Peer is stalling on requests", slog.String("peer", p.name))
		incShareQueuePeerStallingErrors(p.name)
	}
}

func (sq *ShareQueue) Run() {
	workersPerPeer := 1
	if sq.workersPerPeer > 0 {
		workersPerPeer = sq.workersPerPeer
	}
	var (
		localBuilder *shareQueuePeer
		peers        []shareQueuePeer
	)
	if sq.localBuilder != nil {
		builderPeer := newShareQueuePeer("local-builder", sq.localBuilder)
		localBuilder = &builderPeer
		for worker := range workersPerPeer {
			go sq.proxyRequests(localBuilder, worker)
		}
		defer localBuilder.Close()
	}
	for {
		select {
		case req, more := <-sq.queue:
			sq.log.Debug("Share queue received a request", slog.String("name", sq.name), slog.String("method", req.method))
			if !more {
				sq.log.Info("Share queue closing, queue channel closed")
				return
			}
			if localBuilder != nil {
				localBuilder.SendRequest(sq.log, req)
			}
			if !req.publicEndpoint {
				for _, peer := range peers {
					peer.SendRequest(sq.log, req)
				}
			}
		case newPeers, more := <-sq.updatePeers:
			if !more {
				sq.log.Info("Share queue closing, peer channel closed")
				return
			}
			for _, peer := range peers {
				peer.Close()
			}
			peers = nil
			for _, info := range newPeers {
				// don't send to yourself
				if info.OrderflowProxy.EcdsaPubkeyAddress == sq.signer.Address() {
					continue
				}
				client, err := RPCClientWithCertAndSigner(OrderflowProxyURLFromIP(info.IP), []byte(info.OrderflowProxy.TLSCert), sq.signer, workersPerPeer)
				if err != nil {
					sq.log.Error("Failed to create a peer client", slog.Any("error", err))
					shareQueueInternalErrors.Inc()
					continue
				}
				sq.log.Info("Created client for peer", slog.String("peer", info.Name), slog.String("name", sq.name))
				newPeer := newShareQueuePeer(info.Name, client)
				peers = append(peers, newPeer)
				for worker := range workersPerPeer {
					go sq.proxyRequests(&newPeer, worker)
				}
			}
		}
	}
}

func (sq *ShareQueue) proxyRequests(peer *shareQueuePeer, worker int) {
	proxiedRequestCount := 0
	logger := sq.log.With(slog.String("peer", peer.name), slog.String("name", sq.name), slog.Int("worker", worker))
	logger.Info("Started proxying requests to peer")
	defer func() {
		logger.Info("Stopped proxying requets to peer", slog.Int("proxiedRequestCount", proxiedRequestCount))
	}()
	for {
		req, more := <-peer.ch
		if !more {
			return
		}
		var (
			method string
			data   any
		)
		if req.ethSendBundle != nil {
			method = EthSendBundleMethod
			data = req.ethSendBundle
		} else if req.mevSendBundle != nil {
			method = MevSendBundleMethod
			data = req.mevSendBundle
		} else if req.ethCancelBundle != nil {
			method = EthCancelBundleMethod
			data = req.ethCancelBundle
		} else if req.ethSendRawTransaction != nil {
			method = EthSendRawTransactionMethod
			data = req.ethSendRawTransaction
		} else if req.bidSubsidiseBlock != nil {
			method = BidSubsidiseBlockMethod
			data = req.bidSubsidiseBlock
		} else {
			logger.Error("Unknown request type", slog.String("method", req.method))
			shareQueueInternalErrors.Inc()
			continue
		}
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		resp, err := peer.client.Call(ctx, method, data)
		cancel()
		timeShareQueuePeerRPCDuration(peer.name, time.Since(start).Milliseconds())
		if err != nil {
			logger.Debug("Error while proxying request", slog.Any("error", err))
			incShareQueuePeerRPCErrors(peer.name)
		}
		if resp != nil && resp.Error != nil {
			logger.Debug("Error returned from target while proxying", slog.Any("error", resp.Error))
			incShareQueuePeerRPCErrors(peer.name)
		}
		proxiedRequestCount += 1
		logger.Debug("Message proxied")
	}
}
