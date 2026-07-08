package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prometheus metrics exported by the agent and scraped from the /metrics
// endpoint. The default registry also exposes go_* and process_* collectors
// automatically, which the Grafana dashboards rely on.
var (
	// K8sAPIDuration records the latency of Kubernetes operations the agent
	// performs in response to gateway commands, labelled by operation and
	// terminal status ("success" or "error").
	K8sAPIDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "k8s_api_duration_seconds",
		Help:    "Latency of Kubernetes operations handled by the agent.",
		Buckets: prometheus.DefBuckets,
	}, []string{"operation", "status"})

	// GatewayConnected is 1 while the agent holds an active gateway WebSocket
	// connection and 0 otherwise.
	GatewayConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gateway_connected",
		Help: "1 when the agent has an active gateway WebSocket connection, 0 otherwise.",
	})

	// GatewayReconnects counts how often the gateway connection dropped and the
	// agent had to reconnect.
	GatewayReconnects = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gateway_reconnects_total",
		Help: "Total number of gateway WebSocket reconnects.",
	})
)

// NewMetricsServer builds an HTTP server exposing Prometheus metrics on
// /metrics (and a /healthz liveness probe) at addr.
func NewMetricsServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return &http.Server{Addr: addr, Handler: mux}
}
