package telemetry

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsServerExposesAgentMetrics(t *testing.T) {
	// Touch each metric so it appears in the exposition output.
	GatewayConnected.Set(1)
	GatewayReconnects.Inc()
	K8sAPIDuration.WithLabelValues("k8s_list_pods", "success").Observe(0.01)

	srv := httptest.NewServer(NewMetricsServer(":0").Handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	got := string(body)

	want := []string{
		"k8s_api_duration_seconds",
		"gateway_connected",
		"gateway_reconnects_total",
		"go_goroutines",                 // free from the Go collector
		"process_resident_memory_bytes", // free from the process collector
	}
	for _, name := range want {
		if !strings.Contains(got, name) {
			t.Errorf("/metrics output missing %q", name)
		}
	}
}

func TestMetricsServerHealthz(t *testing.T) {
	srv := httptest.NewServer(NewMetricsServer(":0").Handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", resp.StatusCode)
	}
}
