package telemetry

import (
	"net/http"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HealthState separates process liveness from connection readiness. Readiness
// only becomes true after credentials are available and the gateway session is
// connected; a running process alone must not make a rollout successful.
type HealthState struct {
	credentialsReady atomic.Bool
	gatewayConnected atomic.Bool
}

func NewHealthState() *HealthState                        { return &HealthState{} }
func (s *HealthState) SetCredentialsReady(ready bool)     { s.credentialsReady.Store(ready) }
func (s *HealthState) SetGatewayConnected(connected bool) { s.gatewayConnected.Store(connected) }
func (s *HealthState) Ready() bool                        { return s.credentialsReady.Load() && s.gatewayConnected.Load() }

// Prometheus metrics exported by the agent and scraped from the /metrics
// endpoint. The default registry also exposes go_* and process_* collectors
// automatically, which the Grafana dashboards rely on.
var (
	PodCacheRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "incidentflow_k8s_agent_cache_requests_total",
		Help: "Namespace-scoped pod LIST requests handled by the enabled informer cache.",
	}, []string{"result"})
	PodAPIRequestsAvoided = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "incidentflow_k8s_agent_kubernetes_api_requests_avoided_total",
		Help: "Live pod LIST requests replaced by a cache hit, before accounting for informer traffic.",
	}, []string{"resource"})
	InformerSynced = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "incidentflow_k8s_agent_informer_synced",
		Help: "1 when the informer has synced and its watch is active.",
	}, []string{"resource", "watched_namespace"})
	InformerLastSync = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "incidentflow_k8s_agent_informer_last_sync_timestamp_seconds",
		Help: "Unix timestamp of the last successful LIST or WATCH bookmark.",
	}, []string{"resource", "watched_namespace"})
	InformerWatchReconnects = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "incidentflow_k8s_agent_informer_watch_reconnects_total",
		Help: "Informer WATCH attempts after the initial connection attempt.",
	}, []string{"resource", "watched_namespace"})
	PodDirectLists = promauto.NewCounter(prometheus.CounterOpts{
		Name: "incidentflow_k8s_agent_pod_direct_list_total",
		Help: "Logical live Pod LIST requests issued for commands, excluding informer LISTs.",
	})
	PodListSharedResults = promauto.NewCounter(prometheus.CounterOpts{
		Name: "incidentflow_k8s_agent_pod_singleflight_shared_total",
		Help: "Pod LIST callers that received a shared live result, including the first caller.",
	})
	InformerWatchHealthy = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "incidentflow_k8s_agent_informer_watch_healthy",
		Help: "1 while the informer WATCH stream is open and has not reported an error.",
	}, []string{"resource", "watched_namespace"})
	InformerLastActivity = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "incidentflow_k8s_agent_informer_last_activity_timestamp_seconds",
		Help: "Unix timestamp of the last event, bookmark or successful LIST; zero before the first observation.",
	}, []string{"resource", "watched_namespace", "activity"})
	InformerObjects = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "incidentflow_k8s_agent_cache_objects",
		Help: "Number of objects held in the informer store.",
	}, []string{"resource", "watched_namespace"})
	InformerPayloadBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "incidentflow_k8s_agent_cache_payload_bytes",
		Help: "Protobuf size of objects held in the informer store, excluding Go heap and index overhead.",
	}, []string{"resource", "watched_namespace"})
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
// /metrics, /healthz and /readyz at addr.
func NewMetricsServer(addr string, health *HealthState) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if health == nil || !health.Ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	return &http.Server{Addr: addr, Handler: mux}
}
