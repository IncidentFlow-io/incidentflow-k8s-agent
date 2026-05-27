package security

import "strings"

const redacted = "[REDACTED]"

func Redact(value string, secrets ...string) string {
	out := value
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		out = strings.ReplaceAll(out, secret, redacted)
	}
	return out
}
