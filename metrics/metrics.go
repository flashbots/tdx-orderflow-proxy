// Package metrics provides a way to collect and expose metrics for the application.
package metrics

import "github.com/VictoriaMetrics/metrics"

var totalRequestReceived = metrics.NewCounter("requests_total")

func IncRequestsReceived() {
	totalRequestReceived.Inc()
}
