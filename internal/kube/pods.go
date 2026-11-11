package kube

import (
	"context"
	"maps"
	"sort"
	"time"

	"github.com/incidentflow/incidentflow-k8s-agent/internal/telemetry"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func (s *Service) ListPods(ctx context.Context, namespace string, maxAge time.Duration) ([]Pod, bool, error) {
	return s.listPods(ctx, namespace, maxAge, false)
}

// ListPodsForHealth returns unhealthy pods and completed pods for the health tool.
func (s *Service) ListPodsForHealth(ctx context.Context, namespace string, maxAge time.Duration) ([]Pod, bool, error) {
	return s.listPods(ctx, namespace, maxAge, true)
}

func (s *Service) listPods(ctx context.Context, namespace string, maxAge time.Duration, healthOnly bool) ([]Pod, bool, error) {
	if cached := s.podCaches[namespace]; cached != nil {
		if cached.usable(time.Now(), maxAge) {
			items, err := cached.list(healthOnly)
			out := make([]Pod, 0, len(items))
			for _, item := range items {
				pod := toPod(*item)
				pod.Labels = maps.Clone(pod.Labels)
				out = append(out, pod)
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
			if err == nil && cached.usable(time.Now(), maxAge) {
				telemetry.PodCacheRequests.WithLabelValues("hit").Inc()
				telemetry.PodAPIRequestsAvoided.WithLabelValues("pods").Inc()
				return out, true, nil
			}
		}
		telemetry.PodCacheRequests.WithLabelValues("miss").Inc()
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	pending := s.podLists.DoChan(namespace, func() (any, error) {
		requestCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		select {
		case s.podListSlots <- struct{}{}:
			defer func() { <-s.podListSlots }()
		case <-requestCtx.Done():
			return nil, requestCtx.Err()
		}
		telemetry.PodDirectLists.Inc()
		return s.client.CoreV1().Pods(namespace).List(requestCtx, metav1.ListOptions{})
	})
	var items *corev1.PodList
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case result := <-pending:
		if result.Shared {
			telemetry.PodListSharedResults.Inc()
		}
		if result.Err != nil {
			return nil, false, result.Err
		}
		items = result.Val.(*corev1.PodList)
	}
	out := make([]Pod, 0, len(items.Items))
	for _, pod := range items.Items {
		if healthOnly && !podInHealthSummary(&pod) {
			continue
		}
		item := toPod(pod)
		item.Labels = maps.Clone(item.Labels)
		out = append(out, item)
	}
	return out, false, nil
}

func (c *podCache) list(healthOnly bool) ([]*corev1.Pod, error) {
	if !healthOnly {
		return c.lister.List(labels.Everything())
	}
	items, err := c.informer.GetIndexer().ByIndex(podHealthIndex, "included")
	if err != nil {
		return nil, err
	}
	out := make([]*corev1.Pod, 0, len(items))
	for _, item := range items {
		out = append(out, item.(*corev1.Pod))
	}
	return out, nil
}

func podInHealthSummary(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return true
	}
	for _, container := range pod.Status.ContainerStatuses {
		if !container.Ready || container.RestartCount > 5 {
			return true
		}
	}
	return false
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
