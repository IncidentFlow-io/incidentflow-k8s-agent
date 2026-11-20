package kube

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDeploymentEvidenceUsesSelectorAndOwnerUIDs(t *testing.T) {
	controller := true
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "ratings-v1", Namespace: "demo-shop", UID: "deployment-uid"},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "ratings"}}},
	}
	ownedRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ratings-v1-abc", Namespace: "demo-shop", UID: "rs-uid",
			Labels:          map[string]string{"app": "ratings"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "ratings-v1", UID: deployment.UID, Controller: &controller}},
		},
	}
	foreignRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ratings-v2-def", Namespace: "demo-shop", UID: "foreign-rs-uid",
			Labels:          map[string]string{"app": "ratings"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "ratings-v2", UID: "foreign-deployment-uid", Controller: &controller}},
		},
	}
	targetPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ratings-v1-abc-pod", Namespace: "demo-shop", UID: "pod-uid",
			Labels:          map[string]string{"app": "ratings"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: ownedRS.Name, UID: ownedRS.UID, Controller: &controller}},
		},
	}
	foreignPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ratings-v2-def-pod", Namespace: "demo-shop", UID: "foreign-pod-uid",
			Labels:          map[string]string{"app": "ratings"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: foreignRS.Name, UID: foreignRS.UID, Controller: &controller}},
		},
	}
	client := fake.NewSimpleClientset(
		deployment, ownedRS, foreignRS, targetPod, foreignPod,
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "target-event", Namespace: "demo-shop"}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: targetPod.Name, UID: targetPod.UID}},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "foreign-event", Namespace: "demo-shop"}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: foreignPod.Name, UID: foreignPod.UID}},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "other-event", Namespace: "demo-shop"}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "unknown-pod", UID: "unknown-uid"}},
	)
	service := NewService(client)

	pods, err := service.ListPodsForDeployment(context.Background(), "demo-shop", "ratings-v1")
	if err != nil || len(pods) != 1 || pods[0].UID != string(targetPod.UID) || pods[0].OwnerReferences[0].UID != string(ownedRS.UID) {
		t.Fatalf("expected only target workload pod with owner provenance; pods=%#v err=%v", pods, err)
	}
	events, err := service.ListEventsForDeployment(context.Background(), "demo-shop", "ratings-v1")
	if err != nil || len(events) != 1 || events[0].ObjectUID != string(targetPod.UID) {
		t.Fatalf("expected only event for target pod UID; events=%#v err=%v", events, err)
	}
}
