package proxy

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/flashbots/go-utils/rpcserver"
	"github.com/flashbots/go-utils/rpctypes"
	"github.com/google/uuid"
)

const maxRequestBodySizeBytes = 30 * 1024 * 1024 // 30 MB, TODO: configurable

const (
	FlashbotsPeerName = "flashbots"

	EthSendBundleMethod         = "eth_sendBundle"
	MevSendBundleMethod         = "mev_sendBundle"
	EthCancelBundleMethod       = "eth_cancelBundle"
	EthSendRawTransactionMethod = "eth_sendRawTransaction"
	BidSubsidiseBlockMethod     = "bid_subsidiseBlock"
)

var (
	errUnknownPeer          = errors.New("unknown peers can't send to the public address")
	errSubsidyWrongEndpoint = errors.New("subsidy can only be called on public method")
	errSubsidyWrongCaller   = errors.New("subsidy can only be called by Flashbots")

	apiNow = time.Now
)

func (prx *Proxy) PublicJSONRPCHandler() (*rpcserver.JSONRPCHandler, error) {
	handler, err := rpcserver.NewJSONRPCHandler(rpcserver.Methods{
		EthSendBundleMethod:         prx.EthSendBundlePublic,
		MevSendBundleMethod:         prx.MevSendBundlePublic,
		EthCancelBundleMethod:       prx.EthCancelBundlePublic,
		EthSendRawTransactionMethod: prx.EthSendRawTransactionPublic,
		BidSubsidiseBlockMethod:     prx.BidSubsidiseBlockPublic,
	},
		rpcserver.JSONRPCHandlerOpts{
			ServerName:                       "public_server",
			Log:                              prx.Log,
			MaxRequestBodySizeBytes:          maxRequestBodySizeBytes,
			VerifyRequestSignatureFromHeader: true,
		},
	)

	return handler, err
}

func (prx *Proxy) LocalJSONRPCHandler() (*rpcserver.JSONRPCHandler, error) {
	handler, err := rpcserver.NewJSONRPCHandler(rpcserver.Methods{
		EthSendBundleMethod:         prx.EthSendBundleLocal,
		MevSendBundleMethod:         prx.MevSendBundleLocal,
		EthCancelBundleMethod:       prx.EthCancelBundleLocal,
		EthSendRawTransactionMethod: prx.EthSendRawTransactionLocal,
		BidSubsidiseBlockMethod:     prx.BidSubsidiseBlockLocal,
	},
		rpcserver.JSONRPCHandlerOpts{
			ServerName:                       "local_server",
			Log:                              prx.Log,
			MaxRequestBodySizeBytes:          maxRequestBodySizeBytes,
			VerifyRequestSignatureFromHeader: true,
		},
	)

	return handler, err
}

func (prx *Proxy) ValidateSigner(ctx context.Context, req *ParsedRequest, publicEndpoint bool) error {
	req.signer = rpcserver.GetSigner(ctx)
	if !publicEndpoint {
		return nil
	}

	if req.signer == prx.FlashbotsSignerAddress {
		req.peerName = FlashbotsPeerName
		return nil
	}

	prx.peersMu.RLock()
	found := false
	peerName := ""
	for _, peer := range prx.lastFetchedPeers {
		if req.signer == peer.OrderflowProxy.EcdsaPubkeyAddress {
			found = true
			peerName = peer.Name
			break
		}
	}
	if !found {
		return errUnknownPeer
	}
	prx.peersMu.RUnlock()
	req.peerName = peerName
	return nil
}

func (prx *Proxy) EthSendBundle(ctx context.Context, ethSendBundle rpctypes.EthSendBundleArgs, publicEndpoint bool) error {
	parsedRequest := ParsedRequest{
		publicEndpoint: publicEndpoint,
		ethSendBundle:  &ethSendBundle,
		method:         EthSendBundleMethod,
	}

	err := prx.ValidateSigner(ctx, &parsedRequest, publicEndpoint)
	if err != nil {
		return err
	}

	err = ValidateEthSendBundle(&ethSendBundle, publicEndpoint)
	if err != nil {
		return err
	}

	if !publicEndpoint {
		ethSendBundle.SigningAddress = &parsedRequest.signer
	}

	uniqueKey := ethSendBundle.UniqueKey()
	parsedRequest.requestArgUniqueKey = &uniqueKey

	return prx.HandleParsedRequest(ctx, parsedRequest)
}

func (prx *Proxy) EthSendBundlePublic(ctx context.Context, ethSendBundle rpctypes.EthSendBundleArgs) error {
	return prx.EthSendBundle(ctx, ethSendBundle, true)
}

func (prx *Proxy) EthSendBundleLocal(ctx context.Context, ethSendBundle rpctypes.EthSendBundleArgs) error {
	return prx.EthSendBundle(ctx, ethSendBundle, false)
}

func (prx *Proxy) MevSendBundle(ctx context.Context, mevSendBundle rpctypes.MevSendBundleArgs, publicEndpoint bool) error {
	parsedRequest := ParsedRequest{
		publicEndpoint: publicEndpoint,
		mevSendBundle:  &mevSendBundle,
		method:         MevSendBundleMethod,
	}

	err := prx.ValidateSigner(ctx, &parsedRequest, publicEndpoint)
	if err != nil {
		return err
	}

	// TODO: make sure that cancellations are handled by the builder properly
	err = ValidateMevSendBundle(&mevSendBundle, publicEndpoint)
	if err != nil {
		return err
	}

	if !publicEndpoint {
		mevSendBundle.Metadata = &rpctypes.MevBundleMetadata{
			Signer: &parsedRequest.signer,
		}
	}

	uniqueKey := mevSendBundle.UniqueKey()
	parsedRequest.requestArgUniqueKey = &uniqueKey

	return prx.HandleParsedRequest(ctx, parsedRequest)
}

func (prx *Proxy) MevSendBundlePublic(ctx context.Context, mevSendBundle rpctypes.MevSendBundleArgs) error {
	return prx.MevSendBundle(ctx, mevSendBundle, true)
}

func (prx *Proxy) MevSendBundleLocal(ctx context.Context, mevSendBundle rpctypes.MevSendBundleArgs) error {
	return prx.MevSendBundle(ctx, mevSendBundle, false)
}

func (prx *Proxy) EthCancelBundle(ctx context.Context, ethCancelBundle rpctypes.EthCancelBundleArgs, publicEndpoint bool) error {
	parsedRequest := ParsedRequest{
		publicEndpoint:  publicEndpoint,
		ethCancelBundle: &ethCancelBundle,
		method:          EthCancelBundleMethod,
	}

	err := prx.ValidateSigner(ctx, &parsedRequest, publicEndpoint)
	if err != nil {
		return err
	}

	err = ValidateEthCancelBundle(&ethCancelBundle, publicEndpoint)
	if err != nil {
		return err
	}

	if !publicEndpoint {
		ethCancelBundle.SigningAddress = &parsedRequest.signer
	}
	return prx.HandleParsedRequest(ctx, parsedRequest)
}

func (prx *Proxy) EthCancelBundlePublic(ctx context.Context, ethCancelBundle rpctypes.EthCancelBundleArgs) error {
	return prx.EthCancelBundle(ctx, ethCancelBundle, true)
}

func (prx *Proxy) EthCancelBundleLocal(ctx context.Context, ethCancelBundle rpctypes.EthCancelBundleArgs) error {
	return prx.EthCancelBundle(ctx, ethCancelBundle, false)
}

func (prx *Proxy) EthSendRawTransaction(ctx context.Context, ethSendRawTransaction rpctypes.EthSendRawTransactionArgs, publicEndpoint bool) error {
	parsedRequest := ParsedRequest{
		publicEndpoint:        publicEndpoint,
		ethSendRawTransaction: &ethSendRawTransaction,
		method:                EthSendRawTransactionMethod,
	}
	err := prx.ValidateSigner(ctx, &parsedRequest, publicEndpoint)
	if err != nil {
		return err
	}

	uniqueKey := ethSendRawTransaction.UniqueKey()
	parsedRequest.requestArgUniqueKey = &uniqueKey

	return prx.HandleParsedRequest(ctx, parsedRequest)
}

func (prx *Proxy) EthSendRawTransactionPublic(ctx context.Context, ethSendRawTransaction rpctypes.EthSendRawTransactionArgs) error {
	return prx.EthSendRawTransaction(ctx, ethSendRawTransaction, true)
}

func (prx *Proxy) EthSendRawTransactionLocal(ctx context.Context, ethSendRawTransaction rpctypes.EthSendRawTransactionArgs) error {
	return prx.EthSendRawTransaction(ctx, ethSendRawTransaction, false)
}

func (prx *Proxy) BidSubsidiseBlock(ctx context.Context, bidSubsidiseBlock rpctypes.BidSubsisideBlockArgs, publicEndpoint bool) error {
	if !publicEndpoint {
		return errSubsidyWrongEndpoint
	}

	parsedRequest := ParsedRequest{
		publicEndpoint:    publicEndpoint,
		bidSubsidiseBlock: &bidSubsidiseBlock,
		method:            BidSubsidiseBlockMethod,
	}

	err := prx.ValidateSigner(ctx, &parsedRequest, publicEndpoint)
	if err != nil {
		return err
	}

	if parsedRequest.signer != prx.FlashbotsSignerAddress {
		return errSubsidyWrongCaller
	}

	uniqueKey := bidSubsidiseBlock.UniqueKey()
	parsedRequest.requestArgUniqueKey = &uniqueKey

	return prx.HandleParsedRequest(ctx, parsedRequest)
}

func (prx *Proxy) BidSubsidiseBlockPublic(ctx context.Context, bidSubsidiseBlock rpctypes.BidSubsisideBlockArgs) error {
	return prx.BidSubsidiseBlock(ctx, bidSubsidiseBlock, true)
}

func (prx *Proxy) BidSubsidiseBlockLocal(ctx context.Context, bidSubsidiseBlock rpctypes.BidSubsisideBlockArgs) error {
	return prx.BidSubsidiseBlock(ctx, bidSubsidiseBlock, false)
}

type ParsedRequest struct {
	publicEndpoint        bool
	signer                common.Address
	method                string
	peerName              string
	receivedAt            time.Time
	requestArgUniqueKey   *uuid.UUID
	ethSendBundle         *rpctypes.EthSendBundleArgs
	mevSendBundle         *rpctypes.MevSendBundleArgs
	ethCancelBundle       *rpctypes.EthCancelBundleArgs
	ethSendRawTransaction *rpctypes.EthSendRawTransactionArgs
	bidSubsidiseBlock     *rpctypes.BidSubsisideBlockArgs
}

func (prx *Proxy) HandleParsedRequest(ctx context.Context, parsedRequest ParsedRequest) error {
	parsedRequest.receivedAt = apiNow()
	prx.Log.Info("Received request", slog.Bool("isPublicEndpoint", parsedRequest.publicEndpoint), slog.String("method", parsedRequest.method))
	if parsedRequest.publicEndpoint {
		incAPIIncomingRequestsByPeer(parsedRequest.peerName)
	}
	if parsedRequest.requestArgUniqueKey != nil {
		if prx.requestUniqueKeysRLU.Contains(*parsedRequest.requestArgUniqueKey) {
			incAPIDuplicateRequestsByPeer(parsedRequest.peerName)
			return nil
		}
		prx.requestUniqueKeysRLU.Add(*parsedRequest.requestArgUniqueKey, struct{}{})
	}
	select {
	case <-ctx.Done():
	case prx.shareQueue <- &parsedRequest:
	}
	if !parsedRequest.publicEndpoint {
		select {
		case <-ctx.Done():
		case prx.archiveQueue <- &parsedRequest:
		}
	}
	return nil
}
