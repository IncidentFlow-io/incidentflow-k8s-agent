package auth

import (
	"context"
	"errors"
	"testing"
)

type fakeCredentialStore struct {
	identity Identity
	loadErr  error
	saved    Identity
}

func (s *fakeCredentialStore) Load(context.Context) (Identity, error) { return s.identity, s.loadErr }
func (s *fakeCredentialStore) Save(_ context.Context, identity Identity) error {
	s.saved = identity
	return nil
}

type fakeRegistrar struct {
	identity Identity
	calls    int
	err      error
}

func (r *fakeRegistrar) Register(context.Context, string, string) (Identity, error) {
	r.calls++
	return r.identity, r.err
}

func TestBootstrapUsesExistingCredentials(t *testing.T) {
	store := &fakeCredentialStore{identity: Identity{AgentID: "agent_1", Token: "if_agent_existing"}}
	registrar := &fakeRegistrar{}
	identity, registered, err := Bootstrap(context.Background(), store, registrar, "if_reg_unused", "kind", "test")
	if err != nil || registered || identity.Token != "if_agent_existing" || registrar.calls != 0 {
		t.Fatalf("unexpected bootstrap result: %+v, %t, %v, calls=%d", identity, registered, err, registrar.calls)
	}
}

func TestBootstrapRegistersAndPersistsCredentials(t *testing.T) {
	store := &fakeCredentialStore{}
	registrar := &fakeRegistrar{identity: Identity{AgentID: "agent_1", Token: "if_agent_new"}}
	identity, registered, err := Bootstrap(context.Background(), store, registrar, "if_reg_once", "kind", "test")
	if err != nil || !registered || identity.AgentID != "agent_1" || store.saved.Token != "if_agent_new" {
		t.Fatalf("unexpected bootstrap result: %+v, %t, %v, saved=%+v", identity, registered, err, store.saved)
	}
}

func TestBootstrapRejectsIncompleteCredentialsSecret(t *testing.T) {
	store := &fakeCredentialStore{loadErr: errors.New("credentials secret \"incidentflow-agent-credentials\" is incomplete")}
	_, _, err := Bootstrap(context.Background(), store, &fakeRegistrar{}, "if_reg_once", "kind", "test")
	if err == nil {
		t.Fatal("expected incomplete credentials error")
	}
}

func TestBootstrapRequiresTokenWhenCredentialsAreAbsent(t *testing.T) {
	_, _, err := Bootstrap(context.Background(), &fakeCredentialStore{}, &fakeRegistrar{}, "", "kind", "test")
	if err == nil {
		t.Fatal("expected missing registration token error")
	}
}
