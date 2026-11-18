// Package version holds build information, set at build time with:
//
//	go build -ldflags "-X github.com/aniket0951/devcraft-cli.git/internal/version.Version=v0.1.0 \
//	  -X github.com/aniket0951/devcraft-cli.git/internal/version.Commit=$(git rev-parse --short HEAD) \
//	  -X github.com/aniket0951/devcraft-cli.git/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
package version

import (
	"fmt"
	"regexp"
	"runtime"
	"runtime/debug"
)

// defaultVersion is used when no version is injected at build time.
const defaultVersion = "0.0.1"

var (
	Version = defaultVersion
	Commit  = "none"
	Date    = "unknown"
)

// releaseTag matches clean semver tags like v1.2.3 or v1.2.3-rc.1, but not
// Go pseudo-versions (v0.0.0-20261118170608-1cb537738911) or "+dirty" builds.
var releaseTag = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)
var pseudoVersion = regexp.MustCompile(`\d{14}-[0-9a-f]{12}`)

func init() {
	// `go install module@vX.Y.Z` builds carry the release tag even without ldflags.
	if Version != defaultVersion {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if v := info.Main.Version; releaseTag.MatchString(v) && !pseudoVersion.MatchString(v) {
		Version = v
	}
}

// String returns a one-line description of the build.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s, %s/%s)", Version, Commit, Date, runtime.GOOS, runtime.GOARCH)
}

// UserAgent returns the User-Agent the CLI sends to the API.
func UserAgent() string {
	return fmt.Sprintf("devcraft-cli/%s (%s/%s)", Version, runtime.GOOS, runtime.GOARCH)
}
