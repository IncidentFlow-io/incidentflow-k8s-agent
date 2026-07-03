package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/incidentflow/incidentflow-k8s-agent/internal/auth"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/commands"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/config"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/gateway"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/kube"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/metrics"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/security"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/version"
	"go.uber.org/zap"
)

type App struct {
	cfg    config.Config
	logger *zap.Logger
}

func New(cfg config.Config, logger *zap.Logger) *App {
	return &App{cfg: cfg, logger: logger}
}

func (a *App) Run(ctx context.Context) error {
	identity, err := a.identity(ctx)
	if err != nil {
		return err
	}
	kubeService, err := kube.NewInClusterService()
	if err != nil {
		return err
	}
	guard := security.NewNamespaceGuard(a.cfg.NamespaceAllowlist)
	limits := security.Limits{
		DefaultTailLines: a.cfg.DefaultTailLines,
		MaxTailLines:     a.cfg.MaxTailLines,
		MaxLogBytes:      a.cfg.MaxLogBytes,
	}
	router := commands.NewRouter(kubeService, guard, limits)
	gatewayURL := identity.GatewayURL
	if gatewayURL == "" {
		gatewayURL = a.cfg.GatewayURL
	}
	client := gateway.NewClient(gateway.Options{
		GatewayURL:      gatewayURL,
		Identity:        identity,
		ClusterName:     a.cfg.ClusterName,
		Version:         version.Version,
		Logger:          a.logger,
		Handler:         router,
		CommandTimeout:  a.cfg.CommandTimeout,
		HeartbeatPeriod: a.cfg.HeartbeatPeriod,
	})

	if srv := a.startMetricsServer(); srv != nil {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
		}()
	}

	return client.Run(ctx)
}

// startMetricsServer exposes the Prometheus /metrics endpoint on the configured
// address. It returns nil (and starts nothing) when the address is empty.
func (a *App) startMetricsServer() *http.Server {
	if a.cfg.MetricsAddr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	srv := &http.Server{
		Addr:              a.cfg.MetricsAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		a.logger.Info("metrics server listening", zap.String("addr", a.cfg.MetricsAddr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.logger.Warn("metrics server stopped", zap.Error(err))
		}
	}()
	return srv
}

func (a *App) identity(ctx context.Context) (auth.Identity, error) {
	if a.cfg.AgentToken != "" {
		return auth.Identity{Token: a.cfg.AgentToken}, nil
	}
	store := auth.NewTokenStore(a.cfg.TokenFile)
	token, err := store.Load()
	if err != nil {
		return auth.Identity{}, fmt.Errorf("load agent token: %w", err)
	}
	if token != "" {
		return auth.Identity{Token: token}, nil
	}
	registrar := auth.NewRegistrar(a.cfg.PlatformURL, a.cfg.RegistrationToken)
	identity, err := registrar.Register(ctx, a.cfg.ClusterName, version.Version)
	if err != nil {
		return auth.Identity{}, fmt.Errorf("register agent: %w", err)
	}
	if err := store.Save(identity.Token); err != nil {
		return auth.Identity{}, fmt.Errorf("persist agent token: %w", err)
	}
	a.logger.Info(
		"registered IncidentFlow agent",
		zap.String("agent_id", identity.AgentID),
		zap.String("cluster_id", identity.ClusterID),
		zap.String("gateway_url", identity.GatewayURL),
	)
	// Use the gateway URL from registration if the platform returned one,
	// falling back to the configured value.
	if identity.GatewayURL == "" {
		identity.GatewayURL = a.cfg.GatewayURL
	}
	return identity, nil
}
