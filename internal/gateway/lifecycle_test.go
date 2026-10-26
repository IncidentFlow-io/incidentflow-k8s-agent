package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/auth"
	"go.uber.org/zap"
)

func TestClientStopsRetryingRevokedConnection(t *testing.T) {
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
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Agent access revoked"), time.Now().Add(time.Second))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	client := NewClient(Options{
		GatewayURL: "ws://" + listener.Addr().String(), Identity: auth.Identity{AgentID: "old-agent", Token: "old-token"},
		ClusterName: "test", Version: "test", Logger: zap.NewNop(), HeartbeatPeriod: time.Hour,
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Run(ctx); !errors.Is(err, ErrAccessRevoked) {
		t.Fatalf("expected revoked access, got %v", err)
	}
}

func TestDialRejectsForbiddenAgent(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	_, err = dialWebSocket(context.Background(), websocket.DefaultDialer, "ws://"+listener.Addr().String(), auth.Identity{Token: "old-token"}, "test", "test", time.Hour)
	if !errors.Is(err, ErrAccessRevoked) {
		t.Fatalf("expected revoked access for 403 handshake, got %v", err)
	}
}

func TestServeConnectionStopsWorkersOnCancel(t *testing.T) {
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
