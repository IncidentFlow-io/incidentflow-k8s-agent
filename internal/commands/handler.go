package commands

import (
	"context"
	"time"

	"github.com/incidentflow/incidentflow-k8s-agent/internal/kube"
)

type Kubernetes interface {
	ListNamespaces(ctx context.Context) ([]kube.Namespace, error)
	ListPods(ctx context.Context, namespace string, maxAge time.Duration) ([]kube.Pod, bool, error)
	ListPodsForDeployment(ctx context.Context, namespace, deployment string) ([]kube.Pod, error)
	ListPodsForHealth(ctx context.Context, namespace string, maxAge time.Duration) ([]kube.Pod, bool, error)
	GetPod(ctx context.Context, namespace, name string) (kube.Pod, error)
	GetPodLogs(ctx context.Context, namespace, pod, container string, tailLines int64, maxBytes int64) (kube.LogResult, error)
	ListEvents(ctx context.Context, namespace string) ([]kube.Event, error)
	ListEventsForDeployment(ctx context.Context, namespace, deployment string) ([]kube.Event, error)
	ListDeployments(ctx context.Context, namespace string) ([]kube.Deployment, error)
	ListServices(ctx context.Context, namespace string) ([]kube.ServiceResource, error)
	GetServiceEndpoints(ctx context.Context, namespace, serviceName string) (kube.ServiceEndpointStatus, error)
	GetRolloutStatus(ctx context.Context, namespace, name string) (kube.RolloutStatus, error)
	DescribePod(ctx context.Context, namespace, name string) (kube.PodDescription, error)
}
