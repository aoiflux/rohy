// Package version is the single source of truth for what build this is (P13).
//
// 🔒 There is no version number in this file, and there is none anywhere else in the source
// tree or in the build scripts. The release version is stated in exactly one place — README.md —
// and reaches the binary through the linker:
//
//	-ldflags "-X rohy/backend/version.Version=$VERSION \
//	          -X rohy/backend/version.Commit=$(git rev-parse --short HEAD) \
//	          -X rohy/backend/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// A default here would be a second copy of the version, and a second copy goes stale: the one
// certainty about a hardcoded version literal is that some release will ship the previous one.
// So the default is EMPTY, and an unstamped build says `unreleased` rather than claiming a
// number it has no basis for.
//
// Everything that shows a version — the About dialog, the window title, `rohyctl version` —
// reads from here, so the surfaces cannot drift apart (R-V1).
package version

// Unversioned is what Current reports when no version was injected. It is deliberately a word
// rather than a number, so it can never be mistaken for a release and can never go stale.
const Unversioned = "unreleased"

// Injected at link time. Version has no default at all; Commit and Date carry markers that name
// what is missing rather than a plausible-looking value.
var (
	// Version is the SemVer release, injected by the build script from the version passed to it.
	Version string
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
//
// Any one missing stamp marks the build as development. A partial stamp is not a release: a
// binary that knows its version but not its commit cannot be traced back to a tree, which is
// the whole reason the commit is stamped.
func Current(name string) Info {
	v := Version
	if v == "" {
		// Substituted here rather than left blank, so no surface has to decide what an empty
		// version looks like — and none of them can decide differently.
		v = Unversioned
	}
	return Info{
		Name:        name,
		Version:     v,
		Commit:      Commit,
		Date:        Date,
		Development: Version == "" || Commit == "dev" || Date == "unknown",
	}
}
