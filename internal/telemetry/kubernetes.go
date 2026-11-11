package telemetry

import (
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"k8s.io/client-go/rest"
)

var kubernetesRequests = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "incidentflow_k8s_agent_kubernetes_api_requests_total",
	Help: "Kubernetes API HTTP requests issued by this agent, including retries.",
}, []string{"operation"})

func init() {
	for _, operation := range []string{"get", "list", "watch", "create", "update", "patch", "delete", "other"} {
		kubernetesRequests.WithLabelValues(operation)
	}
}

func ObserveKubernetesRequests(config *rest.Config) {
	config.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return kubernetesTransport{next: next}
	})
}

type kubernetesTransport struct {
	next http.RoundTripper
}

func (t kubernetesTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	kubernetesRequests.WithLabelValues(kubernetesOperation(request)).Inc()
	return t.next.RoundTrip(request)
}

func kubernetesOperation(request *http.Request) string {
	switch request.Method {
	case http.MethodGet:
		watch := request.URL.Query().Get("watch")
		if watch == "true" || watch == "1" {
			return "watch"
		}
		parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
		var resource []string
		switch {
		case len(parts) > 2 && parts[0] == "api":
			resource = parts[2:]
		case len(parts) > 3 && parts[0] == "apis":
			resource = parts[3:]
		}
		if len(resource) == 1 || (len(resource) == 3 && resource[0] == "namespaces" &&
			resource[2] != "status" && resource[2] != "finalize") {
			return "list"
		}
		return "get"
	case http.MethodPost:
		return "create"
	case http.MethodPut:
		return "update"
	case http.MethodPatch:
		return "patch"
	case http.MethodDelete:
		return "delete"
	default:
		return "other"
	}
}
