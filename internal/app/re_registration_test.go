package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/incidentflow/incidentflow-k8s-agent/internal/auth"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/config"
	"go.uber.org/zap"
)

type changingTokenSource struct {
	mu    sync.Mutex
	loads int
}

func (s *changingTokenSource) Load(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	if s.loads < 3 {
		return "old-used-token", nil
	}
	return "new-token", nil
}

type savedCredentials struct{ identity auth.Identity }

func (s *savedCredentials) Load(context.Context) (auth.Identity, error) {
	return s.identity, nil
}

func (s *savedCredentials) Save(_ context.Context, identity auth.Identity) error {
	s.identity = identity
	return nil
}

func TestRevokedAgentWaitsForNewTokenAndReplacesCredentials(t *testing.T) {
	var mu sync.Mutex
	called := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RegistrationToken string `json:"registration_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode registration request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		called[body.RegistrationToken]++
		mu.Unlock()
		if body.RegistrationToken != "new-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agent_id":"agent_new","cluster_id":"cluster_1","agent_token":"agent-token-new"}`))
	}))
	defer server.Close()

	a := New(config.Config{PlatformURL: server.URL, ClusterName: "incidentflow-agent"}, zap.NewNop())
	store := &savedCredentials{identity: auth.Identity{AgentID: "agent_revoked", Token: "old-agent-token"}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	identity, err := a.waitForNewRegistration(ctx, store, &changingTokenSource{}, "dev", 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if identity.AgentID != "agent_new" || store.identity.AgentID != "agent_new" {
		t.Fatalf("new credentials were not persisted: %+v, %+v", identity, store.identity)
	}
	mu.Lock()
	defer mu.Unlock()
	if called["old-used-token"] != 1 || called["new-token"] != 1 {
		t.Fatalf("unexpected registration attempts: %+v", called)
	}
}
