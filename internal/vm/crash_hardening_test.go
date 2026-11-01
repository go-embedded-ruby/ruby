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

// ⛔ `(1..).to_set` DID NOT CRASH — IT NEVER CAME BACK, which is worse.
//
// Measured on main: `rbgo -e '(1..).to_set'` had to be killed (`timeout 6` →
// exit 124). Enumerable#to_set enumerated the endless range forever, allocating
// as it went. One expression from any untrusted Ruby an embedding host
// evaluates, and the host is gone — and unlike a stack overflow there is
// nothing to recover from, because nothing faults.
//
// Every other member of the family was already guarded (`to_a`, `last`, `max`,
// `size`), which is how this one hid: `Range#to_set` is new in Ruby 4.0, so
// there was no 3.4-era guard to copy, and its spec sits behind a
// `ruby_version_is "4.0"` block the gate cannot reach at 3.4.1. The sweep
// recorded `core/range/to_set_spec.rb` as "failed to load" — it had not failed,
// the per-file TIMEOUT had killed it.
//
// The message is MRI's, character for character (range.c at v4.0.9,
// `range_to_set`), and core/range/to_set_spec.rb:22 asserts it.
//
// There is no timeout in this test and there does not need to be one: if the
// guard goes away, `go test` hangs here instead of failing, which is itself the
// signal — and the four rows below it are what keep the guard from being
// "fixed" into refusing a bounded range.
func TestEndlessRangeToSetRaisesInsteadOfHanging(t *testing.T) {
	cases := []struct{ src, want string }{
		{`begin; (1..).to_set; rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"RangeError: cannot convert endless range to a set\n"},
		{`begin; (1...).to_set; rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"RangeError: cannot convert endless range to a set\n"},

		// Bounded ranges are untouched, with and without a block.
		{`p (1..4).to_set`, "Set[1, 2, 3, 4]\n"},
		{`p (1...4).to_set`, "Set[1, 2, 3]\n"},
		{`p (1..3).to_set { |x| x * x }`, "Set[1, 4, 9]\n"},

		// A beginless range still fails the way it did, from the iteration
		// rather than from this guard: the check is on `end`, not on either end.
		{`begin; (..0).to_set; rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: can't iterate from NilClass\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// ⛔ THE FIVE TYPES THE TEST ABOVE DID NOT COVER, each of which killed the
// process.
//
// The guard above is real and was tested — for Array, Hash and Set. Struct and
// a plain object were outside that population, and `defaultObjectInspect` and
// Struct#to_s both recursed until the Go runtime aborted with
//
//	fatal error: stack overflow
//	runtime: goroutine stack exceeds 1000000000-byte limit
//
// which is NOT a panic: callNative's recover cannot convert it, so the embedding
// host dies with the interpreter. Measured on 3a3fb818, every case below exited
// 2 with that abort; all of them now exit 0.
//
// The reachability is what makes it serious. `a.peer = b; b.peer = a; p a` is
// two ordinary objects pointing at each other — a doubly-linked list, a parent
// with a back-pointer, an ORM association — and it needs no adversary. Wrapping
// the object in an Array or a Hash did not save it, because those guard THEIR
// OWN identity and the recursion came back through the object's inspect.
//
// The renderings are MRI's, from the two functions that do this upstream with
// rb_exec_recursive: object.c's inspect_obj appends " ..." before the closing
// ">", and struct.c's inspect_struct returns ":...>" (both at v4.0.9).
func TestRecursiveObjectAndStructInspectDoNotKillTheProcess(t *testing.T) {
	cases := []struct{ src, want string }{
		// A plain object holding itself.
		{`class K1; def initialize; @me = self; end; end; puts K1.new.inspect`,
			"#<K1:0x0000000000000008 @me=#<K1:0x0000000000000008 ...>>\n"},

		// ⛔ Two ordinary objects pointing at each other — the realistic shape.
		// Note the two distinct addresses and that the re-entry lands on the
		// first: the guard tracks identity, not depth.
		{`class N1; attr_accessor :peer; end
a = N1.new; b = N1.new; a.peer = b; b.peer = a
p a`,
			"#<N1:0x0000000000000008 @peer=#<N1:0x0000000000000010 @peer=#<N1:0x0000000000000008 ...>>>\n"},

		// Inside a container: the container's own guard does not cover this.
		{`class N2; attr_accessor :peer; end; a = N2.new; a.peer = a; p [a]`,
			"[#<N2:0x0000000000000008 @peer=#<N2:0x0000000000000008 ...>>]\n"},
		{`class N3; attr_accessor :peer; end; a = N3.new; a.peer = a; p({k: a})`,
			"{k: #<N3:0x0000000000000008 @peer=#<N3:0x0000000000000008 ...>>}\n"},

		// A Struct holding itself, and the same through #to_a — which died
		// because the Array guarded itself and then re-entered the Struct.
		{`S1 = Struct.new(:x); s = S1.new(nil); s.x = s; puts s.inspect`,
			"#<struct S1 x=#<struct S1:...>>\n"},
		{`S2 = Struct.new(:x); s = S2.new(nil); s.x = s; puts s.to_s`,
			"#<struct S2 x=#<struct S2:...>>\n"},
		{`S3 = Struct.new(:x); s = S3.new(nil); s.x = s; puts s.to_a.inspect`,
			"[#<struct S3 x=#<struct S3:...>>]\n"},

		// And the guard must CLEAR between siblings, or it would turn an ordinary
		// repeated reference into "..." and silently corrupt normal output. This
		// is the negative control for both new paths.
		{`class K2; attr_accessor :v; end
x = K2.new; x.v = 1
p [x, x]`,
			"[#<K2:0x0000000000000008 @v=1>, #<K2:0x0000000000000008 @v=1>]\n"},
		{`S4 = Struct.new(:x); s = S4.new(1); p [s, s]`,
			"[#<struct S4 x=1>, #<struct S4 x=1>]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
