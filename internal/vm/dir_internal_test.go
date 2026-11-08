package vm

import (
	"errors"
	"testing"
)

// ⛔ These two tests stubbed osUserHomeDir, a seam over os.UserHomeDir, which
// reports an error BOTH for an unset HOME and for an empty one. That is the
// distinction MRI branches on (`if (!dir)` in rb_default_home_dir is NULL only),
// so the seam could not express the rule and `HOME=""` wrongly took the passwd
// fallback. The seam is now osLookupHome, with LookupEnv's (value, set) shape;
// these moved with it rather than being relaxed.

// TestDirHomeError covers dirHomeStr's no-HOME branch: when HOME is UNSET and
// the passwd-database fallback (osCurrentUserHome) also fails, dirHomeStr raises
// ArgumentError. Both seams are stubbed so the test does not touch the process
// environment or depend on the runner's passwd database.
func TestDirHomeError(t *testing.T) {
	origHome, origCur := osLookupHome, osCurrentUserHome
	defer func() { osLookupHome, osCurrentUserHome = origHome, origCur }()
	osLookupHome = func() (string, bool) { return "", false }
	osCurrentUserHome = func() (string, error) { return "", errors.New("no passwd home") }

	defer func() {
		re, ok := recover().(RubyError)
		if !ok || re.Class != "ArgumentError" {
			t.Fatalf("want an ArgumentError panic, got %v", re)
		}
	}()
	dirHomeStr()
}

// TestDirHomePasswdFallback covers dirHomeStr falling back to the passwd
// database (osCurrentUserHome) when $HOME is UNSET, as MRI's rb_default_home_dir
// does.
func TestDirHomePasswdFallback(t *testing.T) {
	origHome, origCur := osLookupHome, osCurrentUserHome
	defer func() { osLookupHome, osCurrentUserHome = origHome, origCur }()
	osLookupHome = func() (string, bool) { return "", false }
	osCurrentUserHome = func() (string, error) { return "/passwd/home", nil }
	if got := dirHomeStr(); got != "/passwd/home" {
		t.Fatalf("dirHomeStr fallback = %q, want /passwd/home", got)
	}
}

// ⛔ A HOME that is SET must NOT reach the passwd database, whatever it holds.
// This is the row the old seam could not express: with osUserHomeDir, `HOME=""`
// looked identical to no HOME at all and took the fallback, so Dir.home answered
// the passwd home where MRI answers "" — measured on
// `ruby 4.0.7 (2026-09-15 revision 229531a6cf) +PRISM [arm64-darwin25]`:
//
//	HOME=""                Dir.home -> ""
//	HOME="relative/home"   Dir.home -> "relative/home"
//
// The passwd stub below would be a visible wrong answer if the fallback ran.
func TestDirHomeAnswersASetHomeVerbatim(t *testing.T) {
	origHome, origCur := osLookupHome, osCurrentUserHome
	defer func() { osLookupHome, osCurrentUserHome = origHome, origCur }()
	osCurrentUserHome = func() (string, error) { return "/passwd/home", nil }

	for _, tc := range []struct{ name, home string }{
		{"empty", ""},
		{"relative", "relative/home"},
		{"absolute", "/an/absolute/home"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			osLookupHome = func() (string, bool) { return tc.home, true }
			if got := dirHomeStr(); got != tc.home {
				t.Fatalf("dirHomeStr = %q, want %q verbatim (the passwd fallback must not run)", got, tc.home)
			}
		})
	}
}
