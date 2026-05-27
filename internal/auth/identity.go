package auth

type Identity struct {
	AgentID   string `json:"agent_id"`
	ClusterID string `json:"cluster_id"`
	Token     string `json:"agent_token"`
}

func (i Identity) Valid() bool {
	return i.Token != ""
}
