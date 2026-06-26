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
		fmt.Fprintf(fs.Output(), "IncidentFlow Kubernetes Agent\n\n")
		fmt.Fprintf(fs.Output(), "Usage:\n")
		fmt.Fprintf(fs.Output(), "  incidentflow-k8s-agent [flags]\n\n")
		fmt.Fprintf(fs.Output(), "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), "\nRequired environment:\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_PLATFORM_URL\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_GATEWAY_URL\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_AGENT_TOKEN or INCIDENTFLOW_REGISTRATION_TOKEN\n\n")
		fmt.Fprintf(fs.Output(), "Optional environment:\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_CLUSTER_NAME\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_LOG_LEVEL\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_NAMESPACE_ALLOWLIST\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_AGENT_TOKEN_FILE\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_DEFAULT_TAIL_LINES\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_MAX_TAIL_LINES\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_MAX_LOG_BYTES\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_COMMAND_TIMEOUT\n")
		fmt.Fprintf(fs.Output(), "  INCIDENTFLOW_HEARTBEAT_PERIOD\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "incidentflow-k8s-agent %s (%s)\n", version.Version, version.Commit)
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
