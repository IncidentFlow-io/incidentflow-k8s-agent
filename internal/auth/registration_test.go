package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestRegistrarSendsRegistrationTokenInBody(t *testing.T) {
	registrar := NewRegistrar("https://platform.example.com", "if_reg_secret")
	registrar.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/agents/register" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer if_reg_secret" {
			t.Fatalf("unexpected authorization header: %s", got)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got := body["registration_token"]; got != "if_reg_secret" {
			t.Fatalf("unexpected registration_token: %s", got)
		}
		if got := body["cluster_name"]; got != "prod" {
			t.Fatalf("unexpected cluster_name: %s", got)
		}
		if got := body["agent_version"]; got != "0.1.0" {
			t.Fatalf("unexpected agent_version: %s", got)
		}

		return jsonResponse(`{
			"agent_id":"agent_123",
			"cluster_id":"cluster_123",
			"agent_token":"if_agent_secret",
			"gateway_url":"wss://gateway.example.com/agent-gateway/agents/ws"
		}`), nil
	})}

	identity, err := registrar.Register(context.Background(), "prod", "0.1.0")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if identity.Token != "if_agent_secret" {
		t.Fatalf("unexpected agent token: %s", identity.Token)
	}
	if identity.GatewayURL != "wss://gateway.example.com/agent-gateway/agents/ws" {
		t.Fatalf("unexpected gateway URL: %s", identity.GatewayURL)
	}
}

func TestRegistrarAcceptsLegacyResponseWithoutGatewayURL(t *testing.T) {
	registrar := NewRegistrar("https://platform.example.com", "if_reg_secret")
	registrar.httpClient = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(`{
			"agent_id":"agent_123",
			"cluster_id":"cluster_123",
			"agent_token":"if_agent_secret"
		}`), nil
	})}

	identity, err := registrar.Register(context.Background(), "prod", "0.1.0")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if identity.Token != "if_agent_secret" {
		t.Fatalf("unexpected agent token: %s", identity.Token)
	}
	if identity.GatewayURL != "" {
		t.Fatalf("unexpected gateway URL: %s", identity.GatewayURL)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}
