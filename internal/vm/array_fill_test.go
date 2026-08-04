// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestArrayFill covers Array#fill in every MRI form: the no-block filler forms
// (obj / obj,start / obj,start,length / obj,range) and the block forms, including
// negative indices, nil length, growth with nil padding, and endless/beginless
// ranges — asserted against MRI Ruby 3.4.
func TestArrayFill(t *testing.T) {
	cases := []struct{ src, want string }{
		// Whole-array fills.
		{`p([1, 2, 3].fill(:a))`, "[:a, :a, :a]\n"},
		{`a = [1, 2, 3]; a.fill(9); p(a.equal?(a))`, "true\n"}, // returns self
		// start / length.
		{`p([1, 2, 3, 4, 5, 6].fill("x", 2, 3))`, "[1, 2, \"x\", \"x\", \"x\", 6]\n"},
		{`p([1, 2, 3].fill("x", 1))`, "[1, \"x\", \"x\"]\n"},
		{`p([1, 2, 3].fill("x", 1, nil))`, "[1, \"x\", \"x\"]\n"},
		{`p([1, 2, 3].fill("y", nil))`, "[\"y\", \"y\", \"y\"]\n"},
		// Negative start.
		{`p([1, 2, 3, 4, 5].fill("x", -2))`, "[1, 2, 3, \"x\", \"x\"]\n"},
		{`p([1, 2, 3, 4, 5].fill("a", -25, 3))`, "[\"a\", \"a\", \"a\", 4, 5]\n"},
		// start past the end with no length is a no-op; with length it pads nils.
		{`p([1, 2, 3, 4, 5].fill("a", 5))`, "[1, 2, 3, 4, 5]\n"},
		{`p([1, 2, 3, 4, 5].fill("a", 8, 5))`, "[1, 2, 3, 4, 5, nil, nil, nil, \"a\", \"a\", \"a\", \"a\", \"a\"]\n"},
		{`a = [1, 2, 3]; a.fill("a", 0, 10); p(a.size)`, "10\n"},
		// Zero / negative length is a no-op.
		{`p([1, 2, 3, 4, 5].fill("a", 2, 0))`, "[1, 2, 3, 4, 5]\n"},
		{`p([1, 2, 3, 4, 5].fill("a", 2, -2))`, "[1, 2, 3, 4, 5]\n"},
		{`p([1, 2, 3, 4].fill("a", 3, -10000))`, "[1, 2, 3, 4]\n"},
		// Range forms.
		{`p([1, 2, 3, 4, 5, 6].fill(8, 0..3))`, "[8, 8, 8, 8, 5, 6]\n"},
		{`p([1, 2, 3, 4, 5, 6].fill(8, 0...3))`, "[8, 8, 8, 4, 5, 6]\n"},
		{`p([1, 2, 3, 4, 5, 6].fill("x", -2..-1))`, "[1, 2, 3, 4, \"x\", \"x\"]\n"},
		{`p([1, 2, 3].fill("x", 1..6))`, "[1, \"x\", \"x\", \"x\", \"x\", \"x\", \"x\"]\n"},
		{`p([1, 2, 3, 4, 5, 6].fill("x", 2...2))`, "[1, 2, 3, 4, 5, 6]\n"}, // zero width
		// Endless / beginless ranges.
		{`p([1, 2, 3, 4, 5].fill("x", 2..))`, "[1, 2, \"x\", \"x\", \"x\"]\n"},
		{`p([1, 2, 3, 4, 5].fill("x", ..2))`, "[\"x\", \"x\", \"x\", 4, 5]\n"},
		// Block forms.
		{`p([nil, nil, nil, nil].fill { |i| i * 2 })`, "[0, 2, 4, 6]\n"},
		{`p([1, 2, 3].fill(1) { |i| i * 2 })`, "[1, 2, 4]\n"},
		{`p([true, false, true, false, true, false, true].fill(1, 4) { |i| i + 3 })`,
			"[true, 4, 5, 6, 7, false, true]\n"},
		{`p([1, 1, 1, 1, 1, 1].fill(1..6) { |i| i + 1 })`, "[1, 2, 3, 4, 5, 6, 7]\n"},
		{`p([1, 2, 3, 4, 5].fill(2, 0) { |i| 99 })`, "[1, 2, 3, 4, 5]\n"}, // no-op length
		// A no-block Range filler fills with the Range object itself.
		{`p([1, 2, 3].fill(1..2))`, "[1..2, 1..2, 1..2]\n"},
		// #to_int coercion of start and length.
		{`o = Object.new; def o.to_int; 2; end; p([1, 2, 3, 4, 5].fill("z", o, o))`,
			"[1, 2, \"z\", \"z\", 5]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestArrayFillErrors covers Array#fill's error and boundary branches.
func TestArrayFillErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		// Argument-count errors.
		{`begin; [].fill; rescue ArgumentError; puts "AE"; end`, "AE\n"},
		{`begin; [].fill("a", 1, 2, true); rescue ArgumentError; puts "AE"; end`, "AE\n"},
		{`begin; [].fill(1, 2, true) { |i| }; rescue ArgumentError; puts "AE"; end`, "AE\n"},
		// Non-numeric start / length -> TypeError.
		{`begin; [].fill("a", true); rescue TypeError; puts "TE"; end`, "TE\n"},
		{`begin; [1, 2, 3].fill("x", 1, "foo"); rescue TypeError; puts "TE"; end`, "TE\n"},
		// A Bignum length -> RangeError; a fixnum-huge length -> "array size too big".
		{`begin; [1, 2, 3].fill(10, 1, 2 ** 64); rescue RangeError; puts "RE"; end`, "RE\n"},
		{`begin; [1, 2, 3].fill(10, 1, 1 << 50); rescue ArgumentError; puts "AE"; end`, "AE\n"},
		// A range whose begin is before the start of the array -> RangeError.
		{`begin; [1, 2, 3].fill("x", -5..-3); rescue RangeError; puts "RE"; end`, "RE\n"},
		// A block raising mid-fill leaves the already-filled elements in place.
		{`a = [1, 2, 3, 4]
begin
  a.fill { |i| i == 2 ? raise("stop") : -(i + 1) }
rescue
end
p(a)`, "[-1, -2, 3, 4]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
