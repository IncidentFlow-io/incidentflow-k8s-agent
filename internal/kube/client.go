package kube

import (
	"context"
	"fmt"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"golang.org/x/sync/singleflight"
)

const namespaceCacheTTL = 30 * time.Second

type Service struct {
	client    kubernetes.Interface
	discovery discovery.DiscoveryInterface

	nsMu       sync.Mutex
	nsCache    []Namespace
	nsCachedAt time.Time
	nsSf       singleflight.Group
}

func NewInClusterService() (*Service, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster Kubernetes config: %w", err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return NewService(client), nil
}

func NewService(client kubernetes.Interface) *Service {
	return &Service{client: client, discovery: client.Discovery()}
}

type Namespace struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Status string            `json:"status"`
}

type Pod struct {
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace"`
	Labels     map[string]string `json:"labels,omitempty"`
	Phase      string            `json:"phase"`
	NodeName   string            `json:"node_name,omitempty"`
	Containers []ContainerStatus `json:"containers,omitempty"`
	Age        string            `json:"age,omitempty"`
}

type ContainerStatus struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restart_count"`
	Image        string `json:"image"`
}

type LogResult struct {
	Logs      string `json:"logs"`
	Truncated bool   `json:"truncated"`
}

type Event struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Message   string `json:"message,omitempty"`
	Object    string `json:"object,omitempty"`
	Count     int32  `json:"count,omitempty"`
	LastSeen  string `json:"last_seen,omitempty"`
}

type Deployment struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Labels            map[string]string `json:"labels,omitempty"`
	Replicas          int32             `json:"replicas"`
	ReadyReplicas     int32             `json:"ready_replicas"`
	UpdatedReplicas   int32             `json:"updated_replicas"`
	AvailableReplicas int32             `json:"available_replicas"`
	Strategy          string            `json:"strategy"`
	Age               string            `json:"age,omitempty"`
}

type ServiceResource struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels,omitempty"`
	Type      string            `json:"type"`
	ClusterIP string            `json:"cluster_ip,omitempty"`
	Ports     []ServicePort     `json:"ports,omitempty"`
	Age       string            `json:"age,omitempty"`
}

type ServicePort struct {
	Name       string `json:"name,omitempty"`
	Port       int32  `json:"port"`
	TargetPort string `json:"target_port,omitempty"`
	Protocol   string `json:"protocol"`
}

type RolloutStatus struct {
	Deployment         string `json:"deployment"`
	Namespace          string `json:"namespace"`
	ObservedGeneration int64  `json:"observed_generation"`
	Generation         int64  `json:"generation"`
	Replicas           int32  `json:"replicas"`
	UpdatedReplicas    int32  `json:"updated_replicas"`
	ReadyReplicas      int32  `json:"ready_replicas"`
	AvailableReplicas  int32  `json:"available_replicas"`
	Complete           bool   `json:"complete"`
	Message            string `json:"message"`
}

func (s *Service) Health(ctx context.Context) (map[string]any, error) {
	version, err := s.discovery.ServerVersion()
	if err != nil {
		return nil, err
	}
	_, resources, err := s.discovery.ServerGroupsAndResources()
	if discovery.IsGroupDiscoveryFailedError(err) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	hasApps := false
	for _, resource := range resources {
		if resource.GroupVersion == appsv1.SchemeGroupVersion.String() {
			hasApps = true
			break
		}
	}
	return map[string]any{
		"kubernetes_version": version.String(),
		"apps_v1":            hasApps,
		"core_v1":            hasAPIGroup(resources, "v1"),
	}, nil
}

func hasAPIGroup(resources []*v1.APIResourceList, groupVersion string) bool {
	for _, resource := range resources {
		if resource.GroupVersion == groupVersion {
			return true
		}
	}
	return false
}
