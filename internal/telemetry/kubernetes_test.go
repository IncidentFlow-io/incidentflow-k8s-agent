package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"k8s.io/client-go/rest"
)

func TestObserveKubernetesRequestsCountsTransportCallsByOperation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	config := &rest.Config{Host: server.URL}
	ObserveKubernetesRequests(config)
	client, err := rest.HTTPClientFor(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path, operation string }{
		{"GET", "/api/v1/namespaces/demo/pods", "list"},
		{"GET", "/apis/apps/v1/namespaces/demo/deployments?watch=true", "watch"},
		{"GET", "/api/v1/namespaces/demo", "get"},
		{"GET", "/api/v1/namespaces/demo/status", "get"},
		{"GET", "/api/v1/namespaces/demo/pods/pod/log", "get"},
		{"GET", "/api/v1/namespaces", "list"},
		{"PATCH", "/api/v1/namespaces/demo/secrets/credentials", "patch"},
	} {
		counter := kubernetesRequests.WithLabelValues(tc.operation)
		var before dto.Metric
		if err := counter.Write(&before); err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequest(tc.method, server.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		var after dto.Metric
		if err := counter.Write(&after); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusForbidden || after.GetCounter().GetValue() != before.GetCounter().GetValue()+1 {
			t.Errorf("%s %s: expected an unchanged 403 response and one %s request", tc.method, tc.path, tc.operation)
		}
	}
}
