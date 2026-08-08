package gateway

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/auth"
	"go.uber.org/zap"
)

func TestServeConnectionStopsWorkersOnCancel(t *testing.T) {
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
