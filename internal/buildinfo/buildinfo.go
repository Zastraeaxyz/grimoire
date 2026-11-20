// Package buildinfo reports which build the bot is running.
package buildinfo

import (
	"os"
	"strings"
)

// Version returns the build identifier for this process. Operators set
// BUILD_VERSION at deploy time; it can hold a commit hash, a release version,
// or any other label. Deployments connected to GitHub fall back to the commit
// SHA Railway provides.
func Version() string {
	if v := strings.TrimSpace(os.Getenv("BUILD_VERSION")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("RAILWAY_GIT_COMMIT_SHA")); v != "" {
		return v
	}
	return "unknown"
}
