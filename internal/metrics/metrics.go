// Package metrics exposes Prometheus latency metrics for the agent.
//
// Labels are intentionally low-cardinality: only the bounded set of allowed
// action names is used for "operation". Namespace, pod, cluster and other
// unbounded identifiers must never become metric labels; they belong in traces.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// latencyBuckets are practical latency buckets shared across the services.
var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

var k8sAPIDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "k8s_api_duration_seconds",
		Help:    "Latency of Kubernetes API operations performed by the agent, in seconds.",
		Buckets: latencyBuckets,
	},
	[]string{"operation", "status"},
)

// gatewayConnected reports whether the agent currently holds an active WebSocket
// connection to the gateway (1) or not (0) — an availability signal.
var gatewayConnected = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "gateway_connected",
		Help: "Whether the agent currently has an active WebSocket connection to the gateway (1) or not (0).",
	},
)

// gatewayReconnectsTotal counts successful reconnections to the gateway,
// excluding the initial connection — a saturation/instability signal.
var gatewayReconnectsTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "gateway_reconnects_total",
		Help: "Total number of successful gateway reconnections (excludes the initial connect).",
	},
)

// cacheRequests counts in-agent cache lookups by result (hit/miss) — shows how
// effective the namespace cache is and whether its TTL is tuned well.
var cacheRequests = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "k8s_cache_requests_total",
		Help: "In-agent cache lookups by result.",
	},
	[]string{"result"},
)

func init() {
	prometheus.MustRegister(k8sAPIDuration, gatewayConnected, gatewayReconnectsTotal, cacheRequests)
}

// ObserveK8sAPI records the duration in seconds of a Kubernetes API operation.
// operation is the bounded action name (e.g. "k8s.list_pods"); status is
// "success" or "error".
func ObserveK8sAPI(operation, status string, seconds float64) {
	k8sAPIDuration.WithLabelValues(operation, status).Observe(seconds)
}

// SetGatewayConnected records whether the agent is currently connected to the gateway.
func SetGatewayConnected(connected bool) {
	if connected {
		gatewayConnected.Set(1)
		return
	}
	gatewayConnected.Set(0)
}

// IncGatewayReconnect increments the gateway reconnection counter.
func IncGatewayReconnect() {
	gatewayReconnectsTotal.Inc()
}

// IncCacheResult records a cache lookup outcome ("hit" or "miss").
func IncCacheResult(result string) {
	cacheRequests.WithLabelValues(result).Inc()
}

// Handler returns the Prometheus scrape handler for the default registry.
func Handler() http.Handler {
	return promhttp.Handler()
}
