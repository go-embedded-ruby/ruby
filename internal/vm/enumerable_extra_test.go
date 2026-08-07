package vm_test

import "testing"

// TestEnumerableExtraMethods covers Enumerable methods added in the prelude so
// every Enumerable (Range, Hash, Struct, Enumerator) gains them, not just Array:
// find_index, find_all, take_while, drop_while, each_slice, each_cons,
// chunk_while and slice_when. Asserted against MRI Ruby 4.0.5.
func TestEnumerableExtraMethods(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p (1..5).find_index { |x| x > 3 }`, "3\n"},
		{`p (1..5).find_index(3)`, "2\n"},
		{`p (1..5).find_index { |x| x > 99 }`, "nil\n"},
		{`p (1..5).find_all(&:even?)`, "[2, 4]\n"},
		{`p (1..5).take_while { |x| x < 3 }`, "[1, 2]\n"},
		{`p (1..5).drop_while { |x| x < 3 }`, "[3, 4, 5]\n"},
		{`p (1..5).each_slice(2).to_a`, "[[1, 2], [3, 4], [5]]\n"},
		{`r = []; (1..5).each_slice(2) { |s| r << s }; p r`, "[[1, 2], [3, 4], [5]]\n"},
		{`p (1..7).each_cons(3).to_a`, "[[1, 2, 3], [2, 3, 4], [3, 4, 5], [4, 5, 6], [5, 6, 7]]\n"},
		{`p (1..10).chunk_while { |a, b| b - a == 1 }.to_a`, "[[1, 2, 3, 4, 5, 6, 7, 8, 9, 10]]\n"},
		{`p [1, 2, 4, 5, 7].chunk_while { |a, b| b - a == 1 }.to_a`, "[[1, 2], [4, 5], [7]]\n"},
		{`p (1..5).slice_when { |a, b| b.even? }.to_a`, "[[1], [2, 3], [4, 5]]\n"},
		{`p [].chunk_while { |a, b| true }.to_a`, "[]\n"},
		// find_index / find_all are not native on Array, so the prelude serves it too.
		{`p [1, 2, 3].find_index(2)`, "1\n"},
		{`p [3, 1, 4, 1, 5].find_all { |x| x > 2 }`, "[3, 4, 5]\n"},
		// Works over a multi-value enumerator (element + index) via __each_packed.
		{`p [10, 20, 30].each_with_index.find_index { |x, i| i == 2 }`, "2\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// customEnum defines a fresh #each-only Enumerable used to prove the prelude's
// first/take/drop work over any Enumerable, not just Array/Range.
const customEnum = "class E\n include Enumerable\n def initialize(*a) @a=a end\n def each; @a.each { |x| yield x } end\nend\n"

// TestEnumerableFirstTakeDrop covers Enumerable#first/#take/#drop added in the
// prelude: leading elements over any Enumerable, count coercion via #to_int,
// negative-count and non-numeric errors, early termination, gathered multi
// yields, and the RangeError for out-of-range counts. Asserted against MRI 4.0.5.
func TestEnumerableFirstTakeDrop(t *testing.T) {
	cases := []struct{ src, want string }{
		// first with no argument returns the leading element (or nil when empty).
		{customEnum + `p E.new(2, 3, 4).first`, "2\n"},
		{customEnum + `p E.new.first`, "nil\n"},
		// first / take with a count return leading Arrays; drop returns the rest.
		{customEnum + `p E.new(4, 3, 2, 1, 0).first(2)`, "[4, 3]\n"},
		{customEnum + `p E.new(4, 3, 2, 1, 0).take(3)`, "[4, 3, 2]\n"},
		{customEnum + `p E.new(4, 3, 2, 1, 0).drop(2)`, "[2, 1, 0]\n"},
		{customEnum + `p E.new(1, 2).first(0)`, "[]\n"},
		{customEnum + `p E.new(1, 2).take(0)`, "[]\n"},
		{customEnum + `p E.new(1, 2, 3).first(9)`, "[1, 2, 3]\n"},
		{customEnum + `p E.new(1, 2, 3).drop(9)`, "[]\n"},
		{customEnum + `p E.new(1, 2, 3).drop(0)`, "[1, 2, 3]\n"},
		// #to_int coercion: a Float truncates, an object answering #to_int works.
		{customEnum + `p E.new(4, 3, 2, 1).take(2.9)`, "[4, 3]\n"},
		{customEnum + `p E.new(4, 3, 2, 1).drop(2.9)`, "[2, 1]\n"},
		{customEnum + `class I; def to_int; 2; end; end; p E.new(4, 3, 2, 1).take(I.new)`, "[4, 3]\n"},
		{customEnum + `class I; def to_int; 1; end; end; p E.new(4, 3, 2, 1).drop(I.new)`, "[3, 2, 1]\n"},
		// multi-value yields gather into whole Arrays.
		{"class M\n include Enumerable\n def each; yield 1, 2; yield 3, 4; end\nend\np M.new.take(1)", "[[1, 2]]\n"},
		// errors: negative count, non-numeric, wrong arity, out-of-range.
		{customEnum + `begin; E.new(1).take(-1); rescue ArgumentError; p :arg; end`, ":arg\n"},
		{customEnum + `begin; E.new(1).drop(-1); rescue ArgumentError; p :arg; end`, ":arg\n"},
		{customEnum + `begin; E.new(1).first(1, 2); rescue ArgumentError; p :arg; end`, ":arg\n"},
		{customEnum + `begin; E.new(1).take("x"); rescue TypeError; p :type; end`, ":type\n"},
		{customEnum + `begin; E.new(1).drop(nil); rescue TypeError; p :type; end`, ":type\n"},
		{customEnum + `class B; def to_int; "no"; end; end; begin; E.new(1).take(B.new); rescue TypeError; p :type; end`, ":type\n"},
		{customEnum + `begin; E.new.first(0x8000_0000_0000_0000); rescue RangeError; p :range; end`, ":range\n"},
		{customEnum + `begin; E.new.drop(-0x8000_0000_0000_0001); rescue RangeError; p :range; end`, ":range\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestEnumerableUniqCompact covers Enumerable#uniq and #compact added in the
// prelude: uniq removes duplicates (by value, or by a block's key) keeping the
// first occurrence and gathers multi-value yields into whole Arrays; compact
// drops nil (including a zero-argument yield). Asserted against MRI Ruby 4.0.5.
func TestEnumerableUniqCompact(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [1, 2, 2, 3, 3, 3, 1].each.uniq`, "[1, 2, 3]\n"},
		{`p [1.0, 1].each.uniq`, "[1.0, 1]\n"}, // eql? semantics: 1.0 and 1 differ
		{`p [0, 1, 2, 3].each.uniq { |n| n.even? }`, "[0, 1]\n"},
		{`p({a: 1, b: 1, c: 2}.uniq { |_, v| v })`, "[[:a, 1], [:c, 2]]\n"},
		{`p Enumerator.new { |y| y.yield 1, 2 }.uniq`, "[[1, 2]]\n"},
		{`p Enumerator.new { |y| y.yield; y.yield :v }.uniq`, "[nil, :v]\n"},
		{`p [nil, 1, 2, nil, true, false].each.compact`, "[1, 2, true, false]\n"},
		{`p Enumerator.new { |y| y.yield 1, 2 }.compact`, "[[1, 2]]\n"},
		{`p Enumerator.new { |y| y.yield; y.yield :v }.compact`, "[:v]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
