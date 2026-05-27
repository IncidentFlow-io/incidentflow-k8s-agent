package kube

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Service) ListServices(ctx context.Context, namespace string) ([]ServiceResource, error) {
	items, err := s.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]ServiceResource, 0, len(items.Items))
	for _, svc := range items.Items {
		ports := make([]ServicePort, 0, len(svc.Spec.Ports))
		for _, port := range svc.Spec.Ports {
			ports = append(ports, ServicePort{
				Name:       port.Name,
				Port:       port.Port,
				TargetPort: port.TargetPort.String(),
				Protocol:   string(port.Protocol),
			})
		}
		out = append(out, ServiceResource{
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Labels:    svc.Labels,
			Type:      string(svc.Spec.Type),
			ClusterIP: svc.Spec.ClusterIP,
			Ports:     ports,
			Age:       age(svc.CreationTimestamp.Time),
		})
	}
	return out, nil
}
