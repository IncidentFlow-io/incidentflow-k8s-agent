package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Registrar struct {
	platformURL       string
	registrationToken string
	httpClient        *http.Client
}

type registrationRequest struct {
	RegistrationToken string `json:"registration_token"`
	ClusterName       string `json:"cluster_name"`
	AgentVersion      string `json:"agent_version"`
}

type registrationResponse struct {
	AgentID    string `json:"agent_id"`
	ClusterID  string `json:"cluster_id"`
	AgentToken string `json:"agent_token"`
	GatewayURL string `json:"gateway_url"`
}

func NewRegistrar(platformURL, registrationToken string) Registrar {
	return Registrar{
		platformURL:       platformURL,
		registrationToken: registrationToken,
		httpClient:        &http.Client{Timeout: 15 * time.Second},
	}
}

func (r Registrar) Register(ctx context.Context, clusterName, version string) (Identity, error) {
	body, err := json.Marshal(registrationRequest{
		RegistrationToken: r.registrationToken,
		ClusterName:       clusterName,
		AgentVersion:      version,
	})
	if err != nil {
		return Identity{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.platformURL+"/api/v1/agents/register", bytes.NewReader(body))
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Authorization", "Bearer "+r.registrationToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "incidentflow-k8s-agent/"+version)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return Identity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Identity{}, fmt.Errorf("registration failed with status %s", resp.Status)
	}
	var decoded registrationResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&decoded); err != nil {
		return Identity{}, err
	}
	if decoded.AgentToken == "" {
		return Identity{}, fmt.Errorf("registration response did not include agent_token")
	}
	return Identity{
		AgentID:    decoded.AgentID,
		ClusterID:  decoded.ClusterID,
		Token:      decoded.AgentToken,
		GatewayURL: decoded.GatewayURL,
	}, nil
}
