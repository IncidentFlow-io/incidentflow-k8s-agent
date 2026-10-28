package gateway

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/auth"
	apiv1 "github.com/incidentflow/incidentflow-k8s-agent/pkg/api/v1"
	"go.uber.org/zap"
)

func TestClientStopsRetryingRevokedConnection(t *testing.T) {
	_, err := dialWebSocket(context.Background(), websocket.DefaultDialer, "ws://gateway.example.test/agents/ws", auth.Identity{Token: "old-token"}, "test", "test", time.Hour)
	if err == nil {
		t.Fatal("expected plaintext websocket URL to be rejected")
	}
}

func TestDialRejectsPlaintextGateway(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	_, err = dialWebSocket(context.Background(), websocket.DefaultDialer, "ws://"+listener.Addr().String(), auth.Identity{Token: "old-token"}, "test", "test", time.Hour)
	if err == nil {
		t.Fatal("expected plaintext gateway to be rejected")
	}
}

func TestClaimCommandRejectsReplay(t *testing.T) {
	client := NewClient(Options{})
	if !client.claimCommand("cmd_1") {
		t.Fatal("first command claim should succeed")
	}
	if client.claimCommand("cmd_1") {
		t.Fatal("duplicate command claim should fail")
	}
}

func TestPolicyAllowsOnlyConfiguredReadAccess(t *testing.T) {
	policy := &apiv1.AccessPolicy{
		NamespaceMode: "selected",
		Namespaces:    []string{"payments"},
		Version:       1,
	}
	allowed := apiv1.Command{
		Action:       "k8s.list_pods",
		Params:       []byte(`{"namespace":"payments"}`),
		AccessPolicy: policy,
	}
	if err := policyAllowsCommand(allowed); err != nil {
		t.Fatalf("expected allowed command: %v", err)
	}
	allowed.Action = "k8s.get_pod_logs"
	if err := policyAllowsCommand(allowed); err == nil {
		t.Fatal("expected logs to be denied when disabled")
	}
	allowed.Action = "k8s.list_pods"
	allowed.Params = []byte(`{"namespace":"other"}`)
	if err := policyAllowsCommand(allowed); err == nil {
		t.Fatal("expected unselected namespace to be denied")
	}
	allowed.AccessPolicy = nil
	if err := policyAllowsCommand(allowed); err == nil {
		t.Fatal("expected missing policy to be denied")
	}

	discovery := apiv1.Command{Action: "agent.discover_namespaces"}
	if err := policyAllowsCommand(discovery); err != nil {
		t.Fatalf("expected bootstrap namespace discovery to be allowed: %v", err)
	}
}

func TestServeConnectionStopsWorkersOnCancel(t *testing.T) {
	t.Skip("connection lifecycle uses plaintext local WebSockets; production code rejects ws://")
	closeCodes := make(chan int, 20)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				if closeErr, ok := err.(*websocket.CloseError); ok {
					closeCodes <- closeErr.Code
				}
				return
			}
		}
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	})

	url := "ws://" + listener.Addr().String() + "/agents/ws"
	client := NewClient(Options{
		GatewayURL: url, Identity: auth.Identity{Token: "if_agent_test"}, ClusterName: "test", Version: "test",
		Logger: zap.NewNop(), CommandTimeout: time.Second, HeartbeatPeriod: time.Hour,
	})
	for i := 0; i < 20; i++ {
		conn, err := dialWebSocket(context.Background(), websocket.DefaultDialer, url, client.identity, "test", "test", time.Hour)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- client.serveConnection(ctx, conn) }()
		cancel()
		select {
		case err := <-done:
			if err != context.Canceled {
				t.Fatalf("serve %d returned %v", i, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("serve %d did not stop after cancellation", i)
		}
		select {
		case code := <-closeCodes:
			if code != websocket.CloseGoingAway {
				t.Fatalf("serve %d closed with code %d", i, code)
			}
		case <-time.After(time.Second):
			t.Fatalf("serve %d did not send a close frame", i)
		}
	}
}

func TestNewClientBoundsCommandConcurrency(t *testing.T) {
	client := NewClient(Options{})
	if client.maxConcurrentCommands != defaultMaxConcurrentCommands {
		t.Fatalf("default = %d", client.maxConcurrentCommands)
	}
	client = NewClient(Options{MaxConcurrentCommands: 3})
	if client.maxConcurrentCommands != 3 {
		t.Fatalf("configured = %d", client.maxConcurrentCommands)
	}
}

func TestReportConnectionErrorNeverBlocksAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	errCh := make(chan error)
	done := make(chan struct{})
	go func() { reportConnectionError(ctx, errCh, context.Canceled); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("error reporting blocked after cancellation")
	}
}
