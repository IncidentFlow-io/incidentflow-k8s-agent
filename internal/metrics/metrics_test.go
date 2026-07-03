package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObserveK8sAPIExposedViaHandler(t *testing.T) {
	ObserveK8sAPI("k8s.list_pods", "success", 0.042)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "k8s_api_duration_seconds") {
		t.Fatalf("metric name missing from /metrics output")
	}
	want := `k8s_api_duration_seconds_count{operation="k8s.list_pods",status="success"}`
	if !strings.Contains(body, want) {
		t.Fatalf("expected sample %q in output, got:\n%s", want, body)
	}
}

func TestGatewayAvailabilityMetricsExposed(t *testing.T) {
	SetGatewayConnected(true)
	IncGatewayReconnect()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"gateway_connected 1", "gateway_reconnects_total"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in /metrics output, got:\n%s", want, body)
		}
	}

	SetGatewayConnected(false)
	rec = httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "gateway_connected 0") {
		t.Fatalf("expected gateway_connected 0 after disconnect")
	}
}
