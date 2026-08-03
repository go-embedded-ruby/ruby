// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestNonLocalReturnAndBreak covers control-flow signals that previously escaped
// the VM as an uncaught Go panic. A non-local `return`/`break` whose home
// activation is gone must raise a rescue-able LocalJumpError; the well-formed
// cases (non-local return to a live method, lambda return/break, iteration
// break, loop break, next) must keep working. Asserted against MRI 4.0.5.
func TestNonLocalReturnAndBreak(t *testing.T) {
	cases := []struct{ src, want string }{
		// Non-local return unwinds to the enclosing (live) method.
		{`def sm; Proc.new { return :pv }.call; :mv; end; p sm`, ":pv\n"},
		// A lambda's return is local.
		{`l = lambda { return 5 }; p l.call`, "5\n"},
		// A lambda's break is local too (returns the value).
		{`p(-> { break 5 }.call)`, "5\n"},
		{`p(-> { break false || true }.call)`, "true\n"},
		// break out of an iterator returns from the iterating call.
		{`p([1, 2].each { break 7 })`, "7\n"},
		// break/next inside while and loop are unaffected (they compile to jumps).
		{`i = 0; while true; break if i > 2; i += 1; end; p i`, "3\n"},
		{`n = 0; loop { n += 1; break if n == 3 }; p n`, "3\n"},
		{`p [1, 2, 3].map { |x| next x * 2 }`, "[2, 4, 6]\n"},
		// The escaped signals are catchable Ruby exceptions.
		{`def m; Proc.new { return 10 }; end
r = begin; m.call; rescue LocalJumpError => e; e.message; end
p r`, "\"unexpected return\"\n"},
		{`r = begin; proc { break 1 }.call; rescue LocalJumpError => e; e.message; end
p r`, "\"break from proc-closure\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// The same signals surface as errors at the top level (no rescue).
	for _, c := range []struct{ src, want string }{
		{`def m; Proc.new { return 10 }; end; m.call`, "unexpected return"},
		{`proc { break 1 }.call`, "break from proc-closure"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

// TestObjectSpaceshipIdentity covers Object#<=> comparing by identity (0 for the
// same object, nil otherwise) rather than sending #==, which used to recurse
// forever for a class that includes Comparable (its #== is defined via <=>).
func TestObjectSpaceshipIdentity(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class C; end; o = C.new; p [o <=> o, (o <=> C.new).nil?]`, "[0, true]\n"},
		// A Numeric subclass includes Comparable; comparing two instances no longer
		// loops (== is false for distinct, true for identical; <=> nil / 0).
		{`class S < Numeric; end; a = S.new; b = S.new; p [a == b, a == a, a <=> b, a <=> a]`,
			"[false, true, nil, 0]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRecursiveStructureEquality covers the recursion guard in ==/eql? for
// self-referential arrays and hashes: a comparison that re-enters the same pair
// is treated as equal (MRI), instead of overflowing the stack.
func TestRecursiveStructureEquality(t *testing.T) {
	cases := []struct{ src, want string }{
		{`a = [1]; a << a; b = [1]; b << b; p a == b`, "true\n"},
		{`a = [1]; a << a; b = [2]; b << b; p a == b`, "false\n"},
		{`a = [1]; a << a; p a.eql?(a)`, "true\n"},
		{`a = [1]; a << a; b = [1]; b << b; p a.eql?(b)`, "true\n"},
		{`h = {}; h[:x] = h; g = {}; g[:x] = g; p h == g`, "true\n"},
		{`h = {}; h[:x] = h; p h.eql?(h)`, "true\n"},
		// Range == compares its ends through the same guard.
		{`p((1..3) == (1..3))`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRecursiveStructureInspect covers the repr recursion guard: inspecting or
// printing a self-referential Array/Hash/Set renders "[...]"/"{...}"/"Set[...]"
// (as MRI does) instead of recursing until the Go stack overflows.
func TestRecursiveStructureInspect(t *testing.T) {
	cases := []struct{ src, want string }{
		{`a = [1]; a << a; p a`, "[1, [...]]\n"},
		{`a = [1, 2]; a << a; puts a.to_s`, "[1, 2, [...]]\n"},
		{`h = {}; h[:x] = h; p h`, "{x: {...}}\n"},
		// puts flattens nested arrays; a recursive member prints as "[...]".
		{`a = [1]; a << a; puts a`, "1\n[...]\n"},
		{`require "set"; s = Set.new; s << 1; s << s; p s`, "Set[1, Set[...]]\n"},
		// A non-recursive structure is unaffected (guard cleared between siblings).
		{`x = [1]; p [x, x]`, "[[1], [1]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
