package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetServiceEndpointsCountsReadyAndNotReadyAddresses(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "ratings", Namespace: "demo-shop"},
		Subsets: []corev1.EndpointSubset{{
			Addresses:         []corev1.EndpointAddress{{IP: "10.0.0.1"}},
			NotReadyAddresses: []corev1.EndpointAddress{{IP: "10.0.0.2"}},
		}},
	})
	status, err := NewService(client).GetServiceEndpoints(context.Background(), "demo-shop", "ratings")
	if err != nil {
		t.Fatal(err)
	}
	if status.ReadyEndpointCount != 1 || status.EndpointCount != 2 {
		t.Fatalf("unexpected endpoint status: %#v", status)
	}
}
