// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestTmpdirStdlib exercises the tmpdir standard library (Dir.tmpdir,
// Dir.mktmpdir in both block and return-value forms, prefix/suffix and explicit
// base arguments). Each behaviour is verified against MRI 4.0.5.
func TestTmpdirStdlib(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// require returns true the first time, false thereafter.
		{"require_first", `p require "tmpdir"`, "true\n"},
		{"require_second", `require "tmpdir"; p require "tmpdir"`, "false\n"},
		// Dir.tmpdir is a String.
		{"tmpdir_class", `require "tmpdir"; p Dir.tmpdir.class`, "String\n"},
		// No-block form creates a real directory with the default "d" prefix.
		{"mktmpdir_default", `require "tmpdir"
d = Dir.mktmpdir
ok = Dir.exist?(d) && File.basename(d).start_with?("d")
Dir.rmdir(d)
p ok`, "true\n"},
		// String prefix.
		{"mktmpdir_prefix", `require "tmpdir"
d = Dir.mktmpdir("pre")
ok = File.basename(d).start_with?("pre")
Dir.rmdir(d)
p ok`, "true\n"},
		// [prefix, suffix] array.
		{"mktmpdir_prefix_suffix", `require "tmpdir"
d = Dir.mktmpdir(["pre", ".suf"])
b = File.basename(d)
ok = b.start_with?("pre") && b.end_with?(".suf")
Dir.rmdir(d)
p ok`, "true\n"},
		// nil prefix falls back to the default.
		{"mktmpdir_nil_prefix", `require "tmpdir"
d = Dir.mktmpdir(nil)
ok = File.basename(d).start_with?("d")
Dir.rmdir(d)
p ok`, "true\n"},
		// Explicit base directory (Dir.tmpdir exists on every platform; a
		// hardcoded "/tmp" does not on Windows).
		{"mktmpdir_base", `require "tmpdir"
base = Dir.tmpdir
d = Dir.mktmpdir("z", base)
ok = d.start_with?(base)
Dir.rmdir(d)
p ok`, "true\n"},
		// nil base falls back to the system tmpdir.
		{"mktmpdir_nil_base", `require "tmpdir"
d = Dir.mktmpdir("z", nil)
ok = Dir.exist?(d)
Dir.rmdir(d)
p ok`, "true\n"},
		// Block form returns the block value.
		{"mktmpdir_block_return", `require "tmpdir"; p Dir.mktmpdir { |d| 99 }`, "99\n"},
		// Block form removes the (possibly non-empty) directory afterwards.
		{"mktmpdir_block_cleanup", `require "tmpdir"
saved = nil
Dir.mktmpdir { |d| File.write(File.join(d, "x"), "hi"); saved = d }
p Dir.exist?(saved)`, "false\n"},
		// Block form with explicit base.
		{"mktmpdir_block_base", `require "tmpdir"
base = Dir.tmpdir
saved = nil
Dir.mktmpdir("z", base) { |d| saved = d }
p saved.start_with?(base)`, "true\n"},
		// Array prefix with only one element (no suffix).
		{"mktmpdir_array_one", `require "tmpdir"
d = Dir.mktmpdir(["only"])
ok = File.basename(d).start_with?("only")
Dir.rmdir(d)
p ok`, "true\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runProg(t, tt.src, nil)
			if err != nil {
				t.Fatalf("src=%q: %v", tt.src, err)
			}
			if out != tt.want {
				t.Fatalf("src=%q: got %q, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestTmpdirError covers the error path: a missing base raises Errno::ENOENT,
// matching MRI.
func TestTmpdirError(t *testing.T) {
	class, _ := evalErr(t, `require "tmpdir"; Dir.mktmpdir(nil, "/no-such-base-dir-xyz")`)
	if class != "Errno::ENOENT" {
		t.Fatalf("got %q, want Errno::ENOENT", class)
	}
}

// TestProcessStdlib exercises the Process module (identity queries, clock_gettime
// with each unit, the maxgroups accessor and the CLOCK_* constants). Class-level
// assertions avoid depending on the host's actual ids; verified against MRI.
func TestProcessStdlib(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"pid", `p Process.pid.class`, "Integer\n"},
		{"ppid", `p Process.ppid.class`, "Integer\n"},
		{"uid", `p Process.uid.class`, "Integer\n"},
		{"euid", `p Process.euid.class`, "Integer\n"},
		{"gid", `p Process.gid.class`, "Integer\n"},
		{"egid", `p Process.egid.class`, "Integer\n"},
		{"groups", `p Process.groups.class`, "Array\n"},
		{"maxgroups", `p Process.maxgroups`, "16\n"},
		{"maxgroups_set", `p(Process.maxgroups = 1024)`, "1024\n"},
		{"clock_realtime_const", `p Process::CLOCK_REALTIME`, "0\n"},
		{"clock_monotonic_const", `p Process::CLOCK_MONOTONIC`, "6\n"},
		{"clock_default", `p Process.clock_gettime(Process::CLOCK_MONOTONIC).class`, "Float\n"},
		{"clock_realtime", `p Process.clock_gettime(Process::CLOCK_REALTIME).class`, "Float\n"},
		{"clock_float_second", `p Process.clock_gettime(Process::CLOCK_MONOTONIC, :float_second).class`, "Float\n"},
		{"clock_second", `p Process.clock_gettime(Process::CLOCK_MONOTONIC, :second).class`, "Integer\n"},
		{"clock_millisecond", `p Process.clock_gettime(Process::CLOCK_MONOTONIC, :millisecond).class`, "Integer\n"},
		{"clock_microsecond", `p Process.clock_gettime(Process::CLOCK_MONOTONIC, :microsecond).class`, "Integer\n"},
		{"clock_nanosecond", `p Process.clock_gettime(Process::CLOCK_MONOTONIC, :nanosecond).class`, "Integer\n"},
		{"clock_float_millisecond", `p Process.clock_gettime(Process::CLOCK_MONOTONIC, :float_millisecond).class`, "Float\n"},
		{"clock_float_microsecond", `p Process.clock_gettime(Process::CLOCK_MONOTONIC, :float_microsecond).class`, "Float\n"},
		// Monotonic time advances non-negatively (sanity, not exactness).
		{"clock_nonneg", `p(Process.clock_gettime(Process::CLOCK_MONOTONIC) >= 0)`, "true\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runProg(t, tt.src, nil)
			if err != nil {
				t.Fatalf("src=%q: %v", tt.src, err)
			}
			if out != tt.want {
				t.Fatalf("src=%q: got %q, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestSingletonMethods covers Object#singleton_methods for class receivers (class
// methods, inherited by default, own-only with a false argument), plain-object
// receivers (per-object singleton methods) and receivers with none. Verified
// against MRI.
func TestSingletonMethods(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"class_inherited", `class A; def self.am; end; end
class B < A; def self.bm; end; end
p B.singleton_methods.sort`, "[:am, :bm]\n"},
		{"class_own", `class A; def self.am; end; end
class B < A; def self.bm; end; end
p B.singleton_methods(false).sort`, "[:bm]\n"},
		{"class_own_false_arg_nil", `class C; def self.cm; end; end
p C.singleton_methods(nil).sort`, "[:cm]\n"},
		{"object_singleton", `o = Object.new
def o.foo; end
p o.singleton_methods`, "[:foo]\n"},
		{"object_none", `p Object.new.singleton_methods`, "[]\n"},
		{"immediate_none", `p 5.singleton_methods`, "[]\n"},
		{"builtin_none", `p [].singleton_methods`, "[]\n"},
		// The Puppet idiom that drove this: Dir.singleton_methods.include?(:exists?).
		{"dir_include", `p Dir.singleton_methods.include?(:exist?)`, "true\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runProg(t, tt.src, nil)
			if err != nil {
				t.Fatalf("src=%q: %v", tt.src, err)
			}
			if !strings.HasPrefix(out, "") || out != tt.want {
				t.Fatalf("src=%q: got %q, want %q", tt.src, out, tt.want)
			}
		})
	}
}
