package kube

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/incidentflow/incidentflow-k8s-agent/internal/metrics"
)

func (s *Service) ListNamespaces(ctx context.Context) ([]Namespace, error) {
	s.nsMu.Lock()
	if len(s.nsCache) > 0 && time.Since(s.nsCachedAt) < namespaceCacheTTL {
		cached := s.nsCache
		s.nsMu.Unlock()
		metrics.IncCacheResult("hit")
		return cached, nil
	}
	s.nsMu.Unlock()
	metrics.IncCacheResult("miss")

	// singleflight collapses concurrent cache-miss fetches into one API call.
	v, err, _ := s.nsSf.Do("list", func() (any, error) {
		return s.fetchNamespaces(ctx)
	})
	if err != nil {
		return nil, err
	}
	return v.([]Namespace), nil
}

func (s *Service) fetchNamespaces(ctx context.Context) ([]Namespace, error) {
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
	s.nsMu.Lock()
	s.nsCache = out
	s.nsCachedAt = time.Now()
	s.nsMu.Unlock()
	return out, nil
}
