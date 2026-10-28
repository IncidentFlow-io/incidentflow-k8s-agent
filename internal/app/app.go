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
	health := telemetry.NewHealthState()
	if a.cfg.MetricsAddr != "" {
		metricsSrv := telemetry.NewMetricsServer(a.cfg.MetricsAddr, health)
		go func() {
			a.logger.Info("serving Prometheus metrics and health probes", zap.String("addr", a.cfg.MetricsAddr))
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
	store, err := auth.NewInClusterCredentialStore(a.cfg.Namespace, a.cfg.CredentialsSecretName)
	if err != nil {
		return fmt.Errorf("create credentials store: %w", err)
	}
	var tokenSource auth.RegistrationTokenSource
	if a.cfg.RegistrationTokenSecretName != "" {
		tokenSource, err = auth.NewInClusterRegistrationTokenSource(a.cfg.Namespace, a.cfg.RegistrationTokenSecretName)
		if err != nil {
			return fmt.Errorf("create registration token source: %w", err)
		}
	}
	identity, err := a.identity(ctx, store, tokenSource, runtimeVersion)
	if err != nil {
		return err
	}
	health.SetCredentialsReady(identity.Valid())
	kubeService, err := kube.NewInClusterService()
	if err != nil {
		return err
	}
	guard := security.NewNamespaceGuard(a.cfg.NamespaceAllowlist, a.cfg.NamespaceDenylist)
	limits := security.Limits{
		DefaultTailLines: a.cfg.DefaultTailLines,
		MaxTailLines:     a.cfg.MaxTailLines,
		MaxLogBytes:      a.cfg.MaxLogBytes,
	}
	router := commands.NewRouter(kubeService, guard, limits)
	gatewayURL := a.cfg.GatewayURL
	for {
		client := gateway.NewClient(gateway.Options{
			GatewayURL:        gatewayURL,
			Identity:          identity,
			ClusterName:       a.cfg.ClusterName,
			Version:           runtimeVersion,
			Logger:            a.logger,
			Handler:           router,
			CommandTimeout:    a.cfg.CommandTimeout,
			HeartbeatPeriod:   a.cfg.HeartbeatPeriod,
			OnConnectionState: health.SetGatewayConnected,
			OnHeartbeat:       func() { health.SetGatewayConnected(true) },
		})
		err = client.Run(ctx)
		if !errors.Is(err, gateway.ErrAccessRevoked) {
			return err
		}
		a.logger.Warn("agent access revoked; waiting for externally rotated credentials")
		identity, err = a.waitForUpdatedCredentials(ctx, store, identity, 10*time.Second)
		if err != nil {
			return err
		}
		health.SetCredentialsReady(identity.Valid())
	}
}

func (a *App) identity(ctx context.Context, store auth.CredentialStore, tokenSource auth.RegistrationTokenSource, runtimeVersion string) (auth.Identity, error) {
	identity, err := store.Load(ctx)
	if err != nil {
		return auth.Identity{}, fmt.Errorf("load agent credentials: %w", err)
	}
	if identity.Valid() {
		return identity, nil
	}
	if !a.cfg.CredentialsWriteEnabled {
		return auth.Identity{}, errors.New("agent credentials are absent; provision agent_id and agent_token through an external Secret manager")
	}
	registrationToken, err := a.registrationToken(ctx, tokenSource)
	if err != nil {
		return auth.Identity{}, err
	}
	registrar := auth.NewRegistrar(a.cfg.PlatformURL, registrationToken)
	identity, registered, err := auth.Bootstrap(ctx, store, registrar, registrationToken, a.cfg.ClusterName, runtimeVersion)
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
	return identity, nil
}

// waitForUpdatedCredentials supports rotation without granting the agent write
// access to Kubernetes Secrets. An external controller updates the named Secret
// and the agent retries only after it observes a different complete identity.
func (a *App) waitForUpdatedCredentials(ctx context.Context, store auth.CredentialStore, previous auth.Identity, pollInterval time.Duration) (auth.Identity, error) {
	for {
		if err := ctx.Err(); err != nil {
			return auth.Identity{}, err
		}
		identity, err := store.Load(ctx)
		if err == nil && identity.Valid() && identity.Token != previous.Token {
			return identity, nil
		}
		if !waitContext(ctx, pollInterval) {
			return auth.Identity{}, ctx.Err()
		}
	}
}

func (a *App) registrationToken(ctx context.Context, source auth.RegistrationTokenSource) (string, error) {
	if source == nil {
		return a.cfg.RegistrationToken, nil
	}
	token, err := source.Load(ctx)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (a *App) waitForNewRegistration(ctx context.Context, store auth.CredentialStore, source auth.RegistrationTokenSource, version string, pollInterval time.Duration) (auth.Identity, error) {
	var rejectedToken string
	for {
		if err := ctx.Err(); err != nil {
			return auth.Identity{}, err
		}
		token, err := a.registrationToken(ctx, source)
		if err != nil {
			a.logger.Warn("cannot read registration Secret", zap.Error(err))
		} else if token != "" && token != rejectedToken {
			identity, registerErr := auth.NewRegistrar(a.cfg.PlatformURL, token).Register(ctx, a.cfg.ClusterName, version)
			if registerErr == nil {
				if identity.AgentID == "" || !identity.Valid() {
					rejectedToken = token
					a.logger.Warn("registration returned incomplete credentials; waiting for a new token")
					continue
				}
				for {
					if err := store.Save(ctx, identity); err == nil {
						a.logger.Info("registered IncidentFlow agent", zap.String("agent_id", identity.AgentID), zap.String("cluster_id", identity.ClusterID))
						return identity, nil
					} else {
						a.logger.Warn("cannot persist new agent credentials", zap.Error(err))
					}
					if !waitContext(ctx, pollInterval) {
						return auth.Identity{}, ctx.Err()
					}
				}
			}
			if auth.IsPermanentRegistrationError(registerErr) {
				rejectedToken = token
			}
			a.logger.Warn("agent registration failed; waiting for a valid token", zap.Error(registerErr))
		}
		if !waitContext(ctx, pollInterval) {
			return auth.Identity{}, ctx.Err()
		}
	}
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
