# orderflow-proxy

[![Goreport status](https://goreportcard.com/badge/github.com/flashbots/tdx-orderflow-proxy)](https://goreportcard.com/report/github.com/flashbots/go-template)
[![Test status](https://github.com/flashbots/tdx-orderflow-proxy/actions/workflows/checks.yml/badge.svg?branch=main)](https://github.com/flashbots/go-template/actions?query=workflow%3A%22Checks%22)

## Getting started

**Build**

```bash
make build
```

## Run

`./build/orderflow-proxy`

This Will 

* generate SSL certificate
* create 2 input servers serving TLS with that certificate (user-listen-addr, network-listen-addr)
* create 1 local http server serving /cert  (cert-listen-addr)
* create metrics server (metrict-addr)
* proxy requests from (user and network listen addresses to the builder-endpoint)

Flags for the orderflow proxy

```
./build/orderflow-proxy -h
NAME:
   orderflow-proxy - Serve API, and metrics

USAGE:
   orderflow-proxy [global options] command [command options] 

COMMANDS:
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --users-listen-addr value                  address to listen on for orderflow proxy API for external users and local operator (default: "127.0.0.1:443")
   --network-listen-addr value                address to listen on for orderflow proxy API for other network participants (default: "127.0.0.1:5544")
   --cert-listen-addr value                   address to listen on for orderflow proxy serving its SSL certificate on /cert (default: "127.0.0.1:14727")
   --builder-endpoint value                   address to send local ordeflow to (default: "127.0.0.1:8645")
   --cert-duration value                      generated certificate duration (default: 8760h0m0s)
   --cert-hosts value [ --cert-hosts value ]  generated certificate hosts (default: "127.0.0.1", "localhost")
   --metrics-addr value                       address to listen on for Prometheus metrics (metrics are served on $metrics-addr/metrics) (default: "127.0.0.1:8090")
   --log-json                                 log in JSON format (default: false)
   --log-debug                                log debug messages (default: false)
   --log-uid                                  generate a uuid and add to all log messages (default: false)
   --log-service value                        add 'service' tag to logs (default: "your-project")
   --pprof                                    enable pprof debug endpoint (pprof is served on $metrics-addr/debug/pprof/*) (default: false)
   --help, -h                                 show help
```


## curl TLS example

1. Run orderflow proxy

```bash
make build 
./build/orderflow-proxy --users-listen-addr 127.0.0.1:6789 --network-listen-addr 127.0.0.1:6799 --cert-listen-addr 127.0.0.1:6889 --builder-endpoint http://127.0.0.1:8769
```

2. Extract self signed certificate 
```bash
#  using cert port
curl http://127.0.0.1:6889/cert > cacert.pem


# or using curl
# -k will tell curl to ignore the fact that cert is self signed
curl -w %{certs} -k https://127.0.0.1:6789 > cacert.pem


```
3. Make call using this certificate
```bash
curl https://127.0.0.1:6789 --cacert cacert.pem
```
