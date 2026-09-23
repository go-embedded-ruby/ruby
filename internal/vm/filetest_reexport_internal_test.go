// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"
)

// TestReexportSingletonsSharesRecords pins the two properties FileTest depends
// on: a re-exported name resolves to the very same *Method record on the target
// (so an alias pair stays an alias), and a name the source does not define is
// skipped rather than installed as a nil record, which would crash at call time.
func TestReexportSingletonsSharesRecords(t *testing.T) {
	src := newClass("Src", nil)
	dst := newClass("Dst", nil)
	m := &Method{name: "zero?", owner: src}
	src.smethods["zero?"] = m
	src.smethods["empty?"] = m // a genuine alias: one record, two names
	src.smethods["nilled"] = nil

	reexportSingletons(dst, src, []string{"zero?", "empty?", "nilled", "absent"})

	if dst.smethods["zero?"] != m || dst.smethods["empty?"] != m {
		t.Fatalf("re-exported methods must share the source record")
	}
	if _, ok := dst.smethods["nilled"]; ok {
		t.Fatalf("a nil source record must not be installed")
	}
	if _, ok := dst.smethods["absent"]; ok {
		t.Fatalf("a name the source does not define must not be installed")
	}
}

// TestFileTestMirrorsFile checks that every name in MRI's
// define_filetest_function list reaches FileTest as File's own method — the
// property that keeps the two surfaces from drifting — and that the zero?/empty?
// alias survives the re-export on both classes.
func TestFileTestMirrorsFile(t *testing.T) {
	vm := New(nil)
	cFile := vm.consts["File"].(*RClass)
	mod := vm.consts["FileTest"].(*RClass)
	if !mod.isModule {
		t.Fatalf("FileTest must be a module")
	}
	for _, name := range fileTestFunctions {
		fm, ok := cFile.smethods[name]
		if !ok {
			t.Errorf("File does not define %s, so FileTest cannot mirror it", name)
			continue
		}
		if mod.smethods[name] != fm {
			t.Errorf("FileTest.%s is not File.%s's own method record", name, name)
		}
	}
	if mod.smethods["zero?"] != mod.smethods["empty?"] {
		t.Errorf("FileTest.zero? must be the same method as FileTest.empty?")
	}
	// MRI 4.0 removed the deprecated FileTest.exists?/File.exists? spelling
	// (ruby/ruby v3_4_0 file.c defines only exist?), so neither carries it.
	if _, ok := mod.smethods["exists?"]; ok {
		t.Errorf("FileTest.exists? was removed in MRI 4.0 and must not be defined")
	}
}

// TestFilePredicateArity covers the single-argument arity MRI's one-path
// predicates enforce (ruby/spec core/filetest/shared/exist.rb asserts it for both
// receivers). Each predicate below reaches oneArg through a different closure —
// access, realAccess, statTest, worldPerm and the four inline ones.
func TestFilePredicateArity(t *testing.T) {
	for _, m := range []string{
		"directory?", "symlink?", "size?", "zero?", "empty?",
		"readable?", "writable?", "executable?",
		"readable_real?", "writable_real?", "executable_real?",
		"pipe?", "socket?", "blockdev?", "chardev?", "setuid?", "setgid?",
		"sticky?", "owned?", "grpowned?", "world_readable?", "world_writable?",
	} {
		for _, recv := range []string{"File", "FileTest"} {
			src := `begin; ` + recv + `.` + m + `("/etc/hosts", "/etc/hosts"); rescue ArgumentError => e; puts e.message; end
begin; ` + recv + `.` + m + `; rescue ArgumentError => e; puts e.message; end`
			out := runFS(t, src)
			if want := "wrong number of arguments (given 2, expected 1)"; !strings.Contains(out, want) {
				t.Errorf("%s.%s with 2 args: got %q, want %q", recv, m, out, want)
			}
			if want := "wrong number of arguments (given 0, expected 1)"; !strings.Contains(out, want) {
				t.Errorf("%s.%s with 0 args: got %q, want %q", recv, m, out, want)
			}
		}
	}
}
