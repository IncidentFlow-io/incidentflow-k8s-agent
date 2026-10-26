package auth

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRegistrationTokenSourceObservesSecretReplacement(t *testing.T) {
	client := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "bootstrap", Namespace: "agent"},
		Data:       map[string][]byte{"registration-token": []byte("old-token")},
	})
	source := NewKubernetesRegistrationTokenSource(client, "agent", "bootstrap")
	ctx := context.Background()
	if token, err := source.Load(ctx); err != nil || token != "old-token" {
		t.Fatalf("first load: %q, %v", token, err)
	}
	secret, err := client.CoreV1().Secrets("agent").Get(ctx, "bootstrap", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	secret.Data["registration-token"] = []byte("new-token")
	if _, err := client.CoreV1().Secrets("agent").Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if token, err := source.Load(ctx); err != nil || token != "new-token" {
		t.Fatalf("replacement load: %q, %v", token, err)
	}
}
