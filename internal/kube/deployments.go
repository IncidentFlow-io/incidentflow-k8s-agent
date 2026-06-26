package kube

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Service) ListDeployments(ctx context.Context, namespace string) ([]Deployment, error) {
	items, err := s.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Deployment, 0, len(items.Items))
	for _, deployment := range items.Items {
		out = append(out, toDeployment(deployment))
	}
	return out, nil
}

func (s *Service) GetRolloutStatus(ctx context.Context, namespace, name string) (RolloutStatus, error) {
	deployment, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return RolloutStatus{}, err
	}
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	complete := deployment.Status.ObservedGeneration >= deployment.Generation &&
		deployment.Status.UpdatedReplicas == desired &&
		deployment.Status.ReadyReplicas == desired &&
		deployment.Status.AvailableReplicas == desired
	message := "rollout is in progress"
	if complete {
		message = "rollout is complete"
	}
	if deployment.Status.ObservedGeneration < deployment.Generation {
		message = "deployment controller has not observed the latest generation"
	}
	return RolloutStatus{
		Deployment:         deployment.Name,
		Namespace:          deployment.Namespace,
		ObservedGeneration: deployment.Status.ObservedGeneration,
		Generation:         deployment.Generation,
		Replicas:           desired,
		UpdatedReplicas:    deployment.Status.UpdatedReplicas,
		ReadyReplicas:      deployment.Status.ReadyReplicas,
		AvailableReplicas:  deployment.Status.AvailableReplicas,
		Complete:           complete,
		Message:            message,
	}, nil
}

func toDeployment(deployment appsv1.Deployment) Deployment {
	replicas := int32(1)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}
	return Deployment{
		Name:              deployment.Name,
		Namespace:         deployment.Namespace,
		Labels:            deployment.Labels,
		Replicas:          replicas,
		ReadyReplicas:     deployment.Status.ReadyReplicas,
		UpdatedReplicas:   deployment.Status.UpdatedReplicas,
		AvailableReplicas: deployment.Status.AvailableReplicas,
		Strategy:          string(deployment.Spec.Strategy.Type),
		Age:               age(deployment.CreationTimestamp.Time),
	}
}

