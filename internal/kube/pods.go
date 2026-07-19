package kube

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Service) ListPods(ctx context.Context, namespace string) ([]Pod, error) {
	items, err := s.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Pod, 0, len(items.Items))
	for _, pod := range items.Items {
		out = append(out, toPod(pod))
	}
	return out, nil
}

func (s *Service) GetPod(ctx context.Context, namespace, name string) (Pod, error) {
	pod, err := s.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Pod{}, err
	}
	return toPod(*pod), nil
}

func toPod(pod corev1.Pod) Pod {
	containers := make([]ContainerStatus, 0, len(pod.Status.ContainerStatuses))
	images := map[string]string{}
	for _, container := range pod.Spec.Containers {
		images[container.Name] = container.Image
	}
	for _, status := range pod.Status.ContainerStatuses {
		containers = append(containers, ContainerStatus{
			Name:          status.Name,
			Ready:         status.Ready,
			RestartCount:  status.RestartCount,
			LastRestartAt: lastRestartAt(status),
			Image:         images[status.Name],
		})
	}
	return Pod{
		Name:       pod.Name,
		Namespace:  pod.Namespace,
		Labels:     pod.Labels,
		Phase:      string(pod.Status.Phase),
		NodeName:   pod.Spec.NodeName,
		Containers: containers,
		Age:        age(pod.CreationTimestamp.Time),
	}
}

func lastRestartAt(status corev1.ContainerStatus) string {
	term := status.LastTerminationState.Terminated
	if term == nil || term.FinishedAt.IsZero() {
		return ""
	}
	return term.FinishedAt.Format("2006-01-02T15:04:05Z07:00")
}

func age(created time.Time) string {
	if created.IsZero() {
		return ""
	}
	return time.Since(created).Round(time.Second).String()
}
