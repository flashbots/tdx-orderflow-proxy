package proxy

import (
	"errors"
	"log/slog"
	"net/http"
	"time"
)

var (
	HTTPDefaultReadTimeout  = 60 * time.Second
	HTTPDefaultWriteTimeout = 30 * time.Second
)

type ReceiverProxyServers struct {
	proxy        *ReceiverProxy
	publicServer *http.Server
	localServer  *http.Server
	certServer   *http.Server
}

func StartReceiverServers(proxy *ReceiverProxy, publicListenAddress, localListenAddress, certListenAddress string) (*ReceiverProxyServers, error) {
	publicServer := &http.Server{
		Addr:         publicListenAddress,
		Handler:      proxy.PublicHandler,
		TLSConfig:    proxy.TLSConfig(),
		ReadTimeout:  HTTPDefaultReadTimeout,
		WriteTimeout: HTTPDefaultWriteTimeout,
	}
	localServer := &http.Server{
		Addr:         localListenAddress,
		Handler:      proxy.LocalHandler,
		TLSConfig:    proxy.TLSConfig(),
		ReadTimeout:  HTTPDefaultReadTimeout,
		WriteTimeout: HTTPDefaultWriteTimeout,
	}
	certServer := &http.Server{
		Addr:         certListenAddress,
		Handler:      proxy.CertHandler,
		ReadTimeout:  HTTPDefaultReadTimeout,
		WriteTimeout: HTTPDefaultWriteTimeout,
	}

	errCh := make(chan error)

	go func() {
		if err := publicServer.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			err = errors.Join(errors.New("public HTTP server failed"), err)
			errCh <- err
		}
	}()
	go func() {
		if err := localServer.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			err = errors.Join(errors.New("local HTTP server failed"), err)
			errCh <- err
		}
	}()
	go func() {
		if err := certServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			err = errors.Join(errors.New("cert HTTP server failed"), err)
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return nil, err
	case <-time.After(time.Millisecond * 100):
	}

	go func() {
		for {
			err, more := <-errCh
			if !more {
				return
			}
			proxy.Log.Error("Error in HTTP server", slog.Any("error", err))
		}
	}()

	return &ReceiverProxyServers{
		proxy:        proxy,
		publicServer: publicServer,
		localServer:  localServer,
		certServer:   certServer,
	}, nil
}

func (s *ReceiverProxyServers) Stop() {
	_ = s.publicServer.Close()
	_ = s.localServer.Close()
	_ = s.certServer.Close()
	s.proxy.Stop()
}

type SenderProxyServers struct {
	proxy  *SenderProxy
	server *http.Server
}

func StartSenderServers(proxy *SenderProxy, listenAddress string) (*SenderProxyServers, error) {
	server := &http.Server{
		Addr:         listenAddress,
		Handler:      proxy.Handler,
		ReadTimeout:  HTTPDefaultReadTimeout,
		WriteTimeout: HTTPDefaultWriteTimeout,
	}

	errCh := make(chan error)

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			err = errors.Join(errors.New("HTTP server failed"), err)
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return nil, err
	case <-time.After(time.Millisecond * 100):
	}

	go func() {
		for {
			err, more := <-errCh
			if !more {
				return
			}
			proxy.Log.Error("Error in HTTP server", slog.Any("error", err))
		}
	}()

	return &SenderProxyServers{
		proxy:  proxy,
		server: server,
	}, nil
}

func (s *SenderProxyServers) Stop() {
	_ = s.server.Close()
	s.proxy.Stop()
}