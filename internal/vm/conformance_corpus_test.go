package vm_test

import (
	"strings"
	"testing"
)

// TestConformanceCorpusBatch covers the conformance gaps closed alongside the
// scripts/conformance/stdlib.txt differential corpus: String#squeeze with
// character-set arguments, Array#permutation, Hash#transform_keys with a mapping
// hash, Enumerator#first/#take on (possibly unbounded) enumerators,
// Float#rationalize, and Struct.new with a class body block. Each expectation is
// asserted against MRI Ruby 4.0.5.
func TestConformanceCorpusBatch(t *testing.T) {
	cases := []struct{ src, want string }{
		// String#squeeze with a character set: only listed runs collapse.
		{`p "aaabbbccc".squeeze("a")`, "\"abbbccc\"\n"},
		{`p "mississippi".squeeze("sp")`, "\"misisipi\"\n"},
		{`p "aaabbbccc".squeeze`, "\"abc\"\n"}, // no arg: collapse all runs
		// squeeze with an intersection of two sets collapses only common bytes.
		{`p "aaabbb".squeeze("ab", "a")`, "\"abbb\"\n"},
		// squeeze! mutates and returns the receiver, or nil when unchanged.
		{`s = "aaabbb"; s.squeeze!("a"); p s`, "\"abbb\"\n"},
		{`p "abc".squeeze!("a")`, "nil\n"},
		{`p "aaa".squeeze!("a")`, "\"a\"\n"},

		// Array#permutation: with k, with no k (full length), and as an enumerator.
		{`p [1, 2, 3].permutation(2).to_a`, "[[1, 2], [1, 3], [2, 1], [2, 3], [3, 1], [3, 2]]\n"},
		{`p [1, 2].permutation.to_a`, "[[1, 2], [2, 1]]\n"},
		{`p [1, 2, 3].permutation(0).to_a`, "[[]]\n"},
		{`p [1, 2].permutation(5).to_a`, "[]\n"}, // k > length yields nothing
		{`a = []; [1, 2].permutation(2) { |x| a << x }; p a`, "[[1, 2], [2, 1]]\n"},

		// Hash#transform_keys with a mapping hash, hash+block, and block only.
		{`p({a: 1, b: 2, c: 3}.transform_keys({a: :x, b: :y}))`, "{x: 1, y: 2, c: 3}\n"},
		{`p({a: 1, b: 2}.transform_keys({a: :x}) { |k| k.to_s })`, "{x: 1, \"b\" => 2}\n"},
		{`p({a: 1, b: 2}.transform_keys(&:to_s))`, "{\"a\" => 1, \"b\" => 2}\n"},
		{`h = {a: 1, b: 2}; h.transform_keys!({a: :x}); p h`, "{x: 1, b: 2}\n"},
		{`h = {a: 1}; h.transform_keys! { |k| k.to_s }; p h`, "{\"a\" => 1}\n"},

		// Enumerator#first / #take, including over an unbounded cycle.
		{`p [1, 2, 3].cycle.first(7)`, "[1, 2, 3, 1, 2, 3, 1]\n"},
		{`p [1, 2, 3].cycle.first`, "1\n"},
		{`p [1, 2, 3].cycle.take(4)`, "[1, 2, 3, 1]\n"},
		{`p [1, 2, 3].each.first(2)`, "[1, 2]\n"},
		{`p [].cycle.first(3)`, "[]\n"},
		{`p [1, 2, 3].each.first(0)`, "[]\n"},
		{`p [].each.first`, "nil\n"},
		// first/take on a generator-block enumerator (the e.block path).
		{`e = Enumerator.new { |y| i = 0; loop { y << (i += 1) } }; p e.first(3)`, "[1, 2, 3]\n"},
		{`e = Enumerator.new { |y| i = 0; loop { y << (i += 1) } }; p e.take(2)`, "[1, 2]\n"},
		// A finite generator: first(n) returns fewer elements when it runs out.
		{`e = Enumerator.new { |y| y << 1; y << 2 }; p e.first(5)`, "[1, 2]\n"},
		// An enumerator yielding several values at once collects them as arrays.
		{`p [10, 20, 30].each_with_index.first(2)`, "[[10, 0], [20, 1]]\n"},

		// Float#rationalize finds the simplest round-tripping rational.
		{`p 1.5.rationalize`, "(3/2)\n"},
		{`p 0.3.rationalize`, "(3/10)\n"},
		{`p 0.1.rationalize`, "(1/10)\n"},
		{`p 2.0.rationalize`, "(2/1)\n"},
		{`p 0.0.rationalize`, "(0/1)\n"},
		{`p (-1.5).rationalize`, "(-3/2)\n"},
		{`p 3.14.rationalize`, "(157/50)\n"},

		// Struct.new with a class body block adds methods to the new subclass.
		{`p Struct.new(:name, :age) { def greet; "#{name} is #{age}"; end }.new("Al", 30).greet`, "\"Al is 30\"\n"},
		{`s = Struct.new(:x) { def double; x * 2; end }; p s.new(5).double`, "10\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// Float#rationalize on non-finite values raises FloatDomainError.
	errs := []struct{ src, want string }{
		{`(0.0 / 0.0).rationalize`, "NaN"},
		{`(1.0 / 0.0).rationalize`, "Infinity"},
		{`(-1.0 / 0.0).rationalize`, "-Infinity"},
		// A non-stop error raised while pulling elements propagates out of #first.
		{`Enumerator.new { |y| y << 1; raise "boom" }.first(5)`, "boom"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
