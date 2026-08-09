package version

import (
	"os"
	"strings"
)

var (
	Version = "0.1.0"
	Commit  = "dev"
)

// Runtime returns the version announced by the running agent. Helm sets
// SERVICE_VERSION to the image tag, so prefer it over the build-time fallback.
// This keeps registration, gateway metadata, logs, and tracing aligned even
// when an image is built without injecting -ldflags.
func Runtime() string {
	if value := strings.TrimSpace(os.Getenv("SERVICE_VERSION")); value != "" {
		return value
	}
	return Version
}
