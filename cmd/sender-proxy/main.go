package main

import (
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/VictoriaMetrics/metrics"
	"github.com/flashbots/go-utils/signature"
	"github.com/flashbots/tdx-orderflow-proxy/common"
	"github.com/flashbots/tdx-orderflow-proxy/proxy"
	"github.com/google/uuid"
	"github.com/urfave/cli/v2" // imports as package "cli"
)

var flags []cli.Flag = []cli.Flag{
	// input and output
	&cli.StringFlag{
		Name:    "listen-address",
		Value:   "127.0.0.1:8080",
		Usage:   "address to listen on for requests",
		EnvVars: []string{"LISTEN_ADDRESS"},
	},
	&cli.StringFlag{
		Name:    "builder-confighub-endpoint",
		Value:   "http://127.0.0.1:14892",
		Usage:   "address of the builder config hub endpoint (directly or using the cvm-proxy)",
		EnvVars: []string{"BUILDER_CONFIGHUB_ENDPOINT"},
	},
	&cli.StringFlag{
		Name:    "orderflow-signer-key",
		Value:   "0xfb5ad18432422a84514f71d63b45edf51165d33bef9c2bd60957a48d4c4cb68e",
		Usage:   "orderflow will be signed with this address",
		EnvVars: []string{"ORDERFLOW_SIGNER_KEY"},
	},
	&cli.Int64Flag{
		Name:    "max-request-body-size-bytes",
		Value:   0,
		Usage:   "Maximum size of the request body, if 0 default will be used",
		EnvVars: []string{"MAX_REQUEST_BODY_SIZE_BYTES"},
	},
	&cli.IntFlag{
		Name:    "connections-per-peer",
		Value:   10,
		Usage:   "Number of parallel connections for each peer",
		EnvVars: []string{"CONN_PER_PEER"},
	},

	// logging, metrics and debug
	&cli.StringFlag{
		Name:    "metrics-addr",
		Value:   "127.0.0.1:8090",
		Usage:   "address to listen on for Prometheus metrics (metrics are served on $metrics-addr/metrics)",
		EnvVars: []string{"METRICS_ADDR"},
	},
	&cli.BoolFlag{
		Name:    "log-json",
		Value:   false,
		Usage:   "log in JSON format",
		EnvVars: []string{"LOG_JSON"},
	},
	&cli.BoolFlag{
		Name:    "log-debug",
		Value:   false,
		Usage:   "log debug messages",
		EnvVars: []string{"LOG_DEBUG"},
	},
	&cli.BoolFlag{
		Name:    "log-uid",
		Value:   false,
		Usage:   "generate a uuid and add to all log messages",
		EnvVars: []string{"LOG_UID"},
	},
	&cli.StringFlag{
		Name:    "log-service",
		Value:   "tdx-orderflow-proxy-sender",
		Usage:   "add 'service' tag to logs",
		EnvVars: []string{"LOG_SERVICE"},
	},
	&cli.BoolFlag{
		Name:    "pprof",
		Value:   false,
		Usage:   "enable pprof debug endpoint (pprof is served on $metrics-addr/debug/pprof/*)",
		EnvVars: []string{"PPROF"},
	},
}

func main() {
	app := &cli.App{
		Name:  "sender-proxy",
		Usage: "Serve API, and metrics",
		Flags: flags,
		Action: func(cCtx *cli.Context) error {
			logJSON := cCtx.Bool("log-json")
			logDebug := cCtx.Bool("log-debug")
			logUID := cCtx.Bool("log-uid")
			logService := cCtx.String("log-service")

			log := common.SetupLogger(&common.LoggingOpts{
				Debug:   logDebug,
				JSON:    logJSON,
				Service: logService,
				Version: common.Version,
			})

			if logUID {
				id := uuid.Must(uuid.NewRandom())
				log = log.With("uid", id.String())
			}

			exit := make(chan os.Signal, 1)
			signal.Notify(exit, os.Interrupt, syscall.SIGTERM)

			builderConfigHubEndpoint := cCtx.String("builder-confighub-endpoint")
			orderflowSignerKeyStr := cCtx.String("orderflow-signer-key")
			orderflowSigner, err := signature.NewSignerFromHexPrivateKey(orderflowSignerKeyStr)
			if err != nil {
				log.Error("Failed to get signer from private key", "error", err)
			}
			log.Info("Ordeflow signing address", "address", orderflowSigner.Address())
			maxRequestBodySizeBytes := cCtx.Int64("max-request-body-size-bytes")

			connectionsPerPeer := cCtx.Int("connections-per-peer")

			proxyConfig := &proxy.SenderProxyConfig{
				SenderProxyConstantConfig: proxy.SenderProxyConstantConfig{
					Log:             log,
					OrderflowSigner: orderflowSigner,
				},
				BuilderConfigHubEndpoint: builderConfigHubEndpoint,
				MaxRequestBodySizeBytes:  maxRequestBodySizeBytes,
				ConnectionsPerPeer:       connectionsPerPeer,
			}

			instance, err := proxy.NewSenderProxy(*proxyConfig)
			if err != nil {
				log.Error("Failed to create proxy server", "err", err)
				return err
			}

			listenAddr := cCtx.String("listen-address")
			servers, err := proxy.StartSenderServers(instance, listenAddr)
			if err != nil {
				log.Error("Failed to start proxy server", "err", err)
				return err
			}

			log.Info("Started sender proxy", "listenAddres", listenAddr)

			// metrics server
			go func() {
				metricsAddr := cCtx.String("metrics-addr")
				usePprof := cCtx.Bool("pprof")
				metricsMux := http.NewServeMux()
				metricsMux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
					metrics.WritePrometheus(w, true)
				})
				metricsMux.HandleFunc("/update_peers", func(w http.ResponseWriter, r *http.Request) {
					select {
					case instance.PeerUpdateForce <- struct{}{}:
					default:
					}
					w.WriteHeader(http.StatusOK)
				})
				if usePprof {
					metricsMux.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))
					metricsMux.Handle("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
					metricsMux.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
					metricsMux.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
					metricsMux.Handle("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
				}

				metricsServer := &http.Server{
					Addr:              metricsAddr,
					ReadHeaderTimeout: 5 * time.Second,
					Handler:           metricsMux,
				}

				err := metricsServer.ListenAndServe()
				if err != nil {
					log.Error("Failed to start metrics server", "err", err)
				}
			}()

			<-exit
			servers.Stop()
			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
