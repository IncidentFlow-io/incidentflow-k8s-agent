// Package observability initializes OpenTelemetry tracing for the k8s-agent.
package observability

import (
	"context"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const tracerName = "incidentflow.k8s-agent"

// Tracer is the module-level tracer used throughout the agent.
var Tracer trace.Tracer = otel.Tracer(tracerName)

// Config holds tracing configuration read from environment variables.
type Config struct {
	OTLPEndpoint   string
	ServiceName    string
	ServiceVersion string
	Environment    string
	K8sNamespace   string
	Enabled        bool
}

// ConfigFromEnv builds a Config from environment variables.
func ConfigFromEnv() Config {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "otel-collector.observability.svc.cluster.local:4317"
	}
	return Config{
		OTLPEndpoint:   endpoint,
		ServiceName:    "incidentflow-k8s-agent",
		ServiceVersion: envOr("SERVICE_VERSION", "0.0.0"),
		Environment:    envOr("ENV", "unknown"),
		K8sNamespace:   os.Getenv("K8S_NAMESPACE_NAME"),
		Enabled:        os.Getenv("OBSERVABILITY_TRACING_ENABLED") == "true",
	}
}

// Init sets up the global TracerProvider and returns a shutdown function.
// If cfg.Enabled is false it returns a no-op shutdown.
func Init(ctx context.Context, cfg Config, logger *zap.Logger) (shutdown func(context.Context), err error) {
	noop := func(context.Context) {}
	if !cfg.Enabled {
		logger.Info("otel tracing disabled")
		return noop, nil
	}

	conn, err := grpc.NewClient(
		cfg.OTLPEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return noop, err
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return noop, err
	}

	attrs := []attribute.KeyValue{
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
		attribute.String("deployment.environment", cfg.Environment),
	}
	if cfg.K8sNamespace != "" {
		attrs = append(attrs, attribute.String("k8s.namespace.name", cfg.K8sNamespace))
	}

	res, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return noop, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	Tracer = provider.Tracer(tracerName)

	logger.Info("otel tracing enabled",
		zap.String("endpoint", cfg.OTLPEndpoint),
		zap.String("env", cfg.Environment),
	)

	return func(shutdownCtx context.Context) {
		shutdownCtx, cancel := context.WithTimeout(shutdownCtx, 5*time.Second)
		defer cancel()
		if shutdownErr := provider.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Warn("otel provider shutdown error", zap.Error(shutdownErr))
		}
		if connErr := conn.Close(); connErr != nil {
			logger.Warn("otel grpc conn close error", zap.Error(connErr))
		}
	}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
