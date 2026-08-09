package version

import "testing"

func TestRuntimeUsesServiceVersion(t *testing.T) {
	t.Setenv("SERVICE_VERSION", "dev-v1.0.13")

	if got := Runtime(); got != "dev-v1.0.13" {
		t.Fatalf("Runtime() = %q, want SERVICE_VERSION", got)
	}
}

func TestRuntimeFallsBackToBuildVersion(t *testing.T) {
	t.Setenv("SERVICE_VERSION", "  ")

	if got := Runtime(); got != Version {
		t.Fatalf("Runtime() = %q, want build version %q", got, Version)
	}
}
