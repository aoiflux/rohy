// Package version is the single source of truth for what build this is (P13).
//
// The defaults below are what a plain `go build` or `wails build` produces. A release build
// overrides them with linker-injected metadata:
//
//	-ldflags "-X rohy/backend/version.Version=0.0.1 \
//	          -X rohy/backend/version.Commit=$(git rev-parse --short HEAD) \
//	          -X rohy/backend/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// Everything that shows a version — the About dialog, the window, the README's claim —
// reads from here, so the surfaces cannot drift apart (R-V1).
package version

// Injected at link time; the values here are the development defaults.
var (
	// Version is the SemVer release. 0.x means the format and API may still move.
	Version = "0.0.1"
	// Commit is the short git revision the binary was built from.
	Commit = "dev"
	// Date is the RFC3339 UTC build time.
	Date = "unknown"
)

// Info is the build identity handed to the UI.
type Info struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	// Development reports whether this is an unstamped local build, so the UI can say so
	// rather than presenting a dev binary as a release.
	Development bool `json:"development"`
}

// Current returns the running build's identity.
func Current(name string) Info {
	return Info{
		Name:        name,
		Version:     Version,
		Commit:      Commit,
		Date:        Date,
		Development: Commit == "dev" || Date == "unknown",
	}
}
