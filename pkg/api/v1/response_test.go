package v1

import "testing"

func TestResponseFactories(t *testing.T) {
	success := Success("req", map[string]string{"ok": "true"})
	if success.Type != MessageTypeResponse || success.Status != StatusSuccess || success.Error != nil {
		t.Fatalf("unexpected success response: %#v", success)
	}
	failure := Failure("req", ErrRBACDenied, "denied")
	if failure.Status != StatusError || failure.Error == nil || failure.Error.Code != ErrRBACDenied {
		t.Fatalf("unexpected failure response: %#v", failure)
	}
}
