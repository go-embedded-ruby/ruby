package vm

import (
	"errors"
	"testing"
)

// TestDirHomeError covers dirHomeStr's no-HOME branch via the osUserHomeDir
// seam, without touching the process environment.
func TestDirHomeError(t *testing.T) {
	orig := osUserHomeDir
	defer func() { osUserHomeDir = orig }()
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }

	defer func() {
		re, ok := recover().(RubyError)
		if !ok || re.Class != "ArgumentError" {
			t.Fatalf("want an ArgumentError panic, got %v", re)
		}
	}()
	dirHomeStr()
}
