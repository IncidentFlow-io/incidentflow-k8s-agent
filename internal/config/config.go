package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultTokenFile       = "/var/lib/incidentflow/agent-token"
	DefaultCommandTimeout  = 30 * time.Second
	DefaultHeartbeatPeriod = 30 * time.Second
	DefaultMaxTailLines    = int64(1000)
	DefaultLogTailLines    = int64(200)
	DefaultMaxLogBytes     = 512 * 1024
)

type Config struct {
	PlatformURL        string
	GatewayURL         string
	RegistrationToken  string
	AgentToken         string
	ClusterName        string
	LogLevel           string
	TokenFile          string
	NamespaceAllowlist []string
	CommandTimeout     time.Duration
	HeartbeatPeriod    time.Duration
	MaxTailLines       int64
	DefaultTailLines   int64
	MaxLogBytes        int64
}

func Load() (Config, error) {
	cfg := Config{
		PlatformURL:        strings.TrimRight(os.Getenv("INCIDENTFLOW_PLATFORM_URL"), "/"),
		GatewayURL:         os.Getenv("INCIDENTFLOW_GATEWAY_URL"),
		RegistrationToken:  os.Getenv("INCIDENTFLOW_REGISTRATION_TOKEN"),
		AgentToken:         os.Getenv("INCIDENTFLOW_AGENT_TOKEN"),
		ClusterName:        getenv("INCIDENTFLOW_CLUSTER_NAME", "unknown-cluster"),
		LogLevel:           getenv("INCIDENTFLOW_LOG_LEVEL", "info"),
		TokenFile:          getenv("INCIDENTFLOW_AGENT_TOKEN_FILE", DefaultTokenFile),
		NamespaceAllowlist: splitCSV(os.Getenv("INCIDENTFLOW_NAMESPACE_ALLOWLIST")),
		CommandTimeout:     getenvDuration("INCIDENTFLOW_COMMAND_TIMEOUT", DefaultCommandTimeout),
		HeartbeatPeriod:    getenvDuration("INCIDENTFLOW_HEARTBEAT_PERIOD", DefaultHeartbeatPeriod),
		MaxTailLines:       getenvInt64("INCIDENTFLOW_MAX_TAIL_LINES", DefaultMaxTailLines),
		DefaultTailLines:   getenvInt64("INCIDENTFLOW_DEFAULT_TAIL_LINES", DefaultLogTailLines),
		MaxLogBytes:        getenvInt64("INCIDENTFLOW_MAX_LOG_BYTES", DefaultMaxLogBytes),
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var missing []string
	if c.PlatformURL == "" {
		missing = append(missing, "INCIDENTFLOW_PLATFORM_URL")
	}
	if c.GatewayURL == "" {
		missing = append(missing, "INCIDENTFLOW_GATEWAY_URL")
	}
	if c.AgentToken == "" && c.RegistrationToken == "" {
		missing = append(missing, "INCIDENTFLOW_AGENT_TOKEN or INCIDENTFLOW_REGISTRATION_TOKEN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}
	if c.MaxTailLines <= 0 {
		return errors.New("INCIDENTFLOW_MAX_TAIL_LINES must be positive")
	}
	if c.DefaultTailLines <= 0 || c.DefaultTailLines > c.MaxTailLines {
		return errors.New("INCIDENTFLOW_DEFAULT_TAIL_LINES must be positive and <= INCIDENTFLOW_MAX_TAIL_LINES")
	}
	if c.MaxLogBytes <= 0 {
		return errors.New("INCIDENTFLOW_MAX_LOG_BYTES must be positive")
	}
	return nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getenvInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
