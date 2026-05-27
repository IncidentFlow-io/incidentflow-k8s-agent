package telemetry

import "go.uber.org/zap"

func NewLogger(level string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	if level == "debug" {
		cfg = zap.NewDevelopmentConfig()
	}
	if err := cfg.Level.UnmarshalText([]byte(level)); err != nil {
		cfg.Level.SetLevel(zap.InfoLevel)
	}
	return cfg.Build()
}
