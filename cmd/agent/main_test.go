package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpDoesNotRequireConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run --help exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "INCIDENTFLOW_PLATFORM_URL") {
		t.Fatalf("help output did not include environment docs: %q", stderr.String())
	}
}

func TestVersionDoesNotRequireConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run --version exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "incidentflow-k8s-agent") {
		t.Fatalf("version output = %q", stdout.String())
	}
}
