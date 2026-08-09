package telemetry

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewLogger creates structured logs suitable for Kubernetes log collectors.
// Log level controls verbosity; output format is configured independently so
// debug deployments retain machine-readable JSON logs.
func NewLogger(level, format string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.OutputPaths = []string{"stdout"}
	cfg.ErrorOutputPaths = []string{"stderr"}
	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.MessageKey = "message"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	cfg.EncoderConfig.EncodeDuration = zapcore.MillisDurationEncoder
	if strings.EqualFold(format, "console") {
		cfg.Encoding = "console"
	}
	if err := cfg.Level.UnmarshalText([]byte(level)); err != nil {
		cfg.Level.SetLevel(zap.InfoLevel)
	}
	return cfg.Build()
}
