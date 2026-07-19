package kube

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestToPodIncludesLastRestartAt(t *testing.T) {
	finishedAt := metav1.NewTime(time.Date(2026, 6, 21, 12, 30, 0, 0, time.UTC))
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "api-123",
			Namespace:         "incidentflow-dev",
			CreationTimestamp: metav1.NewTime(time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "api", Image: "api:v1"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "api",
					Ready:        true,
					RestartCount: 1,
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{FinishedAt: finishedAt},
					},
				},
			},
		},
	}

	result := toPod(pod)

	if len(result.Containers) != 1 {
		t.Fatalf("expected one container, got %d", len(result.Containers))
	}
	if result.Containers[0].LastRestartAt != "2026-06-21T12:30:00Z" {
		t.Fatalf("unexpected last_restart_at: %q", result.Containers[0].LastRestartAt)
	}
}
