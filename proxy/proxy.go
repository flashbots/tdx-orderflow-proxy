package proxy

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/flashbots/orderflow-proxy/metrics"
)

type Config struct {
	Log *slog.Logger

	UsersListenAddr   string
	NetworkListenAddr string
	CertListenAddr    string
	BuilderEndpoint   string

	CertValidDuration time.Duration
	CertHosts         []string

	BuilderConfigHub BuilderConfigHub
}

type Proxy struct {
	Config Config

	log                *slog.Logger
	orderflowSignerKey *ecdsa.PrivateKey

	publicCertPEM []byte
	certificate   *tls.Certificate
}

func New(config Config) (*Proxy, error) {
	return &Proxy{
		Config:      config,
		log:         config.Log,
		certificate: nil,
	}, nil
}

func (prx *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/cert" {
		w.Header().Add("Content-Type", "application/octet-stream")
		w.Write(prx.publicCertPEM) //nolint: errcheck
		return
	}

	metrics.IncRequestsReceived()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		prx.log.Error("Failed to read request body", "err", err)
		return
	}
	client := &http.Client{
		Timeout: 1 * time.Second,
	}

	req, err := http.NewRequest(http.MethodPost, prx.Config.BuilderEndpoint, bytes.NewBuffer(body))
	if err != nil {
		prx.log.Error("Failed to create a req to the local builder", "err", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		prx.log.Error("Failed to make a req to the local builder", "err", err)
		return
	}
	defer resp.Body.Close()
	prx.log.Info("Request proxied")
	w.WriteHeader(http.StatusOK)
}

func (prx *Proxy) GenerateAndPublish() error {
	cert, key, err := GenerateCert(prx.Config.CertValidDuration, prx.Config.CertHosts)
	if err != nil {
		return err
	}
	prx.publicCertPEM = cert

	orderflowSignerKey, err := crypto.GenerateKey()
	if err != nil {
		return err
	}
	prx.orderflowSignerKey = orderflowSignerKey
	orderflowSigner := crypto.PubkeyToAddress(orderflowSignerKey.PublicKey)

	prx.log.Info("Generated ordeflow signer", "address", orderflowSigner)

	selfInfo := BuilderInfo{
		Cert:            cert,
		OrderflowSigner: orderflowSigner,
	}

	err = prx.Config.BuilderConfigHub.PublishConfig(selfInfo)
	if err != nil {
		return err
	}

	certificate, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return err
	}
	prx.certificate = &certificate
	return nil
}

func (prx *Proxy) StartServersInBackground() error {
	// user server
	srvUsers := &http.Server{
		Addr:    prx.Config.UsersListenAddr,
		Handler: prx,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{*prx.certificate},
			MinVersion:   tls.VersionTLS13,
		},
		ReadHeaderTimeout: 2 * time.Second,
	}
	go func() {
		prx.log.Info("Starting orderflow users input", "addr", srvUsers.Addr)
		if err := srvUsers.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			prx.log.Error("Orderflow proxy users input  failed", "err", err)
		}
	}()

	// network server
	srvNetwork := &http.Server{
		Addr:    prx.Config.NetworkListenAddr,
		Handler: prx,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{*prx.certificate},
			MinVersion:   tls.VersionTLS13,
		},
		ReadHeaderTimeout: 2 * time.Second,
	}
	go func() {
		prx.log.Info("Starting orderflow network input", "addr", srvNetwork.Addr)
		if err := srvNetwork.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			prx.log.Error("Orderflow proxy network input failed", "err", err)
		}
	}()

	// cert server
	srvCert := &http.Server{
		Addr:              prx.Config.CertListenAddr,
		Handler:           prx,
		ReadHeaderTimeout: 2 * time.Second,
	}
	go func() {
		prx.log.Info("Starting cert server", "addr", srvCert.Addr)
		if err := srvCert.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			prx.log.Error("Orderflow proxy cert serving failed", "err", err)
		}
	}()

	return nil
}
