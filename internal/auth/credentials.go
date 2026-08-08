package auth

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	AgentIDKey    = "agent_id"
	AgentTokenKey = "agent_token"
)

// CredentialStore keeps the long-lived agent identity inside the cluster. It
// intentionally never handles the short-lived registration token.
type CredentialStore interface {
	Load(context.Context) (Identity, error)
	Save(context.Context, Identity) error
}

type KubernetesCredentialStore struct {
	secrets   kubernetes.Interface
	namespace string
	name      string
}

func NewInClusterCredentialStore(namespace, name string) (*KubernetesCredentialStore, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster Kubernetes config: %w", err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return NewKubernetesCredentialStore(client, namespace, name), nil
}

func NewKubernetesCredentialStore(client kubernetes.Interface, namespace, name string) *KubernetesCredentialStore {
	return &KubernetesCredentialStore{secrets: client, namespace: namespace, name: name}
}

func (s *KubernetesCredentialStore) Load(ctx context.Context) (Identity, error) {
	secret, err := s.secrets.CoreV1().Secrets(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return Identity{}, fmt.Errorf("credentials secret %q is missing; reapply the Helm release before registering", s.name)
	}
	if err != nil {
		return Identity{}, fmt.Errorf("read credentials secret: %w", err)
	}
	identity := Identity{AgentID: string(secret.Data[AgentIDKey]), Token: string(secret.Data[AgentTokenKey])}
	if identity.AgentID == "" && identity.Token == "" {
		return Identity{}, nil // Empty Secret is the expected Helm bootstrap state.
	}
	if identity.AgentID == "" || identity.Token == "" {
		return Identity{}, fmt.Errorf("credentials secret %q is incomplete", s.name)
	}
	return identity, nil
}

func (s *KubernetesCredentialStore) Save(ctx context.Context, identity Identity) error {
	if identity.AgentID == "" || identity.Token == "" {
		return fmt.Errorf("refusing to persist incomplete agent credentials")
	}
	secrets := s.secrets.CoreV1().Secrets(s.namespace)
	secret, err := secrets.Get(ctx, s.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("credentials secret %q is missing; reapply the Helm release before registering", s.name)
	}
	if err != nil {
		return fmt.Errorf("read credentials secret before update: %w", err)
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[AgentIDKey] = []byte(identity.AgentID)
	secret.Data[AgentTokenKey] = []byte(identity.Token)
	if _, err := secrets.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update credentials secret: %w", err)
	}
	return nil
}
