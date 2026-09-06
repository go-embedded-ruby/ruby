package vm

import (
	"errors"
	"testing"
)

// TestDirHomeError covers dirHomeStr's no-HOME branch: when both $HOME
// (osUserHomeDir) and the passwd-database fallback (osCurrentUserHome) fail,
// dirHomeStr raises ArgumentError. Both seams are stubbed so the test does not
// touch the process environment or depend on the runner's passwd database.
func TestDirHomeError(t *testing.T) {
	origHome, origCur := osUserHomeDir, osCurrentUserHome
	defer func() { osUserHomeDir, osCurrentUserHome = origHome, origCur }()
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
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
// database (osCurrentUserHome) when $HOME is unset, as MRI's rb_default_home_dir
// does.
func TestDirHomePasswdFallback(t *testing.T) {
	origHome, origCur := osUserHomeDir, osCurrentUserHome
	defer func() { osUserHomeDir, osCurrentUserHome = origHome, origCur }()
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	osCurrentUserHome = func() (string, error) { return "/passwd/home", nil }
	if got := dirHomeStr(); got != "/passwd/home" {
		t.Fatalf("dirHomeStr fallback = %q, want /passwd/home", got)
	}
}
