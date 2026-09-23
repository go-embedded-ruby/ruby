// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// flockScratch writes a file for the cases below and returns its
// forward-slashed path.
func flockScratch(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "locked.txt")
	if err := os.WriteFile(p, []byte("1234567890"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return slash(p)
}

// TestWave28FlockReal drives File#flock against the real flock(2) on the
// platforms that have one. rb_file_flock (file.c) answers 0 for a lock taken and
// 0 for the unlock, and a second, independent open file description asking for an
// incompatible lock with LOCK_NB gets `false` rather than an exception — the one
// thing a shared lock does not produce, since two shared locks are compatible.
// Asserted against MRI ruby 4.0.5.
func TestWave28FlockReal(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOARCH == "wasm" {
		t.Skip("no flock(2) on this platform; the seam reports ENOTSUP instead")
	}
	p := flockScratch(t)
	cases := []struct{ src, want string }{
		{`f = File.open("` + p + `", "w+"); p f.flock(File::LOCK_EX); p f.flock(File::LOCK_UN); f.close`, "0\n0\n"},
		{`f = File.open("` + p + `", "w+"); p f.flock(File::LOCK_SH); p f.flock(File::LOCK_UN); f.close`, "0\n0\n"},
		// Two shared locks on the same file are compatible, so neither is refused.
		{`a = File.open("` + p + `", "r"); b = File.open("` + p + `", "r"); a.flock(File::LOCK_SH); p b.flock(File::LOCK_SH | File::LOCK_NB); p b.flock(File::LOCK_UN); a.flock(File::LOCK_UN); a.close; b.close`, "0\n0\n"},
		// An exclusive lock refuses a second one; with LOCK_NB that is `false`.
		{`a = File.open("` + p + `", "w+"); b = File.open("` + p + `", "w"); a.flock(File::LOCK_EX); p b.flock(File::LOCK_EX | File::LOCK_NB); a.flock(File::LOCK_UN); a.close; b.close`, "false\n"},
		// Unlocking a stream that never locked is still 0 — there is no descriptor
		// to act on, and MRI's flock(LOCK_UN) on an unlocked fd succeeds too.
		{`f = File.open("` + p + `", "r"); p f.flock(File::LOCK_UN); f.close`, "0\n"},
		// Closing the stream drops the lock, so the next taker is not refused.
		{`a = File.open("` + p + `", "w+"); a.flock(File::LOCK_EX); a.close; b = File.open("` + p + `", "w"); p b.flock(File::LOCK_EX | File::LOCK_NB); b.close`, "0\n"},
		{`f = File.open("` + p + `", "r"); f.close; begin; f.flock(File::LOCK_EX); rescue => e; p [e.class, e.message]; end`, "[IOError, \"closed stream\"]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestWave28FlockRetriesWithoutLockNB covers the loop rb_file_flock spells out:
// WITHOUT LOCK_NB a refusal is not an answer, it is a wait — MRI sleeps 0.1 s and
// calls flock(2) again, rather than blocking inside the syscall, which is what
// keeps the wait interruptible. The seam counts the calls so the retry is
// observed rather than assumed.
func TestWave28FlockRetriesWithoutLockNB(t *testing.T) {
	p := flockScratch(t)
	calls := 0
	restore := swapFlockSeam(func(string) (int, error) { return 7, nil },
		func(fd, op int) error {
			calls++
			if calls < 3 {
				return syscall.EAGAIN
			}
			return nil
		},
		func(int) error { return nil })
	defer restore()

	if got := runFS(t, `f = File.open("`+p+`", "r"); p f.flock(File::LOCK_EX); f.close`); got != "0\n" {
		t.Fatalf("got %q, want %q", got, "0\n")
	}
	if calls != 3 {
		t.Errorf("flock(2) called %d times, want 3 (two refusals then the grant)", calls)
	}
}

// TestWave28FlockErrnoMapping covers the failures rb_file_flock does NOT retry:
// each is rb_syserr_fail_path, naming the file. The unsupported platforms reach
// the last case through their own seam, which is why the stub returns exactly
// what they do.
func TestWave28FlockErrnoMapping(t *testing.T) {
	p := flockScratch(t)
	for _, c := range []struct {
		err  error
		want string
	}{
		{syscall.EBADF, "[Errno::EBADF, \"Bad file descriptor @ rb_file_flock - PATH\"]\n"},
		{syscall.EINVAL, "[Errno::EINVAL, \"Invalid argument @ rb_file_flock - PATH\"]\n"},
		{syscall.ENOTSUP, "[Errno::ENOTSUP, \"Operation not supported @ rb_file_flock - PATH\"]\n"},
	} {
		err := c.err
		restore := swapFlockSeam(func(string) (int, error) { return 7, nil },
			func(int, int) error { return err },
			func(int) error { return nil })
		src := `f = File.open("` + p + `", "r"); begin; f.flock(File::LOCK_EX); rescue => e; p [e.class, e.message]; end`
		got := runFS(t, src)
		restore()
		want := replaceAllPath(c.want, p)
		if got != want {
			t.Errorf("errno %v: got %q, want %q", err, got, want)
		}
	}
	// A LOCK_UN whose flock(2) fails reports the same way rather than silently
	// dropping the descriptor.
	restore := swapFlockSeam(func(string) (int, error) { return 7, nil },
		func(_, op int) error {
			if op&flockUN != 0 {
				return syscall.EBADF
			}
			return nil
		},
		func(int) error { return nil })
	src := `f = File.open("` + p + `", "r"); f.flock(File::LOCK_SH); begin; f.flock(File::LOCK_UN); rescue => e; p [e.class, e.message]; end`
	got := runFS(t, src)
	restore()
	want := replaceAllPath("[Errno::EBADF, \"Bad file descriptor @ rb_file_flock - PATH\"]\n", p)
	if got != want {
		t.Errorf("unlock failure: got %q, want %q", got, want)
	}
}

// TestWave28FlockOpenFailure covers the other half of lockDescriptor: the
// descriptor a lock needs cannot always be opened, and that failure is the
// ordinary rb_sysopen one, named for the path.
func TestWave28FlockOpenFailure(t *testing.T) {
	p := flockScratch(t)
	restore := swapFlockSeam(func(string) (int, error) { return 0, os.ErrPermission },
		func(int, int) error { return nil },
		func(int) error { return nil })
	defer restore()
	src := `f = File.open("` + p + `", "r"); begin; f.flock(File::LOCK_EX); rescue => e; p [e.class, e.message]; end`
	want := replaceAllPath("[Errno::EACCES, \"Permission denied @ rb_sysopen - PATH\"]\n", p)
	if got := runFS(t, src); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// swapFlockSeam installs stub flock seams and returns the restore function.
func swapFlockSeam(open func(string) (int, error), lock func(int, int) error, closeFd func(int) error) func() {
	oOpen, oLock, oClose, oAgain := lockFdFn, flockFn, closeFdFn, flockAgainErrs
	lockFdFn, flockFn, closeFdFn = open, lock, closeFd
	// The retry list is per platform and empty where there is no flock(2); the
	// stub speaks EAGAIN, so give every platform that one entry. EAGAIN rather
	// than EWOULDBLOCK because wasip1 defines no EWOULDBLOCK at all, and on unix
	// the real list already carries EAGAIN beside it.
	flockAgainErrs = []error{syscall.EAGAIN}
	return func() { lockFdFn, flockFn, closeFdFn, flockAgainErrs = oOpen, oLock, oClose, oAgain }
}

// replaceAllPath substitutes the scratch path into an expected message.
func replaceAllPath(want, p string) string {
	out := ""
	for {
		i := indexOf(want, "PATH")
		if i < 0 {
			return out + want
		}
		out += want[:i] + p
		want = want[i+4:]
	}
}

// indexOf is strings.Index, kept local so this file needs no extra import.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
