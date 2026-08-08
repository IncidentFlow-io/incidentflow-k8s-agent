package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/incidentflow/incidentflow-k8s-agent/internal/app"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/config"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/observability"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/telemetry"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/version"
	"go.uber.org/zap"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("incidentflow-k8s-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print version and exit")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "IncidentFlow Kubernetes Agent\n")
		fmt.Fprintf(fs.Output(), "Outbound-only, read-only Kubernetes diagnostics agent.\n\n")
		fmt.Fprintf(fs.Output(), "Usage:\n")
		fmt.Fprintf(fs.Output(), "  incidentflow-k8s-agent [flags]\n\n")
		fmt.Fprintf(fs.Output(), "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), "\nRequired environment:\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_PLATFORM_URL        IncidentFlow Platform API base URL.\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_GATEWAY_URL         IncidentFlow Agent Gateway WebSocket URL.\n")
		fmt.Fprintf(fs.Output(), "  K8S_NAMESPACE_NAME               Namespace containing the credentials Secret.\n")
		fmt.Fprintf(fs.Output(), "\nBootstrap (only when credentials are absent):\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_REGISTRATION_TOKEN  One-time registration token; never logged.\n")
		fmt.Fprintf(fs.Output(), "\nPersistent credentials:\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_CREDENTIALS_SECRET_NAME  Secret with agent_id and agent_token\n")
		fmt.Fprintf(fs.Output(), "                                      (default: incidentflow-agent-credentials).\n")
		fmt.Fprintf(fs.Output(), "  The agent reads this Secret first. After bootstrap, pod restarts need no token.\n")
		fmt.Fprintf(fs.Output(), "\nRuntime options:\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_CLUSTER_NAME         Cluster label (default: unknown-cluster).\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_LOG_LEVEL            debug, info, warn, or error (default: info).\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_NAMESPACE_ALLOWLIST  Comma-separated allowed namespaces.\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_DEFAULT_TAIL_LINES   Default pod log lines (default: 200).\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_MAX_TAIL_LINES       Maximum pod log lines (default: 1000).\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_MAX_LOG_BYTES        Maximum log response bytes (default: 524288).\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_COMMAND_TIMEOUT      Per-command timeout (default: 30s).\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_HEARTBEAT_PERIOD     Gateway heartbeat interval (default: 30s).\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_METRICS_ADDR         Prometheus listener (default: :9090).\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "IncidentFlow Kubernetes Agent\nVersion: %s\nCommit:  %s\n", version.Version, version.Commit)
		return 0
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n\nRun with --help to see required environment variables.\n", err)
		return 1
	}
	logger, err := telemetry.NewLogger(cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(stderr, "logger error: %v\n", err)
		return 1
	}
	defer logger.Sync()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tracingCfg := observability.ConfigFromEnv()
	shutdownTracing, err := observability.Init(ctx, tracingCfg, logger)
	if err != nil {
		logger.Warn("otel tracing init failed, continuing without tracing", zap.Error(err))
		shutdownTracing = func(context.Context) {}
	}
	defer shutdownTracing(context.Background())

	if err := app.New(cfg, logger).Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("agent stopped", zap.Error(err))
		return 1
	}
	logger.Info("agent shutdown complete")
	return 0
}
