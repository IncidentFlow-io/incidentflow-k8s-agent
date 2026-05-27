package security

import (
	"strings"
	"testing"
)

func TestNamespaceGuard(t *testing.T) {
	guard := NewNamespaceGuard([]string{"prod"})
	if err := guard.Check("prod"); err != nil {
		t.Fatalf("prod should be allowed: %v", err)
	}
	if err := guard.Check("dev"); err == nil {
		t.Fatal("dev should be denied by allowlist")
	}
	if err := guard.Check("kube-system"); err == nil {
		t.Fatal("kube-system should always be denied")
	}
}

func TestLimits(t *testing.T) {
	limits := Limits{DefaultTailLines: 200, MaxTailLines: 1000, MaxLogBytes: 5}
	if got := limits.TailLines(0); got != 200 {
		t.Fatalf("TailLines(0) = %d", got)
	}
	if got := limits.TailLines(2000); got != 1000 {
		t.Fatalf("TailLines(2000) = %d", got)
	}
	logs, truncated := limits.TruncateLog("123456")
	if !truncated || logs != "12345" {
		t.Fatalf("TruncateLog() = %q, %v", logs, truncated)
	}
}

func TestRedact(t *testing.T) {
	out := Redact("Authorization: Bearer secret", "secret")
	if strings.Contains(out, "secret") {
		t.Fatalf("secret was not redacted: %q", out)
	}
}
