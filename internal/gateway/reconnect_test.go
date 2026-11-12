package gateway

import (
	"testing"
	"time"
)

func TestBackoffProgressesAndCaps(t *testing.T) {
	backoff := NewBackoff(time.Second, 4*time.Second)
	first := backoff.Next()
	second := backoff.Next()
	for i := 0; i < 10; i++ {
		_ = backoff.Next()
	}
	if first >= time.Second {
		t.Fatalf("first jittered backoff must stay below 1 second: %s", first)
	}
	if second >= 2*time.Second {
		t.Fatalf("second jittered backoff must stay below 2 seconds: %s", second)
	}
	if backoff.current != 4*time.Second {
		t.Fatalf("backoff did not cap: %s", backoff.current)
	}
	backoff.Reset()
	if backoff.current != 0 {
		t.Fatal("Reset did not clear current backoff")
	}
}
