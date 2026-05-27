package kube

import "context"

func (s *Service) Metrics(ctx context.Context) (map[string]any, error) {
	return s.Health(ctx)
}
