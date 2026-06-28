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

// callFU invokes a FileUtils module function by name.
func callFU(t *testing.T, vm *VM, name string, args []object.Value) object.Value {
	t.Helper()
	mod := vm.consts["FileUtils"].(*RClass)
	m := mod.smethods[name]
	if m == nil {
		t.Fatalf("FileUtils.%s not found", name)
	}
	return m.native(vm, mod, args, nil)
}

func TestFileUtilsSuccess(t *testing.T) {
	vm := New(nil)
	d := t.TempDir()
	s := func(x string) object.Value { return object.NewString(x) }
	join := func(parts ...string) string { return filepath.Join(append([]string{d}, parts...)...) }

	// mkdir_p (single + array form) and its aliases.
	if r := callFU(t, vm, "mkdir_p", []object.Value{s(join("a", "b"))}); r.ToS() != join("a", "b") {
		t.Fatalf("mkdir_p return: %v", r)
	}
	if fi, err := os.Stat(join("a", "b")); err != nil || !fi.IsDir() {
		t.Fatalf("mkdir_p did not create dir")
	}
	callFU(t, vm, "mkpath", []object.Value{s(join("c"))})
	callFU(t, vm, "makedirs", []object.Value{s(join("e"))})
	callFU(t, vm, "mkdir_p", []object.Value{&object.Array{Elems: []object.Value{s(join("f")), s(join("g"))}}})
	if fi, err := os.Stat(join("g")); err != nil || !fi.IsDir() {
		t.Fatalf("mkdir_p array form failed")
	}

	// touch: create-then-update path.
	tf := join("t.txt")
	callFU(t, vm, "touch", []object.Value{s(tf)})
	if _, err := os.Stat(tf); err != nil {
		t.Fatalf("touch did not create file")
	}
	callFU(t, vm, "touch", []object.Value{s(tf)}) // exists -> Chtimes path
	if _, err := os.Stat(tf); err != nil {
		t.Fatalf("touch update failed")
	}

	// cp / copy.
	src := join("s.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	callFU(t, vm, "cp", []object.Value{s(src), s(join("d.txt"))})
	if b, _ := os.ReadFile(join("d.txt")); string(b) != "hi" {
		t.Fatalf("cp content wrong")
	}
	callFU(t, vm, "copy", []object.Value{s(src), s(join("d2.txt"))})

	// mv / move.
	callFU(t, vm, "mv", []object.Value{s(join("d.txt")), s(join("d3.txt"))})
	if _, err := os.Stat(join("d3.txt")); err != nil {
		t.Fatalf("mv failed")
	}
	callFU(t, vm, "move", []object.Value{s(join("d2.txt")), s(join("d4.txt"))})

	// rm.
	callFU(t, vm, "rm", []object.Value{s(join("d3.txt"))})
	if _, err := os.Stat(join("d3.txt")); !os.IsNotExist(err) {
		t.Fatalf("rm did not remove")
	}

	// rm_f ignores a missing file (force form) and its alias.
	callFU(t, vm, "rm_f", []object.Value{s(join("missing"))})
	callFU(t, vm, "safe_unlink", []object.Value{s(join("missing2"))})
	// rm_f on an existing file removes it.
	callFU(t, vm, "rm_f", []object.Value{s(join("d4.txt"))})

	// rm_rf and its aliases.
	callFU(t, vm, "rm_rf", []object.Value{s(join("a"))})
	if _, err := os.Stat(join("a")); !os.IsNotExist(err) {
		t.Fatalf("rm_rf did not remove tree")
	}
	callFU(t, vm, "rmtree", []object.Value{s(join("c"))})
	callFU(t, vm, "rm_r", []object.Value{s(join("e"))})
}

func TestFileUtilsNotImplemented(t *testing.T) {
	vm := New(nil)
	for _, m := range []string{"chown", "chown_R", "chmod_R", "copy_stream",
		"remove_entry_secure", "uptodate?", "ln", "ln_s", "ln_sf", "compare_file", "cp_r"} {
		got := catchRaise(func() { callFU(t, vm, m, nil) })
		if got != "NotImplementedError" {
			t.Fatalf("FileUtils.%s: got %q, want NotImplementedError", m, got)
		}
	}
}

// fuFail swaps every FileUtils os seam to a failing stub for the test's
// duration, so each error->Errno mapping is exercised deterministically without
// relying on platform-specific permission behaviour.
func fuFail(t *testing.T) {
	t.Helper()
	o1, o2, o3, o4, o5, o6, o7 :=
		fuMkdirAll, fuRemoveAll, fuRemove, fuRename, fuReadFile, fuWriteFile, fuChtimes
	t.Cleanup(func() {
		fuMkdirAll, fuRemoveAll, fuRemove, fuRename, fuReadFile, fuWriteFile, fuChtimes =
			o1, o2, o3, o4, o5, o6, o7
	})
	perm := os.ErrPermission
	fuMkdirAll = func(string, os.FileMode) error { return perm }
	fuRemoveAll = func(string) error { return perm }
	fuRemove = func(string) error { return perm }
	fuRename = func(string, string) error { return perm }
	fuReadFile = func(string) ([]byte, error) { return nil, perm }
	fuWriteFile = func(string, []byte, os.FileMode) error { return perm }
	fuChtimes = func(string, time.Time, time.Time) error { return perm }
}

func TestFileUtilsErrorBranches(t *testing.T) {
	s := func(x string) object.Value { return object.NewString(x) }
	cases := []struct {
		name string
		args []object.Value
		want string
	}{
		{"mkdir_p", []object.Value{s("p")}, "Errno::EACCES"},
		{"rm_rf", []object.Value{s("p")}, "Errno::EACCES"},
		{"rm_f", []object.Value{s("p")}, "Errno::EACCES"}, // a non-ENOENT failure surfaces
		{"rm", []object.Value{s("p")}, "Errno::ENOENT"},   // any failure -> ENOENT
		{"mv", []object.Value{s("a"), s("b")}, "Errno::ENOENT"},
		{"cp", []object.Value{s("a"), s("b")}, "Errno::ENOENT"}, // read failure
	}
	for _, c := range cases {
		vm := New(nil)
		fuFail(t)
		got := catchRaise(func() { callFU(t, vm, c.name, c.args) })
		if got != c.want {
			t.Fatalf("%s error: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFileUtilsRmFIgnoresNotExist(t *testing.T) {
	// rm_f swallows a genuine ENOENT (the "force" contract) instead of raising.
	o := fuRemove
	t.Cleanup(func() { fuRemove = o })
	fuRemove = func(string) error { return os.ErrNotExist }
	vm := New(nil)
	got := catchRaise(func() { callFU(t, vm, "rm_f", []object.Value{object.NewString("p")}) })
	if got != "" {
		t.Fatalf("rm_f on ENOENT raised %q, want no raise", got)
	}
}

func TestFileUtilsCpWriteError(t *testing.T) {
	// cp where the read succeeds but the write fails -> EACCES on the dest.
	or, ow := fuReadFile, fuWriteFile
	t.Cleanup(func() { fuReadFile, fuWriteFile = or, ow })
	fuReadFile = func(string) ([]byte, error) { return []byte("data"), nil }
	fuWriteFile = func(string, []byte, os.FileMode) error { return os.ErrPermission }
	vm := New(nil)
	got := catchRaise(func() {
		callFU(t, vm, "cp", []object.Value{object.NewString("a"), object.NewString("b")})
	})
	if got != "Errno::EACCES" {
		t.Fatalf("cp write error: got %q, want Errno::EACCES", got)
	}
}

func TestFileUtilsTouchErrors(t *testing.T) {
	s := func(x string) object.Value { return object.NewString(x) }

	// touch where the file is absent and the create write fails -> EACCES.
	func() {
		os1, ow := fuStat, fuWriteFile
		t.Cleanup(func() { fuStat, fuWriteFile = os1, ow })
		fuStat = func(string) (os.FileInfo, error) { return nil, errors.New("absent") }
		fuWriteFile = func(string, []byte, os.FileMode) error { return os.ErrPermission }
		vm := New(nil)
		got := catchRaise(func() { callFU(t, vm, "touch", []object.Value{s("p")}) })
		if got != "Errno::EACCES" {
			t.Fatalf("touch create error: got %q, want Errno::EACCES", got)
		}
	}()

	// touch where the file exists but Chtimes fails -> EACCES.
	func() {
		os1, oc := fuStat, fuChtimes
		t.Cleanup(func() { fuStat, fuChtimes = os1, oc })
		fuStat = func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil }
		fuChtimes = func(string, time.Time, time.Time) error { return os.ErrPermission }
		vm := New(nil)
		got := catchRaise(func() { callFU(t, vm, "touch", []object.Value{s("p")}) })
		if got != "Errno::EACCES" {
			t.Fatalf("touch chtimes error: got %q, want Errno::EACCES", got)
		}
	}()
}

// fakeFileInfo is a minimal os.FileInfo standing in for an existing file so the
// touch "exists" branch (Chtimes) is taken without any real filesystem state.
type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "f" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }
