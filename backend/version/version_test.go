package version

import "testing"

// The version package is the single source every surface reads its identity from, so the
// one piece of logic it owns — deciding whether a build is a development build — is worth
// pinning. The rest is link-injected data with no behaviour to test.

// withVars runs fn with the injected build vars set, restoring them afterwards so tests do
// not leak state into one another.
func withVars(t *testing.T, ver, commit, date string, fn func()) {
	t.Helper()
	ov, oc, od := Version, Commit, Date
	Version, Commit, Date = ver, commit, date
	defer func() { Version, Commit, Date = ov, oc, od }()
	fn()
}

func TestCurrentReportsInjectedIdentity(t *testing.T) {
	withVars(t, "1.2.3", "abc1234", "2026-07-25T00:00:00Z", func() {
		info := Current("rohy")
		if info.Name != "rohy" {
			t.Errorf("name = %q, want rohy", info.Name)
		}
		if info.Version != "1.2.3" || info.Commit != "abc1234" || info.Date != "2026-07-25T00:00:00Z" {
			t.Errorf("identity not carried through: %+v", info)
		}
		if info.Development {
			t.Error("a fully stamped build reported itself as development")
		}
	})
}

// TestUnstampedBuildIsHonest pins 17.3: a plain `go build` (defaults, or a missing stamp)
// must present as development, never as a release. Both signals — the default commit and
// the default date — independently mark it, so a partial stamp cannot pass as clean.
func TestUnstampedBuildIsHonest(t *testing.T) {
	cases := map[string]struct{ commit, date string }{
		"both defaults": {"dev", "unknown"},
		"commit only":   {"dev", "2026-07-25T00:00:00Z"},
		"date only":     {"abc1234", "unknown"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			withVars(t, "1.2.3", c.commit, c.date, func() {
				if !Current("rohy").Development {
					t.Errorf("commit=%q date=%q was not flagged as a development build", c.commit, c.date)
				}
			})
		})
	}
}

// TestDirtyBuildIsAReleaseButCarriesItsMarker pins 17.4: a build from a modified tree has a
// real commit and date, so it is not "development" — but the dirtiness is visible in the
// commit string, which is where the About dialog surfaces it. The marker must survive.
func TestDirtyBuildIsAReleaseButCarriesItsMarker(t *testing.T) {
	withVars(t, "1.2.3", "abc1234-dirty", "2026-07-25T00:00:00Z", func() {
		info := Current("rohy")
		if info.Development {
			t.Error("a dirty build with a real commit and date was hidden as development")
		}
		if info.Commit != "abc1234-dirty" {
			t.Errorf("commit = %q, want the -dirty marker preserved", info.Commit)
		}
	})
}

// TestMissingVersionIsUnreleasedNotBlank pins the consequence of there being NO version literal
// in the source tree: an unstamped build has nothing to report, and it says so in a word rather
// than showing an empty field or inventing a number.
func TestMissingVersionIsUnreleasedNotBlank(t *testing.T) {
	withVars(t, "", "abc1234", "2026-07-25T00:00:00Z", func() {
		info := Current("rohy")
		if info.Version != Unversioned {
			t.Errorf("version = %q, want %q", info.Version, Unversioned)
		}
		// 🔒 A missing version is on its own enough to mark the build. A binary stamped with a
		// commit and a date but no version is not a release, and must not present as one.
		if !info.Development {
			t.Error("a build with no version reported itself as a release")
		}
	})
}

// TestNoVersionLiteralIsCompiledIn pins the rule the package comment states: the release version
// lives in README.md and arrives through the linker. A default here would be a second copy, and
// the one certainty about a hardcoded version is that some release ships the previous one.
func TestNoVersionLiteralIsCompiledIn(t *testing.T) {
	if Version != "" {
		t.Errorf("Version has a compiled-in default of %q — it must be injected at link time", Version)
	}
	if Unversioned == "" {
		t.Error("Unversioned is empty, so an unstamped build would show a blank version")
	}
}
