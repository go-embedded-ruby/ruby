// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestArrayJoinToSFallback covers Array#join's last-resort rendering: when an
// element's #to_s returns a non-String, MRI falls back to the default object
// representation. The address is non-deterministic, so assert its structure.
func TestArrayJoinToSFallback(t *testing.T) {
	got := eval(t, `class BadToS; def to_s; 42; end; end
print([1, BadToS.new].join(":"))`)
	if !strings.HasPrefix(got, "1:#<BadToS") {
		t.Errorf("join to_s fallback: got %q, want prefix %q", got, "1:#<BadToS")
	}
}

// TestArrayJoinCoercion covers Array#join's MRI-faithful behavior: nested/recursive
// joining, #to_str/#to_ary/#to_s element coercion, separator #to_str coercion, the
// $, field separator, recursion detection, and encoding negotiation.
func TestArrayJoinCoercion(t *testing.T) {
	cases := []struct{ src, want string }{
		// Basic and nested joins (the same separator threads through nesting).
		{`p([1, 2, 3].join(":"))`, "\"1:2:3\"\n"},
		{`p([1, [2, [3, 4], 5], 6].join(":"))`, "\"1:2:3:4:5:6\"\n"},
		{`p([1, 2, 3].join(nil))`, "\"123\"\n"},
		{`p([].join("x"))`, "\"\"\n"},
		// Array subclass elements join recursively via #to_ary/kind_of Array.
		{`class MyArr < Array; end
p([1, MyArr[2, 3], 4].join(":"))`, "\"1:2:3:4\"\n"},
		// Separator coercion via #to_str.
		{`class Sep; def to_str; ", "; end; end
p([1, 2].join(Sep.new))`, "\"1, 2\"\n"},
		// Element coercion order: #to_str first.
		{`class E1; def to_str; "foo"; end; end
p([E1.new].join)`, "\"foo\"\n"},
		// #to_ary second (when #to_str returns non-String).
		{`class E2; def to_str; nil; end; def to_ary; ["a", "b"]; end; end
p([E2.new].join(":"))`, "\"a:b\"\n"},
		// #to_s third.
		{`class E3; def to_str; nil; end; def to_ary; nil; end; def to_s; "z"; end; end
p([E3.new].join)`, "\"z\"\n"},
		// $, field separator is used when no explicit separator is given.
		{`$, = "-"; r = [1, 2, 3].join; $, = nil; p(r)`, "\"1-2-3\"\n"},
		// An empty array ignores $, and never touches the separator.
		{`$, = "-"; r = [].join; $, = nil; p(r)`, "\"\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestArrayJoinErrors covers Array#join's error paths.
func TestArrayJoinErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		// A separator that cannot be coerced to a String raises TypeError.
		{`class Bad; end
begin; [1, 2].join(Bad.new); rescue TypeError; puts "TE"; end`, "TE\n"},
		{`begin; [1, 2].join(false); rescue TypeError; puts "TE"; end`, "TE\n"},
		// A self-referential array raises ArgumentError.
		{`a = [1, 2]; a << a
begin; a.join(":"); rescue ArgumentError; puts "AE"; end`, "AE\n"},
		{`a = []; a << a
begin; a.join; rescue ArgumentError; puts "AE"; end`, "AE\n"},
		// An element responding to none of #to_str/#to_ary/#to_s raises NoMethodError.
		{`o = Object.new
class << o; undef :to_s; end
begin; [1, o].join; rescue NoMethodError; puts "NME"; end`, "NME\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestArrayJoinEncoding covers the running-encoding negotiation of Array#join.
func TestArrayJoinEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		// Empty array -> US-ASCII.
		{`p [].join.encoding.name`, "\"US-ASCII\"\n"},
		// A UTF-8 (non-7bit) element with an ASCII element -> UTF-8.
		{`p ["é", "a"].join.encoding.name`, "\"UTF-8\"\n"},
		// Two ASCII US-ASCII strings stay US-ASCII.
		{`a = "x".force_encoding("US-ASCII"); b = "y".force_encoding("US-ASCII")
p [a, b].join.encoding.name`, "\"US-ASCII\"\n"},
		// Incompatible encodings raise EncodingError.
		{`u = "é"; b = "\xff".force_encoding("BINARY")
begin; [u, b].join; rescue EncodingError; puts "EE"; end`, "EE\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestArrayMultiplyString covers Array#* with a String/#to_str separator (join)
// and #to_int repeat coercion.
func TestArrayMultiplyString(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p([1, 2, 3, 4] * "::")`, "\"1::2::3::4\"\n"},
		{`class Sep; def to_str; "::"; end; end
p([1, 2, 3, 4] * Sep.new)`, "\"1::2::3::4\"\n"},
		// #to_int drives the repeat case.
		{`class Cnt; def to_int; 2; end; end
p([1, 2] * Cnt.new)`, "[1, 2, 1, 2]\n"},
		// #to_str wins over #to_int.
		{`class Both; def to_int; 2; end; def to_str; "-"; end; end
p([1, 2, 3] * Both.new)`, "\"1-2-3\"\n"},
		// Array subclass constructor Array.[] and its inheritance.
		{`p Array[1, 2, 3]`, "[1, 2, 3]\n"},
		{`class Sub < Array; end; p Sub[7, 8].class`, "Sub\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
