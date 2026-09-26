// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// caughtRaise runs fn and returns the RubyError it raises, or a zero value when
// it does not raise. raise panics with a RubyError, which the VM's Run recovers;
// a direct call to one of the raiseXxx helpers has no VM around it, so the test
// recovers it itself.
func caughtRaise(t *testing.T, fn func()) RubyError {
	t.Helper()
	var got RubyError
	func() {
		defer func() {
			if r := recover(); r != nil {
				re, ok := r.(RubyError)
				if !ok {
					t.Fatalf("panicked with %T (%v), want RubyError", r, r)
				}
				got = re
			}
		}()
		fn()
	}()
	return got
}

// TestGetwdOrFailSeams covers the two failure branches of getwdOrFail that the
// platform cannot produce on its own. On darwin os.Getwd does not fail even for
// a removed working directory (it is getattrlist(".", ATTR_CMN_FULLPATH)), and
// stat(".") keeps working there too, so both are reached through the osGetwd /
// osStat seams. The message shape is rb_syserr_fail's: "<strerror> - getcwd".
func TestGetwdOrFailSeams(t *testing.T) {
	defer func(g func() (string, error), s func(string) (fs.FileInfo, error)) {
		osGetwd, osStat = g, s
	}(osGetwd, osStat)

	// os.Getwd itself failing: the errno it carries names the class, as MRI's
	// rb_syserr_fail(errno, "getcwd") does. EACCES is what getcwd(3) reports when
	// a parent directory has become unsearchable.
	osGetwd = func() (string, error) { return "", syscall.EACCES }
	checkGetcwdFailure(t, "osGetwd failing", caughtRaise(t, func() { getwdOrFail() }),
		"Errno::EACCES", "Permission denied")

	// stat(".") failing: the confirmation cannot be made, so the stat's own errno
	// is reported — the same one getcwd(3) would have failed with.
	osGetwd = os.Getwd
	osStat = func(name string) (fs.FileInfo, error) {
		if name == "." {
			return nil, &fs.PathError{Op: "stat", Path: ".", Err: syscall.ENOENT}
		}
		return os.Stat(name)
	}
	checkGetcwdFailure(t, `stat(".") failing`, caughtRaise(t, func() { getwdOrFail() }),
		"Errno::ENOENT", "No such file or directory")
}

// checkGetcwdFailure asserts what rb_syserr_fail(errno, "getcwd") guarantees on
// every platform — the Errno::Exxx class, and a message ending in the OPERATION
// name "- getcwd" — and, on POSIX only, the strerror sentence in front of it.
//
// The sentence is the C library's, not Ruby's: Windows maps syscall.ENOENT onto
// ERROR_FILE_NOT_FOUND, so errnoStrerror renders it "The system cannot find the
// file specified." there. Asserting the POSIX wording everywhere was an
// assertion about libc rather than about rbgo; narrowing it is not the same as
// loosening a correct one, and the POSIX lane still pins the text exactly.
func checkGetcwdFailure(t *testing.T, label string, got RubyError, wantClass, wantStrerror string) {
	t.Helper()
	if got.Class != wantClass {
		t.Errorf("%s: class %q, want %q", label, got.Class, wantClass)
	}
	if !strings.HasSuffix(got.Message, " - getcwd") {
		t.Errorf("%s: message %q, want it to end in %q", label, got.Message, " - getcwd")
	}
	if runtime.GOOS == "windows" {
		return
	}
	if want := wantStrerror + " - getcwd"; got.Message != want {
		t.Errorf("%s: message %q, want %q", label, got.Message, want)
	}
}

// TestRaiseChdirErrFallbacks covers the two branches of raiseChdirErr that a real
// chdir(2) cannot reach here: an errno with no Errno::Exxx class registered for
// it, which MRI reports as a bare (still rescuable) SystemCallError, and an error
// carrying no errno at all, which keeps ENOENT — what this code asserted for
// every failure before #681.
func TestRaiseChdirErrFallbacks(t *testing.T) {
	// An errno number no platform table claims: no class, so SystemCallError.
	unclaimed := syscall.Errno(0)
	for n := syscall.Errno(1); n < 4096; n++ {
		if _, ok := errnoClasses[int64(n)]; !ok {
			unclaimed = n
			break
		}
	}
	if unclaimed == 0 {
		t.Skip("every errno below 4096 has a class on this platform")
	}
	got := caughtRaise(t, func() { raiseChdirErr(unclaimed, "/p") })
	if got.Class != "SystemCallError" {
		t.Errorf("unclaimed errno %d: class %q, want SystemCallError", unclaimed, got.Class)
	}

	got = caughtRaise(t, func() { raiseChdirErr(errors.New("no errno here"), "/p") })
	if got.Class != "Errno::ENOENT" || got.Message != "No such file or directory @ chdir_path - /p" {
		t.Errorf("errno-less error: got %q / %q, want Errno::ENOENT / %q",
			got.Class, got.Message, "No such file or directory @ chdir_path - /p")
	}
}
