package kube

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (s *Service) ListPodsForDeployment(ctx context.Context, namespace, name string) ([]Pod, error) {
	_, _, pods, err := s.deploymentPods(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	out := make([]Pod, 0, len(pods))
	for _, pod := range pods {
		out = append(out, toPod(*pod))
	}
	return out, nil
}

func (s *Service) ListEventsForDeployment(ctx context.Context, namespace, name string) ([]Event, error) {
	deployment, replicaSets, pods, err := s.deploymentPods(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	workloadUIDs := map[types.UID]struct{}{deployment.UID: {}}
	for _, replicaSet := range replicaSets {
		workloadUIDs[replicaSet.UID] = struct{}{}
	}
	for _, pod := range pods {
		workloadUIDs[pod.UID] = struct{}{}
	}
	events, err := s.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0)
	for _, event := range events.Items {
		if _, ok := workloadUIDs[event.InvolvedObject.UID]; ok && event.InvolvedObject.UID != "" {
			out = append(out, toEvent(event))
		}
	}
	return out, nil
}

func (s *Service) deploymentPods(
	ctx context.Context, namespace, name string,
) (*appsv1.Deployment, []*appsv1.ReplicaSet, []*corev1.Pod, error) {
	deployment, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, nil, err
	}
	if deployment.Spec.Selector == nil {
		return nil, nil, nil, fmt.Errorf("deployment %s/%s has no pod selector", namespace, name)
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil || selector.Empty() {
		return nil, nil, nil, fmt.Errorf("deployment %s/%s has an invalid pod selector", namespace, name)
	}
	replicaSetList, err := s.client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, nil, nil, err
	}
	replicaSets := make([]*appsv1.ReplicaSet, 0, len(replicaSetList.Items))
	replicaSetUIDs := map[types.UID]struct{}{}
	for i := range replicaSetList.Items {
		item := &replicaSetList.Items[i]
		if metav1.IsControlledBy(item, deployment) {
			replicaSets = append(replicaSets, item)
			replicaSetUIDs[item.UID] = struct{}{}
		}
	}
	podList, err := s.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, nil, nil, err
	}
	pods := make([]*corev1.Pod, 0, len(podList.Items))
	for i := range podList.Items {
		pod := &podList.Items[i]
		for _, owner := range pod.OwnerReferences {
			if owner.Controller != nil && *owner.Controller && owner.Kind == "ReplicaSet" {
				if _, ok := replicaSetUIDs[owner.UID]; ok {
					pods = append(pods, pod)
					break
				}
			}
		}
	}
	return deployment, replicaSets, pods, nil
}
