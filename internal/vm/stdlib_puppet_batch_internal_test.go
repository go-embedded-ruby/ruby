// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// catchRaise runs fn, returning the RubyError class of any raise it triggers, or
// "" when fn returns normally.
func catchRaise(fn func()) (class string) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok {
				class = re.Class
			} else {
				panic(r)
			}
		}
	}()
	fn()
	return ""
}

// TestSystemTmpdirEnv covers the TMPDIR/TMP/TEMP precedence and the /tmp
// fallback, including the "set but not a directory" skip.
func TestSystemTmpdirEnv(t *testing.T) {
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, "")
	}
	// All unset -> /tmp fallback.
	if got := systemTmpdir(); got != "/tmp" {
		t.Fatalf("fallback: got %q, want /tmp", got)
	}
	// TMPDIR points at a real directory (with a trailing slash to exercise trim).
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir+"/")
	if got := systemTmpdir(); got != dir {
		t.Fatalf("TMPDIR: got %q, want %q", got, dir)
	}
	// TMPDIR points at a non-directory -> skipped, falls through to TMP.
	f := filepath.Join(dir, "afile")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	t.Setenv("TMPDIR", f)
	t.Setenv("TMP", tmp)
	if got := systemTmpdir(); got != tmp {
		t.Fatalf("TMPDIR-nondir skip: got %q, want %q", got, tmp)
	}
}

// TestMakeTmpdirErrors covers makeTmpdir's non-collision error branches via os
// failures, plus the collision-retry and retry-exhaustion paths via the rand
// token seam.
func TestMakeTmpdirErrors(t *testing.T) {
	// Missing base -> ENOENT.
	if c := catchRaise(func() { makeTmpdir("/no-such-base-xyz", "p", "") }); c != "Errno::ENOENT" {
		t.Fatalf("missing base: got %q, want Errno::ENOENT", c)
	}

	// Unwritable base -> EACCES. Inject a permission error through the mkdir
	// seam so the mapping is exercised deterministically on every platform; a
	// chmod-readonly directory is a no-op on Windows and a no-op under root.
	func() {
		orig := osMkdir
		defer func() { osMkdir = orig }()
		osMkdir = func(string, os.FileMode) error { return os.ErrPermission }
		if c := catchRaise(func() { makeTmpdir(t.TempDir(), "p", "") }); c != "Errno::EACCES" {
			t.Fatalf("permission error: got %q, want Errno::EACCES", c)
		}
	}()

	// Collision then success: force a fixed token once (collides with a
	// pre-created dir) then a fresh one.
	base := t.TempDir()
	stamp := time.Now().Format("20060102")
	pid := os.Getpid()
	collide := filepath.Join(base, "p"+stamp+"-"+itoaPid(pid)+"-aaaaaa")
	if err := os.Mkdir(collide, 0o700); err != nil {
		t.Fatal(err)
	}
	origNow, origPid, origTok := tmpNow, osGetpid, secureRandReadSwap(t)
	tmpNow = func() time.Time { return mustParseDay(stamp) }
	osGetpid = func() int { return pid }
	defer func() { tmpNow, osGetpid = origNow, origPid; _ = origTok }()

	calls := 0
	secureRandRead = func(b []byte) (int, error) {
		calls++
		if calls == 1 {
			for i := range b {
				b[i] = 0 // -> 'a' for every byte: token "aaaaaa", collides
			}
		} else {
			for i := range b {
				b[i] = byte(i + 1) // distinct token, succeeds
			}
		}
		return len(b), nil
	}
	got := makeTmpdir(base, "p", "")
	if calls < 2 {
		t.Fatalf("expected a collision retry, got %d rand calls", calls)
	}
	if !dirExistsGo(got) {
		t.Fatalf("makeTmpdir returned a non-existent dir %q", got)
	}

	// Exhaustion: always return the colliding token -> EEXIST after the cap.
	secureRandRead = func(b []byte) (int, error) {
		for i := range b {
			b[i] = 0
		}
		return len(b), nil
	}
	if c := catchRaise(func() { makeTmpdir(base, "p", "") }); c != "Errno::EEXIST" {
		t.Fatalf("exhaustion: got %q, want Errno::EEXIST", c)
	}
}

// TestProcessGroupsError covers the Process.groups failure branch via the seam.
func TestProcessGroupsError(t *testing.T) {
	orig := processGroups
	defer func() { processGroups = orig }()
	processGroups = func() ([]int, error) { return nil, errors.New("boom") }

	vm := New(nil)
	mod := object.Kind[*RClass](vm.consts["Process"])
	out := mod.smethods["groups"].native(vm, mod, nil, nil)
	arr, ok := object.KindOK[*object.Array](out)
	if !ok || len(arr.Elems) != 0 {
		t.Fatalf("groups error: got %#v, want empty Array", out)
	}
}

// TestProcessGroupsSuccess covers the success branch (the supplementary-group
// list is built into an Array) deterministically through the seam, so it is
// exercised on platforms where os.Getgroups is unavailable (e.g. Windows, where
// the real call errors and only the error branch would otherwise run).
func TestProcessGroupsSuccess(t *testing.T) {
	orig := processGroups
	defer func() { processGroups = orig }()
	processGroups = func() ([]int, error) { return []int{0, 20}, nil }

	vm := New(nil)
	mod := object.Kind[*RClass](vm.consts["Process"])
	out := mod.smethods["groups"].native(vm, mod, nil, nil)
	arr, ok := object.KindOK[*object.Array](out)
	if !ok || len(arr.Elems) != 2 || arr.Elems[0] != object.Integer(0) || arr.Elems[1] != object.Integer(20) {
		t.Fatalf("groups success: got %#v, want [0, 20]", out)
	}
}

// TestClockGettimeRealtimeBranch covers the default (non-monotonic) clock branch
// directly, which uses the wall clock.
func TestClockGettimeRealtimeBranch(t *testing.T) {
	vm := New(nil)
	mod := object.Kind[*RClass](vm.consts["Process"])
	out := mod.smethods["clock_gettime"].native(vm, mod, []object.Value{object.Integer(clockRealtime)}, nil)
	if _, ok := object.AsFloatOK(out); !ok {
		t.Fatalf("realtime clock: got %#v, want Float", out)
	}
}

// --- small helpers (kept package-local so the test stands alone) -------------

func itoaPid(p int) string {
	if p == 0 {
		return "0"
	}
	neg := p < 0
	if neg {
		p = -p
	}
	var b [20]byte
	i := len(b)
	for p > 0 {
		i--
		b[i] = byte('0' + p%10)
		p /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func mustParseDay(s string) time.Time {
	tm, err := time.Parse("20060102", s)
	if err != nil {
		panic(err)
	}
	return tm
}

func dirExistsGo(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// secureRandReadSwap saves the original secureRandRead and restores it at test
// end, returning a no-op token so the caller can defer through it.
func secureRandReadSwap(t *testing.T) struct{} {
	orig := secureRandRead
	t.Cleanup(func() { secureRandRead = orig })
	return struct{}{}
}
