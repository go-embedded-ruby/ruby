// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	find "github.com/go-ruby-find/find"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// memLister is a deterministic, in-memory find.Lister used to drive findWalk
// without touching the filesystem — covering the engine wiring (order, IsDir,
// Children) and the per-entry error branches under a controlled oracle. dirs maps
// each directory path to its (unsorted) children base names; fail names a path
// whose Children call returns errFail, exercising the error policy.
type memLister struct {
	dirs map[string][]string
	fail string
}

func (m memLister) Exist(path string) bool {
	if _, ok := m.dirs[path]; ok {
		return true
	}
	// A leaf "file" exists if it is listed as a child of some directory.
	for _, kids := range m.dirs {
		for _, k := range kids {
			if find.Join(findDirOf(path), k) == path {
				return true
			}
		}
	}
	return false
}

func (m memLister) IsDir(path string) (bool, error) {
	_, ok := m.dirs[path]
	return ok, nil
}

func (m memLister) Children(dir string) ([]string, error) {
	if dir == m.fail {
		return nil, errFail
	}
	kids := append([]string(nil), m.dirs[dir]...)
	return kids, nil
}

var errFail = errors.New("permission denied")

// findDirOf returns the parent directory of a "/"-joined path (for memLister.Exist).
func findDirOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return path
}

// newTestVM builds a VM with stdout discarded, used by the internal coverage
// tests that call findWalk directly.
func newTestVM() *VM { return New(&bytes.Buffer{}) }

// collectWalk runs findWalk over the in-memory lister and returns the yielded
// paths in visit order, recovering any raised Ruby exception as (class, msg).
func collectWalk(vm *VM, lister find.Lister, roots []string, ignoreError bool) (visited []string, class, msg string) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok {
				class, msg = re.Class, re.Message
				return
			}
			panic(r)
		}
	}()
	findWalk(vm, lister, roots, ignoreError, func(p object.Value) {
		visited = append(visited, object.Kind[*object.String](p).Str())
	})
	return visited, "", ""
}

// TestFindWalkInMemory drives findWalk with a deterministic in-memory Lister,
// asserting MRI's ascending-sorted depth-first order independent of the real
// filesystem.
func TestFindWalkInMemory(t *testing.T) {
	lister := memLister{dirs: map[string][]string{
		"r":     {"z.txt", "a", "b"},
		"r/a":   {"2.txt", "1.txt"},
		"r/b":   {"d"},
		"r/b/d": {"k.txt"},
	}}
	visited, class, _ := collectWalk(newTestVM(), lister, []string{"r"}, true)
	if class != "" {
		t.Fatalf("unexpected raise: %s", class)
	}
	want := []string{"r", "r/a", "r/a/1.txt", "r/a/2.txt", "r/b", "r/b/d", "r/b/d/k.txt", "r/z.txt"}
	if !equalStrings(visited, want) {
		t.Errorf("order: got=%v want=%v", visited, want)
	}
}

// TestFindWalkChildrenError covers the per-entry Children failure under both
// ignore_error policies through the in-memory lister: true skips the failing
// directory's contents and continues; false surfaces the error. Here the lister
// returns a plain (non-RubyError) Go error, so the propagation hits findWalk's
// defensive branch, which renders it as a RuntimeError.
func TestFindWalkChildrenError(t *testing.T) {
	lister := memLister{
		dirs: map[string][]string{
			"r":   {"a", "z.txt"},
			"r/a": {"1.txt"},
		},
		fail: "r/a",
	}

	// ignore_error: true — r/a is yielded but its (failing) listing is skipped.
	visited, class, _ := collectWalk(newTestVM(), lister, []string{"r"}, true)
	if class != "" {
		t.Fatalf("swallow: unexpected raise %s", class)
	}
	if want := []string{"r", "r/a", "r/z.txt"}; !equalStrings(visited, want) {
		t.Errorf("swallow: got=%v want=%v", visited, want)
	}

	// ignore_error: false — the plain Go error reaches findWalk's defensive branch
	// and becomes a RuntimeError carrying the error text.
	_, class, msg := collectWalk(newTestVM(), lister, []string{"r"}, false)
	if class != "RuntimeError" || msg != errFail.Error() {
		t.Errorf("propagate: got=%s:%q want RuntimeError:%q", class, msg, errFail.Error())
	}
}

// TestFindWalkMissingPath covers the *find.MissingPathError -> Errno::ENOENT
// mapping through findWalk directly (the in-memory lister reports the root absent).
func TestFindWalkMissingPath(t *testing.T) {
	lister := memLister{dirs: map[string][]string{}}
	_, class, msg := collectWalk(newTestVM(), lister, []string{"gone"}, true)
	if class != "Errno::ENOENT" || msg != "No such file or directory - gone" {
		t.Errorf("got=%s:%q", class, msg)
	}
}

// TestFindWalkPrune covers ErrPrune: findYield turns a `throw :prune` (Find.prune)
// into find.ErrPrune so the engine prunes the current directory. Here the emit
// callback itself throws :prune for directory "r/a", which must be yielded but not
// descended into.
func TestFindWalkPrune(t *testing.T) {
	lister := memLister{dirs: map[string][]string{
		"r":   {"a", "z.txt"},
		"r/a": {"1.txt"},
	}}
	var visited []string
	findWalk(newTestVM(), lister, []string{"r"}, true, func(p object.Value) {
		s := object.Kind[*object.String](p).Str()
		visited = append(visited, s)
		if s == "r/a" {
			panic(throwSignal{tag: pruneTag, value: object.NilVal()})
		}
	})
	if want := []string{"r", "r/a", "r/z.txt"}; !equalStrings(visited, want) {
		t.Errorf("prune: got=%v want=%v", visited, want)
	}
}

// TestFindYieldRepanic asserts findYield re-panics a signal it does not handle
// (neither a :prune throw nor a Ruby exception) — e.g. a throw to a different tag
// — rather than swallowing it.
func TestFindYieldRepanic(t *testing.T) {
	other := object.Symbol("other")
	defer func() {
		r := recover()
		sig, ok := r.(throwSignal)
		if !ok || sig.tag != object.SymVal(string(other)) {
			t.Fatalf("expected re-panicked throwSignal{other}, got %#v", r)
		}
	}()
	findYield(newTestVM(), func(object.Value) {
		panic(throwSignal{tag: object.SymVal(string(other)), value: object.NilVal()})
	}, "p")
}

// TestFindCallRepanic asserts findCall re-panics a non-RubyError (a real Go bug),
// rather than turning it into a swallow-able Lister error.
func TestFindCallRepanic(t *testing.T) {
	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("expected re-panicked \"boom\", got %#v", r)
		}
	}()
	_, _ = findCall(newTestVM(), func() object.Value { panic("boom") })
}

// TestFindRubyErrorError asserts the findRubyError wrapper renders its underlying
// Ruby exception's Error() string (used when the engine surfaces it).
func TestFindRubyErrorError(t *testing.T) {
	w := findRubyError{err: RubyError{Class: "Errno::EACCES", Message: "denied"}}
	if got := w.Error(); got != "Errno::EACCES: denied" {
		t.Errorf("got %q", got)
	}
}

// TestFindArgsDefaults covers findArgs: a bare path list defaults ignore_error to
// true, and an explicit kwargs hash overrides it (false), with the hash stripped
// from the path list.
func TestFindArgsDefaults(t *testing.T) {
	vm := newTestVM()

	paths, ig := findArgs(vm, []object.Value{object.Wrap(object.NewString("x")), object.Wrap(object.NewString("y"))})
	if !ig || !equalStrings(paths, []string{"x", "y"}) {
		t.Errorf("defaults: paths=%v ignore=%v", paths, ig)
	}

	h := object.NewHash()
	h.Set(object.SymVal(string(object.Symbol("ignore_error"))), object.BoolValue(bool(object.Bool(false))))
	paths, ig = findArgs(vm, []object.Value{object.Wrap(object.NewString("x")), object.Wrap(h)})
	if ig || !equalStrings(paths, []string{"x"}) {
		t.Errorf("kwargs: paths=%v ignore=%v", paths, ig)
	}

	// A trailing hash without :ignore_error leaves the default (true) but is still
	// stripped from the path list.
	empty := object.NewHash()
	paths, ig = findArgs(vm, []object.Value{object.Wrap(object.NewString("x")), object.Wrap(empty)})
	if !ig || !equalStrings(paths, []string{"x"}) {
		t.Errorf("empty-hash: paths=%v ignore=%v", paths, ig)
	}
}

// equalStrings reports slice equality.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
