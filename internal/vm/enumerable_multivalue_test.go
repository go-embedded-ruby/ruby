package vm_test

import "testing"

// TestEnumerableMultiValueYield covers the fix for chaining an Enumerable method
// over an Enumerator that yields multiple values per step (the canonical case is
// each_with_index, which yields element + index). Enumerable now iterates through
// __each_packed, which packs a multi-value yield into an Array so a downstream
// multi-parameter block (`map { |x, i| }`) auto-splats it exactly as MRI does.
// Single-value sources and Hash (whose #each yields one [k, v] pair) are
// unaffected. Asserted against MRI Ruby 4.0.5.
func TestEnumerableMultiValueYield(t *testing.T) {
	cases := []struct{ src, want string }{
		// Multi-value enumerator (each_with_index) chained through Enumerable.
		{`p [1, 2, 3].each_with_index.map { |x, i| x * i }`, "[0, 2, 6]\n"},
		{`p [10, 20, 30].each_with_index.select { |x, i| i.even? }`, "[[10, 0], [30, 2]]\n"},
		{`p [1, 2, 3].each_with_index.reject { |x, i| i.even? }`, "[[2, 1]]\n"},
		{`p [1, 2, 3].each_with_index.find { |x, i| i == 1 }`, "[2, 1]\n"},
		{`p [1, 2, 3].each_with_index.flat_map { |x, i| [x, i] }`, "[1, 0, 2, 1, 3, 2]\n"},
		{`p [1, 2, 3].each_with_index.count { |x, i| i > 0 }`, "2\n"},
		{`p [5, 6, 7].each_with_index.to_a`, "[[5, 0], [6, 1], [7, 2]]\n"},
		{`p [1, 2, 3].each_with_index.partition { |x, i| i.even? }`, "[[[1, 0], [3, 2]], [[2, 1]]]\n"},
		{`p [1, 2, 3].each_with_index.each_with_object([]) { |(x, i), a| a << x * i }`, "[0, 2, 6]\n"},
		// Hash#each yields a [k, v] pair: still auto-splats to a 2-param block.
		{`p({a: 1, b: 2}.map { |k, v| [k, v * 2] })`, "[[:a, 2], [:b, 4]]\n"},
		{`p({a: 1, b: 2}.count { |k, v| v > 1 })`, "1\n"},
		{`p({a: 1, b: 2}.find { |k, v| v == 2 })`, "[:b, 2]\n"},
		{`p({a: 1, b: 2}.to_a)`, "[[:a, 1], [:b, 2]]\n"},
		{`p({a: 1, b: 2}.any? { |k, v| v > 1 })`, "true\n"},
		// Single-value sources (Enumerator over scalars, Range, Struct) unchanged.
		{`p [1, 2, 3].each.map { |x| x * 10 }`, "[10, 20, 30]\n"},
		{`p (1..5).map { |x| x * 2 }`, "[2, 4, 6, 8, 10]\n"},
		{`S = Struct.new(:a, :b); p S.new(1, 2).map { |x| x * 10 }`, "[10, 20]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// yieldsMulti is a custom Enumerable whose #each yields several values per step,
// used to prove the predicate value-packing rules below.
const yieldsMulti = "class YM\n include Enumerable\n def each; yield 1, 2; yield 3, 4, 5; yield 6, 7, 8, 9; end\nend\n"

// simpleEnum is a plain #each-only Enumerable over its constructor arguments.
const simpleEnum = "class SE\n include Enumerable\n def initialize(*a) @a = a end\n def each; @a.each { |x| yield x } end\nend\n"

// TestEnumerablePredicateValuePacking covers the MRI-3.4 value-packing and
// short-circuit rules of Enumerable#any?/all?/none?/one?/count added/reworked in
// the prelude: the block form forwards raw #each arguments (block arity governs,
// so a single-parameter block receives the first value), while the pattern and
// no-block forms match against the packed value; each predicate stops iterating
// as soon as its result is settled. Asserted against MRI Ruby 4.0.5.
func TestEnumerablePredicateValuePacking(t *testing.T) {
	cases := []struct{ src, want string }{
		// one?: no-block / block / pattern / empty.
		{simpleEnum + `p SE.new(false, nil, :x).one?`, "true\n"},
		{simpleEnum + `p SE.new(false, :x, :y).one?`, "false\n"},
		{simpleEnum + `p SE.new.one?`, "false\n"},
		{simpleEnum + `p SE.new(:a, :b, :c).one? { |s| s == :a }`, "true\n"},
		{simpleEnum + `p SE.new(:a, :b, :c).one? { |s| s == :a || s == :b }`, "false\n"},
		{simpleEnum + `p SE.new(1, 2, 3).one?(String)`, "false\n"},
		{simpleEnum + `p SE.new(nil, false, 1).one?(Integer)`, "true\n"},
		{simpleEnum + `begin; SE.new(1).one?(1, 2, 3); rescue ArgumentError; p :arg; end`, ":arg\n"},
		// one? short-circuits after the second truthy element.
		{simpleEnum + `seen = []; SE.new(1, 2, 3, 4).one? { |e| seen << e; true }; p seen`, "[1, 2]\n"},
		// block form forwards raw args: a |e| block gets the first value.
		{yieldsMulti + `seen = []; YM.new.one? { |e| seen << e; false }; p seen`, "[1, 3, 6]\n"},
		{yieldsMulti + `seen = []; YM.new.any? { |e| seen << e; false }; p seen`, "[1, 3, 6]\n"},
		{yieldsMulti + `seen = []; YM.new.all? { |e| seen << e; true }; p seen`, "[1, 3, 6]\n"},
		{yieldsMulti + `seen = []; YM.new.none? { |e| seen << e; false }; p seen`, "[1, 3, 6]\n"},
		// a |*args| block gets the whole gathered argument list.
		{yieldsMulti + `seen = []; YM.new.one? { |*a| seen << a; false }; p seen`, "[[1, 2], [3, 4, 5], [6, 7, 8, 9]]\n"},
		// any?/all?/none? short-circuit as soon as the result is settled.
		{simpleEnum + `seen = []; SE.new(1, 2, 3).any? { |e| seen << e; e == 2 }; p seen`, "[1, 2]\n"},
		{simpleEnum + `seen = []; SE.new(1, 2, 3).all? { |e| seen << e; e < 2 }; p seen`, "[1, 2]\n"},
		{simpleEnum + `seen = []; SE.new(1, 2, 3).none? { |e| seen << e; e == 2 }; p seen`, "[1, 2]\n"},
		// count: block form forwards raw args; item form compares the packed value.
		{yieldsMulti + `p YM.new.count { |e| e == 1 }`, "1\n"},
		{yieldsMulti + `p YM.new.count([1, 2])`, "1\n"},
		{simpleEnum + `p SE.new(1, 2, 2, 3).count(2)`, "2\n"},
		{simpleEnum + `p SE.new(1, 2, 3, 4).count`, "4\n"},
		{simpleEnum + `p SE.new(1, 2, 3, 4).count(&:even?)`, "2\n"},
		{simpleEnum + `begin; SE.new(1).count(1, 2); rescue ArgumentError; p :arg; end`, ":arg\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
