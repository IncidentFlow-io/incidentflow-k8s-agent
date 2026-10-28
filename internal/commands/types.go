package commands

const (
	ActionListNamespaces   = "k8s.list_namespaces"
	ActionListPods         = "k8s.list_pods"
	ActionGetPod           = "k8s.get_pod"
	ActionGetPodLogs       = "k8s.get_pod_logs"
	ActionListEvents       = "k8s.list_events"
	ActionListDeployments  = "k8s.list_deployments"
	ActionListServices     = "k8s.list_services"
	ActionGetRolloutStatus = "k8s.get_rollout_status"
	ActionDescribePod      = "k8s.describe_pod"
	// ActionDiscoverNamespaces is an internal gateway operation. It is not an
	// MCP tool and must only be dispatched by the Access Control admin flow.
	ActionDiscoverNamespaces = "agent.discover_namespaces"
)

var allowedActions = map[string]struct{}{
	ActionListNamespaces:     {},
	ActionListPods:           {},
	ActionGetPod:             {},
	ActionGetPodLogs:         {},
	ActionListEvents:         {},
	ActionListDeployments:    {},
	ActionListServices:       {},
	ActionGetRolloutStatus:   {},
	ActionDescribePod:        {},
	ActionDiscoverNamespaces: {},
}

func IsAllowedAction(action string) bool {
	_, ok := allowedActions[action]
	return ok
}
