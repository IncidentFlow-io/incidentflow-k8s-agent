package auth

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// RegistrationTokenSource reads the current one-time token without persisting it.
// Unlike a Secret-backed environment variable, it observes replacements while
// the pod is running.
type RegistrationTokenSource interface {
	Load(context.Context) (string, error)
}

type KubernetesRegistrationTokenSource struct {
	secrets   kubernetes.Interface
	namespace string
	name      string
}

func NewInClusterRegistrationTokenSource(namespace, name string) (*KubernetesRegistrationTokenSource, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster Kubernetes config: %w", err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return NewKubernetesRegistrationTokenSource(client, namespace, name), nil
}

func NewKubernetesRegistrationTokenSource(client kubernetes.Interface, namespace, name string) *KubernetesRegistrationTokenSource {
	return &KubernetesRegistrationTokenSource{secrets: client, namespace: namespace, name: name}
}

func (s *KubernetesRegistrationTokenSource) Load(ctx context.Context) (string, error) {
	secret, err := s.secrets.CoreV1().Secrets(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read registration Secret: %w", err)
	}
	return string(secret.Data["registration-token"]), nil
}
