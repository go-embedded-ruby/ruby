// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestArraySliceAndAt covers Array#slice (a true alias of #[]), #at, and the
// shared aref span resolution (single index, start/length, Range), including
// #to_int coercion of index and Range-bound arguments and Float truncation.
// Verified against ruby 4.0.6.
func TestArraySliceAndAt(t *testing.T) {
	cases := []struct{ src, want string }{
		// #slice mirrors #[] in all three arg shapes.
		{`p [1, 2, 3].slice(1)`, "2"},
		{`p [1, 2, 3].slice(-1)`, "3"},
		{`p [1, 2, 3].slice(9)`, "nil"},
		{`p [1, 2, 3].slice(0, 2)`, "[1, 2]"},
		{`p [1, 2, 3].slice(1, 9)`, "[2, 3]"}, // length clamps to the end
		{`p [1, 2, 3].slice(5, 1)`, "nil"},    // start past the end
		{`p [1, 2, 3].slice(0, -1)`, "nil"},   // negative length
		{`p [1, 2, 3].slice(-9, 1)`, "nil"},   // start normalises below 0
		{`p [1, 2, 3].slice(1..2)`, "[2, 3]"},
		{`p [1, 2, 3].slice(4..5)`, "nil"},   // range fully past the end
		{`p [1, 2, 3].slice(1..0)`, "[]"},    // empty but valid range
		{`p [1, 2, 3].slice(1..)`, "[2, 3]"}, // endless range
		// #slice is the *same* method record as #[] (as in MRI).
		{`p Array.instance_method(:slice) == Array.instance_method(:[])`, "true"},
		// #at takes one index and truncates a Float via #to_int.
		{`p [1, 2, 3].at(1)`, "2"},
		{`p [1, 2, 3].at(-1)`, "3"},
		{`p [1, 2, 3].at(9)`, "nil"},
		{`p [1, 2, 3].at(1.9)`, "2"},
		{`p [10, 20, 30][1.9]`, "20"}, // #[] also truncates a Float index
		// A non-Integer index/bound converts through #to_int.
		{`o = Object.new; def o.to_int; 1; end; p [1, 2, 3].at(o)`, "2"},
		{`o = Object.new; def o.to_int; 2; end; p [1, 2, 3].slice(0, o)`, "[1, 2]"},
		{`o = Object.new; def o.to_int; 2; end; p [1, 2, 3].slice(0..o)`, "[1, 2, 3]"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// #at with the wrong argument count raises ArgumentError.
	if got := eval(t, `p (([1, 2].at(1, 2); :no) rescue $!.class)`); got != "ArgumentError\n" {
		t.Errorf("at arity: got=%q want ArgumentError", got)
	}
	if got := eval(t, `p (([1, 2].at; :no) rescue $!.class)`); got != "ArgumentError\n" {
		t.Errorf("at no-arg: got=%q want ArgumentError", got)
	}
	// A non-Integer index without #to_int raises TypeError.
	if got := eval(t, `p (([1, 2].at(:x); :no) rescue $!.class)`); got != "TypeError\n" {
		t.Errorf("at bad index: got=%q want TypeError", got)
	}
}

// TestArraySliceBang covers Array#slice!: element, span and Range removal,
// out-of-range nil, empty-but-valid range, #to_int Range bounds, and the frozen
// receiver check. Verified against ruby 4.0.6.
func TestArraySliceBang(t *testing.T) {
	cases := []struct{ src, want string }{
		{`a = [1, 2, 3]; r = a.slice!(1); p r; p a`, "2\n[1, 3]"},
		{`a = [1, 2, 3]; r = a.slice!(-1); p r; p a`, "3\n[1, 2]"},
		{`a = [1, 2, 3]; r = a.slice!(5); p r; p a`, "nil\n[1, 2, 3]"},
		{`a = [1, 2, 3]; r = a.slice!(1, 2); p r; p a`, "[2, 3]\n[1]"},
		{`a = [1, 2, 3]; r = a.slice!(1..2); p r; p a`, "[2, 3]\n[1]"},
		{`a = [1, 2, 3]; r = a.slice!(1..0); p r; p a`, "[]\n[1, 2, 3]"},
		{`a = [1, 2, 3]; r = a.slice!(9..10); p r; p a`, "nil\n[1, 2, 3]"},
		{`o = Object.new; def o.to_int; 2; end; a = [1, 2, 3]; p a.slice!(1..o); p a`, "[2, 3]\n[1]"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	if got := eval(t, `p (([1, 2].freeze.slice!(0); :no) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen slice!: got=%q want FrozenError", got)
	}
}

// TestArrayDeleteAt covers Array#delete_at: removal, negative index, out of
// range, #to_int coercion and the frozen receiver check. Verified against
// ruby 4.0.6.
func TestArrayDeleteAt(t *testing.T) {
	cases := []struct{ src, want string }{
		{`a = [1, 2, 3]; r = a.delete_at(1); p r; p a`, "2\n[1, 3]"},
		{`a = [1, 2, 3]; r = a.delete_at(-1); p r; p a`, "3\n[1, 2]"},
		{`a = [1, 2, 3]; r = a.delete_at(5); p r; p a`, "nil\n[1, 2, 3]"},
		{`o = Object.new; def o.to_int; 0; end; a = [1, 2, 3]; p a.delete_at(o); p a`, "1\n[2, 3]"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	if got := eval(t, `p (([1, 2].freeze.delete_at(0); :no) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen delete_at: got=%q want FrozenError", got)
	}
}

// TestArrayKeepIf covers Array#keep_if: it keeps elements the block accepts,
// always returns self, yields an Enumerator without a block, and raises on a
// frozen receiver. Verified against ruby 4.0.6.
func TestArrayKeepIf(t *testing.T) {
	cases := []struct{ src, want string }{
		{`a = [1, 2, 3, 4]; r = a.keep_if { |x| x.even? }; p r; p r.equal?(a)`, "[2, 4]\ntrue"},
		{`a = [1, 2, 3]; p a.keep_if { true }.equal?(a)`, "true"}, // unchanged still returns self
		{`p [1, 2, 3].keep_if.class`, "Enumerator"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	if got := eval(t, `p (([1, 2].freeze.keep_if { true }; :no) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen keep_if: got=%q want FrozenError", got)
	}
}

// TestArraySortByBang covers Array#sort_by!: in-place sort by the block key,
// returns self, yields an Enumerator without a block, tolerates the block
// mutating the array (no panic on the fixed snapshot), and raises on a frozen
// receiver. Verified against ruby 4.0.6.
func TestArraySortByBang(t *testing.T) {
	cases := []struct{ src, want string }{
		{`a = [3, 1, 2]; r = a.sort_by! { |x| x }; p r; p r.equal?(a)`, "[1, 2, 3]\ntrue"},
		{`a = %w[bb a ccc]; p a.sort_by!(&:length)`, `["a", "bb", "ccc"]`},
		{`p [3, 1, 2].sort_by!.class`, "Enumerator"},
		// The block growing the array mid-sort must not crash; the snapshot sorts.
		{`a = [3, 1, 2]; a.sort_by! { |x| a.push(9) if x == 3; x }; p a`, "[1, 2, 3]"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	if got := eval(t, `p (([1, 2].freeze.sort_by! { |x| x }; :no) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen sort_by!: got=%q want FrozenError", got)
	}
}
