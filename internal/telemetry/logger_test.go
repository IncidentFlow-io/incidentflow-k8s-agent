package telemetry

import "testing"

func TestNewLoggerSupportsJSONAtDebugLevel(t *testing.T) {
	logger, err := NewLogger("debug", "json")
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	defer logger.Sync()
}

func TestNewLoggerSupportsConsoleFormat(t *testing.T) {
	logger, err := NewLogger("info", "console")
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	defer logger.Sync()
}
