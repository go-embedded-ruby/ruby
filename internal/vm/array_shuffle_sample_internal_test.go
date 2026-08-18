// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestArrayShuffle covers Array#shuffle / #shuffle! including the default
// generator, non-destructiveness, the frozen check, and the random: keyword
// protocol (a Float result scales, a #to_int result is a direct index, and an
// out-of-range index raises RangeError; a receiver without #rand raises
// NoMethodError). Verified against ruby 4.0.6.
func TestArrayShuffle(t *testing.T) {
	cases := []struct{ src, want string }{
		// Default generator: a permutation of the same values, receiver untouched.
		{`srand(1); a = [1, 2, 3, 4]; s = a.shuffle; p s.sort; p a`, "[1, 2, 3, 4]\n[1, 2, 3, 4]"},
		{`a = [1, 2, 3]; r = a.shuffle!; p r.equal?(a); p a.sort`, "true\n[1, 2, 3]"},
		{`p [].shuffle; p [7].shuffle`, "[]\n[7]"},
		// #shuffle returns a plain Array, never a subclass instance.
		{`p [1, 2, 3].shuffle.instance_of?(Array)`, "true"},
		// random: with a #rand returning a direct integer index (0 every draw
		// rotates the array, matching MRI's Fisher-Yates).
		{`o = Object.new; def o.rand(n); 0; end
		  p [1, 2, 3].shuffle(random: o)`, "[2, 3, 1]"},
		// A #rand of n-1 swaps each element with itself: the array is unchanged.
		{`o = Object.new; def o.rand(n); n - 1; end
		  p [1, 2, 3].shuffle(random: o)`, "[1, 2, 3]"},
		// random: with a #rand returning a Float (scaled to floor(f*bound)).
		{`o = Object.new; def o.rand(n); 0.0; end
		  p [1, 2, 3].shuffle(random: o).sort`, "[1, 2, 3]"},
		// random: whose #rand result converts via #to_int.
		{`v = Object.new; def v.to_int; 0; end
		  o = Object.new; def o.rand(n); @v; end; o.instance_variable_set(:@v, v)
		  p [1, 2].shuffle(random: o).sort`, "[1, 2]"},
		// A trailing hash without a random: key falls back to the default generator.
		{`srand(2); p [1, 2, 3].shuffle({}).sort`, "[1, 2, 3]"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// A random index out of [0, bound) raises RangeError.
	if got := eval(t, `o = Object.new; def o.rand(n); n; end
	                   p (([1, 2].shuffle(random: o); :no) rescue $!.class)`); got != "RangeError\n" {
		t.Errorf("shuffle rng too big: got=%q want RangeError", got)
	}
	if got := eval(t, `o = Object.new; def o.rand(n); -1; end
	                   p (([1, 2].shuffle(random: o); :no) rescue $!.class)`); got != "RangeError\n" {
		t.Errorf("shuffle rng negative: got=%q want RangeError", got)
	}
	// A receiver that does not define #rand raises NoMethodError.
	if got := eval(t, `p (([1, 2].shuffle(random: BasicObject.new); :no) rescue $!.class)`); got != "NoMethodError\n" {
		t.Errorf("shuffle no #rand: got=%q want NoMethodError", got)
	}
	if got := eval(t, `p (([1, 2].freeze.shuffle!; :no) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen shuffle!: got=%q want FrozenError", got)
	}
}

// TestArraySample covers Array#sample: the single-element form (nil when empty),
// the count form (#to_int coercion, negative-count ArgumentError, clamping to the
// size), a plain-Array result for a subclass, and the random: keyword protocol
// including RangeError and NoMethodError. Verified against ruby 4.0.6.
func TestArraySample(t *testing.T) {
	cases := []struct{ src, want string }{
		{`srand(1); p [42].sample; p [].sample`, "42\nnil"},
		{`p [1, 2, 3, 4].sample(2).size`, "2"},
		{`p [1, 2].sample(5).sort`, "[1, 2]"}, // count clamps to the size
		{`p [].sample(3)`, "[]"},
		{`p [1, 2, 3].sample(0)`, "[]"},
		// #to_int coerces a non-Integer count.
		{`o = Object.new; def o.to_int; 2; end; p [1, 2, 3].sample(o).size`, "2"},
		// random: single form uses #rand as an index.
		{`o = Object.new; def o.rand(n); 1; end; p [10, 20].sample(random: o)`, "20"},
		// random: whose #rand returns a Float scales it.
		{`o = Object.new; def o.rand(n); 0.5; end; p [10, 20].sample(random: o)`, "20"},
		// random: whose #rand result converts via #to_int.
		{`v = Object.new; def v.to_int; 1; end
		  o = Object.new; def o.rand(n); @v; end; o.instance_variable_set(:@v, v)
		  p [10, 20].sample(random: o)`, "20"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// A negative count raises ArgumentError.
	if got := eval(t, `p (([1, 2].sample(-1); :no) rescue $!.class)`); got != "ArgumentError\n" {
		t.Errorf("sample negative: got=%q want ArgumentError", got)
	}
	// An out-of-range #rand index raises RangeError.
	if got := eval(t, `o = Object.new; def o.rand(n); 2; end
	                   p (([1, 2].sample(random: o); :no) rescue $!.class)`); got != "RangeError\n" {
		t.Errorf("sample rng too big: got=%q want RangeError", got)
	}
	// A receiver without #rand raises NoMethodError.
	if got := eval(t, `p (([1, 2].sample(random: BasicObject.new); :no) rescue $!.class)`); got != "NoMethodError\n" {
		t.Errorf("sample no #rand: got=%q want NoMethodError", got)
	}
}
