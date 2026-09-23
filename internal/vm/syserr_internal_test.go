// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strconv"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// errnoLit renders an errno number as the Ruby integer literal a test embeds in
// its source, so an expectation reads the host's number rather than hard-coding
// a platform-specific one.
func errnoLit(n int64) string { return strconv.FormatInt(n, 10) }

// TestErrnoStrerror pins the message text MRI's syserr_initialize builds from
// strerror(). The expectations are the host MRI 4.0.5's own output on darwin
// (SystemCallError.new(n).message), which is where the conformance gate runs.
func TestErrnoStrerror(t *testing.T) {
	for _, c := range []struct {
		n    int64
		want string
	}{
		{1, "Operation not permitted"},
		{2, "No such file or directory"},
		{22, "Invalid argument"},
		{92, "Illegal byte sequence"},
		{0, "Undefined error: 0"},
		{1 << 28, "Unknown error: 268435456"},
		{-1, "Unknown error: -1"},
	} {
		if got := errnoStrerror(c.n); got != c.want {
			t.Errorf("errnoStrerror(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// TestErrnoUnknownText drives errnoUnknownText's two decisions directly: whether
// the text is Go's "errno N" placeholder at all, and the separate wording errno 0
// takes. A named errno must be left for the capitalising path.
func TestErrnoUnknownText(t *testing.T) {
	if _, ok := errnoUnknownText("invalid argument", 22); ok {
		t.Errorf("a named errno must not be treated as unknown")
	}
	if s, ok := errnoUnknownText("errno 0", 0); !ok || s != "Undefined error: 0" {
		t.Errorf("errno 0: got %q %v", s, ok)
	}
	if s, ok := errnoUnknownText("errno 5000", 5000); !ok || s != "Unknown error: 5000" {
		t.Errorf("errno 5000: got %q %v", s, ok)
	}
}

// TestCapitalizeASCII covers the three shapes the helper distinguishes: a
// lower-case ASCII lead (the only one it changes), an already-capital lead, and
// the empty string.
func TestCapitalizeASCII(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"invalid argument", "Invalid argument"},
		{"Already", "Already"},
		{"", ""},
		{"1 thing", "1 thing"},
	} {
		if got := capitalizeASCII(c.in); got != c.want {
			t.Errorf("capitalizeASCII(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestErrnoClassRegistration pins the three registration rules of MRI's
// Init_syserr: one class per number (so two names for one number are one class),
// a name with no number on this platform naming Errno::NOERROR, and every class
// carrying its number as its own Errno constant.
func TestErrnoClassRegistration(t *testing.T) {
	vm := New(nil)
	mod := vm.consts["Errno"].(*RClass)
	if got, want := len(mod.consts), len(errnoNumbers)+len(errnoUndefinedNames)+len(errnoAliases); got != want {
		t.Errorf("Errno has %d constants, want %d", got, want)
	}
	if mod.consts["EWOULDBLOCK"] != mod.consts["EAGAIN"] {
		t.Errorf("EWOULDBLOCK must name EAGAIN's class")
	}
	noerror := mod.consts["NOERROR"].(*RClass)
	for _, n := range errnoUndefinedNames {
		if mod.consts[n] != noerror {
			t.Fatalf("%s must name Errno::NOERROR", n)
		}
	}
	for name, n := range errnoNumbers {
		c := mod.consts[name].(*RClass)
		if e, ok := c.consts["Errno"].(object.Integer); !ok || int64(e) != n {
			t.Errorf("Errno::%s::Errno = %v, want %d", name, c.consts["Errno"], n)
		}
		if vm.consts["Errno::"+name] == nil {
			t.Errorf("Errno::%s is not registered under its flat name", name)
		}
	}
}

// TestErrnoClassLookup covers vm.errnoClass: a number a class claims, one no
// class claims, and a VM whose Errno constant is not a class at all (the guard
// that keeps the lookup from panicking if the module were ever shadowed).
func TestErrnoClassLookup(t *testing.T) {
	vm := New(nil)
	c := vm.errnoClass(errnoNumbers["EINVAL"])
	if c == nil || c != vm.consts["Errno::EINVAL"] {
		t.Errorf("errnoClass(EINVAL) = %v, want Errno::EINVAL", c)
	}
	if got := vm.errnoClass(1 << 40); got != nil {
		t.Errorf("errnoClass of an unclaimed number = %v, want nil", got)
	}
	vm.consts["Errno"] = object.NewString("not a class")
	if got := vm.errnoClass(2); got != nil {
		t.Errorf("errnoClass with no Errno module = %v, want nil", got)
	}
}

// TestClassErrno covers the ancestry walk behind #errno: a class carrying its own
// Errno constant, a user subclass inheriting it, and a class outside the
// SystemCallError tree, which must NOT pick up the top-level Errno module that
// lives in Object's constant table.
func TestClassErrno(t *testing.T) {
	vm := New(nil)
	if e := vm.classErrno(vm.consts["Errno::EINVAL"].(*RClass)); e != object.IntValue(errnoNumbers["EINVAL"]) {
		t.Errorf("classErrno(EINVAL) = %v", e)
	}
	sub := newClass("Sub", vm.consts["Errno::ENOENT"].(*RClass))
	if e := vm.classErrno(sub); e != object.IntValue(errnoNumbers["ENOENT"]) {
		t.Errorf("a subclass must inherit its parent's Errno: got %v", e)
	}
	if e := vm.classErrno(vm.consts["SystemCallError"].(*RClass)); e != object.NilV {
		t.Errorf("generic SystemCallError must have no Errno: got %v", e)
	}
	if e := vm.classErrno(vm.cObject); e != object.NilV {
		t.Errorf("the walk must stop before Object's constant table: got %v", e)
	}
}

// TestBecomeErrnoClassGuards covers becomeErrnoClass's two non-rewriting paths:
// an errno no class claims leaves the receiver alone, and a receiver that is not
// a plain object is MRI's "invalid instance type" TypeError rather than a panic.
func TestBecomeErrnoClassGuards(t *testing.T) {
	vm := New(nil)
	o := &RObject{class: vm.consts["SystemCallError"].(*RClass), ivars: map[string]object.Value{}}
	vm.becomeErrnoClass(o, 1<<40)
	if o.class != vm.consts["SystemCallError"] {
		t.Errorf("an unclaimed errno must leave the class alone, got %v", o.class)
	}
	defer func() {
		r := recover()
		e, ok := r.(RubyError)
		if !ok || e.Class != "TypeError" || e.Message != "invalid instance type" {
			t.Errorf("non-object receiver: got %v", r)
		}
	}()
	vm.becomeErrnoClass(object.IntValue(3), errnoNumbers["EINVAL"])
}

// TestSyserrEqqNonException covers the two syserr_eqq branches that a Ruby-level
// spec cannot reach through the shim: an argument that is not a SystemCallError
// but does answer #errno (which MRI still compares), and one whose #errno is not
// an Integer, which falls through to a dispatched ==.
func TestSyserrEqqNonException(t *testing.T) {
	vm := New(nil)
	einval := vm.consts["Errno::EINVAL"].(*RClass)
	out := runFS(t, `
class Quacks; def errno; `+errnoLit(errnoNumbers["EINVAL"])+`; end; end
class QuacksFloat; def errno; `+errnoLit(errnoNumbers["EINVAL"])+`.0; end; end
class Mute; end
p Errno::EINVAL === Quacks.new
p Errno::EINVAL === QuacksFloat.new
p Errno::EINVAL === Mute.new
`)
	if want := "true\ntrue\nfalse\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
	if !vm.syserrEqq(vm.consts["SystemCallError"].(*RClass), vm.send(einval, "new", nil, nil)) {
		t.Errorf("a generic SystemCallError must match any SystemCallError")
	}
}

// TestSyserrInitializeShapes drives SystemCallError#initialize through every
// branch of MRI's syserr_initialize from Ruby. Each expectation is the host MRI
// 4.0.5's own output, captured with the same expression.
func TestSyserrInitializeShapes(t *testing.T) {
	einval := errnoLit(errnoNumbers["EINVAL"])
	for _, c := range []struct{ src, want string }{
		// Generic receiver: a lone Integer is the errno and rewrites the class.
		{`e = SystemCallError.new(` + einval + `); p [e.class, e.message, e.errno]`,
			`[Errno::EINVAL, "Invalid argument", ` + einval + "]\n"},
		// A lone non-Integer stays the message, and the object stays generic.
		{`e = SystemCallError.new("m"); p [e.class, e.message, e.errno]`,
			"[SystemCallError, \"unknown error - m\", nil]\n"},
		// nil message with an errno behaves as if the message were not passed.
		{`p SystemCallError.new(nil, ` + einval + `).message`, "\"Invalid argument\"\n"},
		// The location argument is inserted before the custom message and is #to_s'd.
		{`p SystemCallError.new("foo", 1, :not_a_string).message`,
			"\"Operation not permitted @ not_a_string - foo\"\n"},
		// An errno no class claims leaves a generic SystemCallError.
		{`e = SystemCallError.new(-1); p [e.class, e.message]`,
			"[SystemCallError, \"Unknown error: -1\"]\n"},
		// Subclass receiver: (msg, func), errno from the class constant.
		{`e = Errno::EINVAL.new; p [e.class, e.message, e.errno]`,
			`[Errno::EINVAL, "Invalid argument", ` + einval + "]\n"},
		{`p Errno::EINVAL.new("custom message", "location").message`,
			"\"Invalid argument @ location - custom message\"\n"},
		// A user subclass inherits the default message through its parent's Errno.
		{`c = Class.new(Errno::ENOENT); p c.new("custom message").message`,
			"\"No such file or directory - custom message\"\n"},
		// Arity, per receiver shape.
		{`begin; SystemCallError.new; rescue ArgumentError => e; puts e.message; end`,
			"wrong number of arguments (given 0, expected 1..3)\n"},
		{`begin; SystemCallError.new(1,2,3,4); rescue ArgumentError => e; puts e.message; end`,
			"wrong number of arguments (given 4, expected 1..3)\n"},
		{`begin; Errno::EINVAL.new(1,2,3); rescue ArgumentError => e; puts e.message; end`,
			"wrong number of arguments (given 3, expected 0..2)\n"},
		// Coercion failures, in MRI's order: the errno is converted first.
		{`begin; SystemCallError.new(:foo, 1); rescue TypeError => e; puts e.message; end`,
			"no implicit conversion of Symbol into String\n"},
		{`begin; SystemCallError.new("foo", "bar"); rescue TypeError => e; puts e.message; end`,
			"no implicit conversion of String into Integer\n"},
		// A Float errno is truncated for the class lookup but stored as given.
		{`e = SystemCallError.new("foo", 2.9); p [e.class, e.errno]`, "[Errno::ENOENT, 2.9]\n"},
	} {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSystemCallErrorEqqArity pins the single-argument arity of the .=== MRI
// defines with argc 1.
func TestSystemCallErrorEqqArity(t *testing.T) {
	out := runFS(t, `begin; SystemCallError.===(1, 2); rescue ArgumentError => e; puts e.message; end`)
	if want := "wrong number of arguments (given 2, expected 1)\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}
