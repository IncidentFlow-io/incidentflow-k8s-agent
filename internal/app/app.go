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
	"github.com/incidentflow/incidentflow-k8s-agent/internal/security"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/telemetry"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/version"
	"go.uber.org/zap"
)

type App struct {
	cfg    config.Config
	logger *zap.Logger
}

func New(cfg config.Config, logger *zap.Logger) *App {
	return &App{cfg: cfg, logger: logger.With(zap.String("component", "app"))}
}

func (a *App) Run(ctx context.Context) error {
	runtimeVersion := version.Runtime()
	identity, err := a.identity(ctx, runtimeVersion)
	if err != nil {
		return err
	}
	kubeService, err := kube.NewInClusterService()
	if err != nil {
		return err
	}
	if a.cfg.MetricsAddr != "" {
		metricsSrv := telemetry.NewMetricsServer(a.cfg.MetricsAddr)
		go func() {
			a.logger.Info("serving Prometheus metrics", zap.String("addr", a.cfg.MetricsAddr))
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				a.logger.Warn("metrics server stopped", zap.Error(err))
			}
		}()
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = metricsSrv.Shutdown(shutdownCtx)
		}()
	}
	guard := security.NewNamespaceGuard(a.cfg.NamespaceAllowlist, a.cfg.NamespaceDenylist)
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
		Version:         runtimeVersion,
		Logger:          a.logger,
		Handler:         router,
		CommandTimeout:  a.cfg.CommandTimeout,
		HeartbeatPeriod: a.cfg.HeartbeatPeriod,
	})
	return client.Run(ctx)
}

func (a *App) identity(ctx context.Context, runtimeVersion string) (auth.Identity, error) {
	store, err := auth.NewInClusterCredentialStore(a.cfg.Namespace, a.cfg.CredentialsSecretName)
	if err != nil {
		return auth.Identity{}, fmt.Errorf("create credentials store: %w", err)
	}
	identity, err := store.Load(ctx)
	if err != nil {
		return auth.Identity{}, fmt.Errorf("load agent credentials: %w", err)
	}
	if identity.Valid() {
		return identity, nil
	}
	registrar := auth.NewRegistrar(a.cfg.PlatformURL, a.cfg.RegistrationToken)
	identity, registered, err := auth.Bootstrap(ctx, store, registrar, a.cfg.RegistrationToken, a.cfg.ClusterName, runtimeVersion)
	if err != nil {
		return auth.Identity{}, fmt.Errorf("register agent: %w", err)
	}
	if registered {
		a.logger.Info(
			"registered IncidentFlow agent",
			zap.String("agent_id", identity.AgentID),
			zap.String("cluster_id", identity.ClusterID),
			zap.String("gateway_url", identity.GatewayURL),
		)
	}
	// Use the gateway URL from registration if the platform returned one,
	// falling back to the configured value.
	if identity.GatewayURL == "" {
		identity.GatewayURL = a.cfg.GatewayURL
	}
	return identity, nil
}
