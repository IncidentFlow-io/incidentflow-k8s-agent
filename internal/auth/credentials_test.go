package auth

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesCredentialStoreLoadsExistingCredentials(t *testing.T) {
	client := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: "agent"},
		Data:       map[string][]byte{AgentIDKey: []byte("agent_1"), AgentTokenKey: []byte("if_agent_secret")},
	})
	identity, err := NewKubernetesCredentialStore(client, "agent", "credentials").Load(context.Background())
	if err != nil || identity.AgentID != "agent_1" || identity.Token != "if_agent_secret" {
		t.Fatalf("unexpected identity: %+v, %v", identity, err)
	}
}

func TestKubernetesCredentialStoreRejectsIncompleteSecret(t *testing.T) {
	client := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: "agent"},
		Data:       map[string][]byte{AgentTokenKey: []byte("if_agent_secret")},
	})
	_, err := NewKubernetesCredentialStore(client, "agent", "credentials").Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete secret error, got %v", err)
	}
}

func TestKubernetesCredentialStorePopulatesHelmCreatedSecret(t *testing.T) {
	client := fake.NewClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: "agent"}})
	store := NewKubernetesCredentialStore(client, "agent", "credentials")
	if err := store.Save(context.Background(), Identity{AgentID: "agent_1", Token: "if_agent_secret"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	identity, err := store.Load(context.Background())
	if err != nil || identity.AgentID != "agent_1" || identity.Token != "if_agent_secret" {
		t.Fatalf("unexpected persisted identity: %+v, %v", identity, err)
	}
}
