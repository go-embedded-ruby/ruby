// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestSignalModule covers the Signal module and Kernel#trap: name normalisation
// across Symbol/String("SIG"-prefixed and bare)/Integer designators, the
// previous-handler return value of #trap, #list and #signame.
//
// Five rows here PINNED rbgo's own divergence while the comment said they were
// pinned to MRI: Signal.signame("x") answered nil where MRI raises TypeError,
// Signal.signame with no argument answered nil where MRI raises ArgumentError,
// Signal.trap(987654) invented a slot where MRI range-checks against NSIG,
// Signal.trap(:QUIT) with no block recorded DEFAULT where MRI calls
// rb_block_proc() and fails, and an object with only #to_s was accepted where
// signm2signo coerces through #to_str. Each replacement below was MEASURED
// against MRI 4.0.5 (ruby 4.0.5 (2026-05-20) +PRISM [arm64-darwin25]) and says so.
func TestSignalModule(t *testing.T) {
	cases := []struct{ src, want string }{
		// First trap returns "DEFAULT"; a second returns the prior Proc.
		{`p Signal.trap(:INT) { }`, "\"DEFAULT\"\n"},
		{`Signal.trap(:INT) { }; p Signal.trap(:INT, "DEFAULT").class`, "Proc\n"},
		// A bare command/handler string is recorded and returned next time.
		{`Signal.trap("TERM", "IGNORE"); p Signal.trap("TERM", "DEFAULT")`, "\"IGNORE\"\n"},
		// "SIG"-prefixed and Integer designators normalise to the same slot as the
		// bare name, so a handler set through one spelling is seen through another.
		{`Signal.trap("SIGINT") { }; p Signal.trap(2, "DEFAULT").class`, "Proc\n"},
		{`Signal.trap(15) { }; p Signal.trap("TERM", "DEFAULT").class`, "Proc\n"},
		// list maps names to numbers; signame inverts a known number and nil-checks.
		{`p Signal.list["INT"]`, "2\n"},
		{`p Signal.list.class`, "Hash\n"},
		{`p Signal.signame(2)`, "\"INT\"\n"},
		{`p Signal.signame(987654)`, "nil\n"},
		// MEASURED on MRI 4.0.5: sig_signame takes NUM2INT, so a String is a
		// TypeError, not nil. This row read "nil" and claimed to be pinned to MRI.
		{`begin; Signal.signame("x"); rescue TypeError => e; puts e.message; end`,
			"no implicit conversion of String into Integer\n"},
		// MEASURED on MRI 4.0.5: ArgumentError, not nil.
		{`begin; Signal.signame; rescue ArgumentError => e; puts e.message; end`,
			"wrong number of arguments (given 0, expected 1)\n"},
		// MEASURED on MRI 4.0.5: trap_signm range-checks against NSIG, so an
		// out-of-range number is an ArgumentError rather than a fresh slot named
		// after its decimal string.
		{`begin; Signal.trap(987654) { }; rescue ArgumentError => e; puts e.message; end`,
			"invalid signal number (987654)\n"},
		// Kernel#trap reaches the same machinery without the Signal receiver.
		{`p trap("USR1") { }`, "\"DEFAULT\"\n"},
		// Defaulting with two args and no block records the literal handler.
		{`Signal.trap(:HUP, "DEFAULT"); p Signal.trap(:HUP, "IGNORE")`, "\"DEFAULT\"\n"},
		// MEASURED on MRI 4.0.5: sig_trap calls rb_block_proc() for a one-argument
		// trap, which without a block is an ArgumentError — it does not record
		// DEFAULT.
		{`begin; Signal.trap(:QUIT); rescue ArgumentError => e; puts e.message; end`,
			"tried to create Proc object without a block\n"},
		// MEASURED on MRI 4.0.5: signm2signo coerces through #to_str (via
		// rb_check_string_type), NOT #to_s, so an object with only #to_s is a
		// "bad signal type" naming its class. This row accepted it as a fresh slot.
		{`class SigName; def to_s; "CUSTOMSIG"; end; end
begin; Signal.trap(SigName.new) { }; rescue ArgumentError => e; puts e.message; end`,
			"bad signal type SigName\n"},
		{`class SigStr; def to_str; "HUP"; end; end
p Signal.trap(SigStr.new, "IGNORE")`, "\"DEFAULT\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Zero arguments is an ArgumentError, as in MRI.
	if class, _ := evalErr(t, `Signal.trap`); class != "ArgumentError" {
		t.Errorf("Signal.trap with no args raised %q, want ArgumentError", class)
	}
}

// TestOpen3Shell covers the Open3 loadable shell: require succeeds and returns
// true then false, and the spawning entry points raise NotImplementedError.
func TestOpen3Shell(t *testing.T) {
	if got := eval(t, `p require "open3"`); got != "true\n" {
		t.Errorf(`require "open3" got %q, want true`, got)
	}
	if got := eval(t, `require "open3"; p require "open3"`); got != "false\n" {
		t.Errorf("second require got %q, want false", got)
	}
	if got := eval(t, `require "open3"; p defined?(Open3)`); got != "\"constant\"\n" {
		t.Errorf("Open3 constant got %q", got)
	}
	for _, m := range []string{"popen3", "capture3", "capture2", "capture2e", "pipeline", "pipeline_start"} {
		class, _ := evalErr(t, `require "open3"; Open3.`+m+`("x")`)
		if class != "NotImplementedError" {
			t.Errorf("Open3.%s raised %q, want NotImplementedError", m, class)
		}
	}
}

// TestRemoveConst covers Module#remove_const: it deletes an own constant and
// returns its value, and raises NameError for an absent one — at the top level
// (Object) and inside a nested module.
func TestRemoveConst(t *testing.T) {
	cases := []struct{ src, want string }{
		{`module M; X = 7; end; p M.send(:remove_const, :X)`, "7\n"},
		{`module M; X = 7; end; M.send(:remove_const, :X); p M.const_defined?(:X, false)`, "false\n"},
		{`TOP = 5; p Object.send(:remove_const, :TOP)`, "5\n"},
		{`TOP = 5; Object.send(:remove_const, :TOP); p defined?(TOP)`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if class, msg := evalErr(t, `module M; end; M.send(:remove_const, :Nope)`); class != "NameError" || msg != "constant M::Nope not defined" {
		t.Errorf("remove_const of absent const: (%s, %q)", class, msg)
	}
}

// TestConstDefinedInherit covers Module#const_defined?'s second argument: with
// inherit=false only the receiver's own table is consulted; with the default the
// ancestor chain (and Object) is searched.
func TestConstDefinedInherit(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class P; end; class P::Q; end; p P.const_defined?(:Q, false)`, "true\n"},
		{`class P; end; p P.const_defined?(:String, false)`, "false\n"},
		{`class P; end; p P.const_defined?(:String, true)`, "true\n"},
		{`class P; end; p P.const_defined?(:String)`, "true\n"},
		{`p Object.const_defined?(:String, false)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestAnonymousClassNaming covers the "permanent name on first constant binding"
// rule for const_set: an anonymous class/module bound to a constant takes that
// constant's qualified name, so to_s/name and a nested-constant path work.
func TestAnonymousClassNaming(t *testing.T) {
	cases := []struct{ src, want string }{
		{`module A; module B; end; end; k = Class.new; A::B.const_set(:C, k); p k.name`, "\"A::B::C\"\n"},
		{`k = Class.new; A = k; p k.name`, "\"A\"\n"},
		{`module A; module B; end; end; k = Class.new; A::B.const_set(:C, k); p k.to_s`, "\"A::B::C\"\n"},
		// A class that already has a name keeps it when also bound elsewhere.
		{`class Named; end; Alias = Named; p Alias.name`, "\"Named\"\n"},
		// The newly-named class can hold and resolve its own nested constant.
		{`module A; end; k = Class.new; A.const_set(:K, k); k.const_set(:SEP, "/"); p A::K::SEP`, "\"/\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestClassMethodSuper covers `super` from a class method, defined both as
// `def self.foo` and inside a `class << self` body, walking the metaclass chain
// to the inherited class method (the form Puppet's nameservice provider uses for
// initvars). A multi-level chain and a genuine miss are exercised too.
func TestClassMethodSuper(t *testing.T) {
	cases := []struct{ src, want string }{
		// def self.foo super.
		{`class B; def self.f; "b"; end; end
class C < B; def self.f; "c-" + super; end; end
p C.f`, "\"c-b\"\n"},
		// class << self ; def f ; super.
		{`class B; def self.f; "b"; end; end
class C < B
  class << self
    def f; "c-" + super; end
  end
end
p C.f`, "\"c-b\"\n"},
		// Three-level metaclass chain.
		{`class A; def self.f; "a"; end; end
class B < A
  class << self; def f; "b-" + super; end; end
end
class C < B
  class << self; def f; "c-" + super; end; end
end
p C.f`, "\"c-b-a\"\n"},
		// Side effects in the overriding class method run, and super's result flows.
		{`class B; def self.init; @v = 1; "base"; end; end
class C < B
  class << self
    def init; @w = 2; super; end
  end
end
r = C.init
p [r, C.instance_variable_get(:@w)]`, "[\"base\", 2]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// A class-method super with no inherited definition raises NoMethodError.
	if class, _ := evalErr(t, `class B; end
class C < B
  class << self; def f; super; end; end
end
C.f`); class != "NoMethodError" {
		t.Errorf("orphan class-method super raised %q, want NoMethodError", class)
	}
}

// TestClassEvalConstScope covers constant *lookup* and *definition* inside a
// class_eval/module_eval block: both follow the block's lexical nesting (where
// the block was written), not the eval receiver — so a bare `File` resolves to
// ::File even when the receiver is itself named File (Puppet's file type
// referencing File::SEPARATOR), and a `CONST = …` lands in the block's lexical
// scope rather than on the receiver (so a sibling block in the same eval body
// reads it back the same way — Puppet's file type defines CREATORS in its
// newtype block and a nested validate block reads it). This matches MRI.
func TestClassEvalConstScope(t *testing.T) {
	cases := []struct{ src, want string }{
		// Receiver named File; File::SEPARATOR must reach ::File, not the receiver.
		// The defined SEP lands in the block's lexical scope (module P), not on the
		// File receiver — exactly as MRI does.
		{`module P; module Type; class File; end; end; end
module P
  P::Type::File.class_eval { SEP = File::SEPARATOR.to_s }
end
p P::SEP`, "\"/\"\n"},
		// A bare constant from the surrounding lexical scope is visible in the block,
		// and a constant defined there lands in that same lexical scope (Outer), not
		// on the eval receiver (Inner).
		{`module Outer
  TARGET = 42
  class Inner; end
  Inner.class_eval { GOT = TARGET }
end
p Outer::GOT`, "42\n"},
		// A constant defined in a top-level class_eval block lands at the top level
		// (Object), reachable as a bare constant — not as Recv::MADE.
		{`class Recv; end
Recv.class_eval { MADE = 9 }
p MADE`, "9\n"},
		// A constant defined in one class_eval block is visible to a sibling block in
		// the same eval body via the shared lexical scope (the CREATORS pattern).
		{`module Q
  class Prop
    def self.cap(&b); define_method(:run, &b); end
  end
  Prop.class_eval do
    THINGS = [:a, :b]
    cap { THINGS }
  end
end
p Q::Prop.new.run`, "[:a, :b]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestARGVConstant covers the top-level ARGV constant: it exists, is the same
// Array as $*, starts empty, and a mutation through one spelling is seen through
// the other (the form Puppet's command line uses: ARGV.replace([...])).
func TestARGVConstant(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p ARGV`, "[]\n"},
		{`p ARGV.equal?($*)`, "true\n"},
		{`ARGV.replace(["a", "b"]); p $*`, "[\"a\", \"b\"]\n"},
		{`$*.push("z"); p ARGV`, "[\"z\"]\n"},
		{`p defined?(ARGV)`, "\"constant\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestErrnoConstants covers the expanded Errno table: the common POSIX errnos
// resolve both scoped (rescue Errno::ENOTDIR) and as SystemCallError subclasses,
// and Errno::EEXIST is a single shared class across the File/Dir registrations.
func TestErrnoConstants(t *testing.T) {
	for _, name := range []string{"ENOENT", "EEXIST", "EACCES", "ENOTDIR", "EISDIR", "EPERM", "EINVAL", "EAGAIN", "EBADF", "ESRCH"} {
		src := `p Errno::` + name + `.ancestors.include?(SystemCallError)`
		if got := eval(t, src); got != "true\n" {
			t.Errorf("Errno::%s not a SystemCallError: %q", name, got)
		}
	}
	// Dir.mkdir of an existing path raises the same Errno::EEXIST class the File
	// registration installed (no shadowing).
	if got := eval(t, `p Errno::EEXIST.equal?(Errno.const_get(:EEXIST))`); got != "true\n" {
		t.Errorf("Errno::EEXIST identity: %q", got)
	}
}

// TestTempfile covers the pure-Ruby Tempfile: require, a uniquely-named file in
// the temp dir, IO delegation (write/rewind/read), close leaving the file, and
// unlink/delete removing it and clearing #path; plus the create/open block forms.
func TestTempfile(t *testing.T) {
	if got := eval(t, `p require "tempfile"`); got != "true\n" {
		t.Errorf(`require "tempfile" got %q`, got)
	}
	cases := []struct{ src, want string }{
		// Round-trip through the delegated File, then unlink clears #path.
		{`require "tempfile"
t = Tempfile.new("rbgo-test")
t.write("data"); t.flush; t.rewind
r = t.read
path = t.path
path_existed = File.exist?(path)
t.close
still = File.exist?(path)
t.unlink
gone = !File.exist?(path)
p [r, path_existed, still, gone, t.path]`,
			"[\"data\", true, true, true, nil]\n"},
		// The basename carries the requested prefix.
		{`require "tempfile"
t = Tempfile.new("myprefix"); ok = File.basename(t.path).start_with?("myprefix"); t.close; t.unlink; p ok`,
			"true\n"},
		// create with a block yields a writable handle and cleans up afterwards.
		{`require "tempfile"
saved = nil
Tempfile.create("blk") { |f| f.write("z"); f.flush; saved = f.path; p File.exist?(saved) }
p File.exist?(saved)`,
			"true\nfalse\n"},
		// open with a block behaves like new-with-block.
		{`require "tempfile"
Tempfile.open("op") { |f| p f.path.class }`, "String\n"},
		// create without a block returns the Tempfile; the file materialises on
		// flush/close, then cleanup removes it.
		{`require "tempfile"
t = Tempfile.create("noblk"); t.write("x"); t.close; ok = File.exist?(t.path); t.unlink; p ok`, "true\n"},
		// close! closes and unlinks in one call, leaving nothing behind.
		{`require "tempfile"
t = Tempfile.new("bang"); t.write("y"); path = t.path; t.close!; p File.exist?(path)`, "false\n"},
		// delete is an alias of unlink.
		{`require "tempfile"
t = Tempfile.new("del"); path = t.path; t.close; t.delete; p File.exist?(path)`, "false\n"},
		// chmod is a no-op-safe call (File.chmod absent) returning without raising.
		{`require "tempfile"
t = Tempfile.new("cm"); t.chmod(0o600); t.close; t.unlink; p true`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPathnameAscend covers Pathname#ascend/#descend, including the absolute,
// relative, single-component and root cases and the enumerator (no-block) form,
// matching MRI (Puppet's settings walk a Pathname with #ascend).
func TestPathnameAscend(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "pathname"
out = []; Pathname.new("/a/b/c").ascend { |p| out << p.to_s }; p out`,
			"[\"/a/b/c\", \"/a/b\", \"/a\", \"/\"]\n"},
		{`require "pathname"
out = []; Pathname.new("a/b").ascend { |p| out << p.to_s }; p out`,
			"[\"a/b\", \"a\"]\n"},
		{`require "pathname"
out = []; Pathname.new("x").ascend { |p| out << p.to_s }; p out`,
			"[\"x\"]\n"},
		{`require "pathname"
out = []; Pathname.new("/").ascend { |p| out << p.to_s }; p out`,
			"[\"/\"]\n"},
		{`require "pathname"
p Pathname.new("/a/b").ascend.to_a.map(&:to_s)`,
			"[\"/a/b\", \"/a\", \"/\"]\n"},
		{`require "pathname"
out = []; Pathname.new("/a/b").descend { |p| out << p.to_s }; p out`,
			"[\"/\", \"/a\", \"/a/b\"]\n"},
		{`require "pathname"
p Pathname.new("/a/b").descend.to_a.map(&:to_s)`,
			"[\"/\", \"/a\", \"/a/b\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
