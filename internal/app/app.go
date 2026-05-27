package app

import (
	"context"
	"fmt"

	"github.com/incidentflow/incidentflow-k8s-agent/internal/auth"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/commands"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/config"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/gateway"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/kube"
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
	client := gateway.NewClient(gateway.Options{
		GatewayURL:      a.cfg.GatewayURL,
		Identity:        identity,
		ClusterName:     a.cfg.ClusterName,
		Version:         version.Version,
		Logger:          a.logger,
		Handler:         router,
		CommandTimeout:  a.cfg.CommandTimeout,
		HeartbeatPeriod: a.cfg.HeartbeatPeriod,
	})
	return client.Run(ctx)
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
	a.logger.Info("registered IncidentFlow agent", zap.String("agent_id", identity.AgentID), zap.String("cluster_id", identity.ClusterID))
	return identity, nil
}
