package v1

import "encoding/json"

const (
	MessageTypeCommand  = "command"
	MessageTypeResponse = "response"
)

type Command struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Action string          `json:"action"`
	Params json.RawMessage `json:"params,omitempty"`
}

type ListNamespacesParams struct{}

type NamespaceParams struct {
	Namespace string `json:"namespace,omitempty"`
}

type ListPodsParams struct {
	Namespace string `json:"namespace,omitempty"`
}

type GetPodParams struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
}

type GetPodLogsParams struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container,omitempty"`
	TailLines int64  `json:"tail_lines,omitempty"`
}

type ListEventsParams struct {
	Namespace string `json:"namespace,omitempty"`
}

type ListDeploymentsParams struct {
	Namespace string `json:"namespace,omitempty"`
}

type ListServicesParams struct {
	Namespace string `json:"namespace,omitempty"`
}

type GetRolloutStatusParams struct {
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
}
