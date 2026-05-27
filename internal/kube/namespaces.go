package kube

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Service) ListNamespaces(ctx context.Context) ([]Namespace, error) {
	items, err := s.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Namespace, 0, len(items.Items))
	for _, ns := range items.Items {
		out = append(out, Namespace{
			Name:   ns.Name,
			Labels: ns.Labels,
			Status: string(ns.Status.Phase),
		})
	}
	return out, nil
}
