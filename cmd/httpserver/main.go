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
	"github.com/flashbots/orderflow-proxy/common"
	"github.com/flashbots/orderflow-proxy/proxy"
	"github.com/google/uuid"
	"github.com/urfave/cli/v2" // imports as package "cli"
)

var flags []cli.Flag = []cli.Flag{
	&cli.StringFlag{
		Name:  "metrics-addr",
		Value: "127.0.0.1:8090",
		Usage: "address to listen on for Prometheus metrics (metrics are served on $metrics-addr/metrics)",
	},
	&cli.BoolFlag{
		Name:  "log-json",
		Value: false,
		Usage: "log in JSON format",
	},
	&cli.BoolFlag{
		Name:  "log-debug",
		Value: false,
		Usage: "log debug messages",
	},
	&cli.BoolFlag{
		Name:  "log-uid",
		Value: false,
		Usage: "generate a uuid and add to all log messages",
	},
	&cli.StringFlag{
		Name:  "log-service",
		Value: "your-project",
		Usage: "add 'service' tag to logs",
	},
	&cli.BoolFlag{
		Name:  "pprof",
		Value: false,
		Usage: "enable pprof debug endpoint (pprof is served on $metrics-addr/debug/pprof/*)",
	},
	&cli.StringFlag{
		Name:  "listen-addr",
		Value: "127.0.0.1:9090",
		Usage: "address to listen on for orderflow proxy API",
	},
	&cli.StringFlag{
		Name:  "external-addr",
		Value: "127.0.0.1",
		Usage: "address of this service reachable from outside",
	},
	&cli.StringFlag{
		Name:  "builder-endpoint",
		Value: "127.0.0.1:8546",
		Usage: "address to send local ordeflow to",
	},
	&cli.DurationFlag{
		Name:  "cert-duration",
		Value: time.Hour * 24 * 365,
		Usage: "generated certificate duration",
	},
	&cli.StringSliceFlag{
		Name:  "cert-hosts",
		Value: cli.NewStringSlice("127.0.0.1", "localhost"),
		Usage: "generated certificate hosts",
	},
}

func main() {
	app := &cli.App{
		Name:  "orderflow-proxy",
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

			// metrics server
			go func() {
				metricsAddr := cCtx.String("metrics-addr")
				usePprof := cCtx.Bool("pprof")
				metricsMux := http.NewServeMux()
				metricsMux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
					metrics.WritePrometheus(w, true)
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

			builderEndpoint := cCtx.String("builder-endpoint")
			listedAddr := cCtx.String("listen-addr")
			certDuration := cCtx.Duration("cert-duration")
			certHosts := cCtx.StringSlice("cert-hosts")
			externalAddr := cCtx.String("external-addr")
			proxyConfig := &proxy.Config{
				Log:               log,
				BuilderEndpoint:   builderEndpoint,
				ListenAddr:        listedAddr,
				CertValidDuration: certDuration,
				CertHosts:         certHosts,
				BuilderConfigHub:  proxy.MockBuilderConfigHub{},
				ExternalAddr:      externalAddr,
			}

			proxy, err := proxy.New(*proxyConfig)
			if err != nil {
				log.Error("failed to create proxy server", "err", err)
				return err
			}
			err = proxy.RunProxyInBackground()
			if err != nil {
				log.Error("failed to start proxy server", "err", err)
				return err
			}

			<-exit
			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
