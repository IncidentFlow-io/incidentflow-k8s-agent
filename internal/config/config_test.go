package config

import "testing"

func TestLoadRequiresGatewayAndPlatform(t *testing.T) {
	_, err := Load()
	if err == nil {
		t.Fatal("expected missing required configuration error")
	}
}

func TestLoad(t *testing.T) {
	t.Setenv("INCIDENTFLOW_PLATFORM_URL", "https://api.example.com/")
	t.Setenv("INCIDENTFLOW_GATEWAY_URL", "wss://gateway.example.com/ws")
	t.Setenv("INCIDENTFLOW_NAMESPACE_ALLOWLIST", "prod, staging")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PlatformURL != "https://api.example.com" {
		t.Fatalf("PlatformURL = %q", cfg.PlatformURL)
	}
	if cfg.LogFormat != DefaultLogFormat {
		t.Fatalf("LogFormat = %q, want %q", cfg.LogFormat, DefaultLogFormat)
	}
	if len(cfg.NamespaceAllowlist) != 2 {
		t.Fatalf("NamespaceAllowlist length = %d", len(cfg.NamespaceAllowlist))
	}
}
