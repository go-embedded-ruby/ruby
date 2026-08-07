package vm_test

import (
	"strings"
	"testing"
)

// TestLazy covers Enumerator::Lazy: deferred map/select/reject/filter_map/
// take_while/drop_while/take/drop chained over finite and infinite sources, the
// terminals first/to_a/force/each, and the source kinds (array, range — endless
// and Float::INFINITY-bounded — hash, enumerator). MRI Ruby 4.0.5.
func TestLazy(t *testing.T) {
	cases := []struct{ src, want string }{
		// Infinite ranges become usable.
		{`p (1..Float::INFINITY).lazy.map { |x| x * 2 }.first(5)`, "[2, 4, 6, 8, 10]\n"},
		{`p (1..).lazy.select { |x| x.even? }.first(3)`, "[2, 4, 6]\n"},
		{`p (1..Float::INFINITY).lazy.map { |x| x * x }.take(4).to_a`, "[1, 4, 9, 16]\n"},
		{`p (1..Float::INFINITY).lazy.select { |x| x % 3 == 0 }.map { |x| x * x }.first(4)`, "[9, 36, 81, 144]\n"},
		{`p (1..Float::INFINITY).lazy.take_while { |x| x < 5 }.to_a`, "[1, 2, 3, 4]\n"},
		{`p (1..Float::INFINITY).lazy.filter_map { |x| x * 2 if x.even? }.first(3)`, "[4, 8, 12]\n"},
		// Finite sources.
		{`p [1, 2, 3, 4].lazy.map { |x| x * 10 }.to_a`, "[10, 20, 30, 40]\n"},
		{`p [1, 2, 3].lazy.collect { |x| x + 1 }.to_a`, "[2, 3, 4]\n"},
		{`p (1...5).lazy.to_a`, "[1, 2, 3, 4]\n"},                                  // exclusive bounded range
		{`p (1..5.0).lazy.to_a`, "[1, 2, 3, 4, 5]\n"},                              // float (non-infinite) range end
		{`p [1, 2, 3].lazy.map { |x| x }`, "#<Enumerator::Lazy: [1, 2, 3]:map>\n"}, // inspect with ops
		{`p (1..20).lazy.reject { |x| x.even? }.take(3).to_a`, "[1, 3, 5]\n"},
		{`p (1..10).lazy.drop(3).first(2)`, "[4, 5]\n"},
		{`p (1..6).lazy.drop_while { |x| x < 3 }.to_a`, "[3, 4, 5, 6]\n"},
		{`p (1..10).lazy.filter { |x| x.even? }.first(2)`, "[2, 4]\n"},
		{`p (1..4).lazy.filter_map { |x| nil }.to_a`, "[]\n"},
		// first with no argument returns a single element (or nil when empty).
		{`p (1..10).lazy.select { |x| x > 5 }.first`, "6\n"},
		{`p [].lazy.first`, "nil\n"},
		{`p (1..3).lazy.select { |x| x > 10 }.first`, "nil\n"},
		// Hash and Enumerator sources (materialised).
		{`p({a: 1, b: 2}.lazy.map { |k, v| [k, v] }.to_a)`, "[[:a, 1], [:b, 2]]\n"},
		{`p({a: 1, b: 2}.lazy.to_a)`, "[[:a, 1], [:b, 2]]\n"},
		{`p [1, 2, 3].each.lazy.map { |x| x * 2 }.to_a`, "[2, 4, 6]\n"},
		// .lazy on a Lazy is itself; class and inspect.
		{`p [1, 2, 3].lazy.lazy.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].lazy.class`, "Enumerator::Lazy\n"},
		{`p [1, 2, 3].lazy`, "#<Enumerator::Lazy: [1, 2, 3]>\n"},
		{`p [1, 2, 3].lazy.each.class`, "Enumerator::Lazy\n"}, // each without block returns self
		{`p([1, 2, 3].lazy ? "y" : "n")`, "\"y\"\n"},          // Truthy
		// each with a block iterates.
		{`r = []; (1..3).lazy.map { |x| x * 2 }.each { |y| r << y }; p r`, "[2, 4, 6]\n"},
		// flat_map / collect_concat: one level of flattening; scalars pass through.
		{`p (1..Float::INFINITY).lazy.flat_map { |x| [x, -x] }.first(4)`, "[1, -1, 2, -2]\n"},
		{`p (1..3).lazy.flat_map { |x| x }.to_a`, "[1, 2, 3]\n"},
		{`p (1..3).lazy.collect_concat { |x| [x, x] }.to_a`, "[1, 1, 2, 2, 3, 3]\n"},
		{`p (1..Float::INFINITY).lazy.flat_map { |x| [x] }.take(2).to_a`, "[1, 2]\n"},
		// grep / grep_v: #=== pattern, with an optional mapping block; proc pattern.
		{`p (1..Float::INFINITY).lazy.grep(1..3).first(3)`, "[1, 2, 3]\n"},
		{`p (1..10).lazy.grep(->(x) { x.even? }).to_a`, "[2, 4, 6, 8, 10]\n"},
		{`p (1..3).lazy.grep(2) { |x| x * 100 }.to_a`, "[200]\n"},
		{`p (1..10).lazy.grep_v(1..5).to_a`, "[6, 7, 8, 9, 10]\n"},
		{`p (1..6).lazy.grep_v(->(x) { x.even? }) { |x| x * 10 }.to_a`, "[10, 30, 50]\n"},
		// zip: pads with nil past the shorter source; drives infinite self.
		{`p (1..Float::INFINITY).lazy.zip([10, 20, 30]).first(4)`, "[[1, 10], [2, 20], [3, 30], [4, nil]]\n"},
		{`p [1, 2].lazy.zip([3, 4], [5, 6]).to_a`, "[[1, 3, 5], [2, 4, 6]]\n"},
		{`p [1, 2, 3].lazy.zip.to_a`, "[[1], [2], [3]]\n"},
		{`p (1..Float::INFINITY).lazy.zip((10..)).first(2)`, "[[1, 10], [2, 11]]\n"},
		// uniq: with and without a key block; works on an infinite source.
		{`p [1, 1, 2, 3, 3, 4].lazy.uniq.to_a`, "[1, 2, 3, 4]\n"},
		{`p (1..Float::INFINITY).lazy.uniq { |x| x % 3 }.first(3)`, "[1, 2, 3]\n"},
		// compact: drops nils.
		{`p [1, nil, 2, nil, 3].lazy.compact.to_a`, "[1, 2, 3]\n"},
		// with_index: pairs by default; with a block it maps for side effects only
		// and yields the original items (MRI semantics), honouring the offset.
		{`p (1..Float::INFINITY).lazy.with_index.first(3)`, "[[1, 0], [2, 1], [3, 2]]\n"},
		{`p (1..Float::INFINITY).lazy.with_index(10).first(3)`, "[[1, 10], [2, 11], [3, 12]]\n"},
		{`r = []; p((1..3).lazy.with_index(5) { |x, i| r << [x, i] }.to_a); p r`,
			"[1, 2, 3]\n[[1, 5], [2, 6], [3, 7]]\n"},
		// first(0) / take(0) short-circuit to empty (no source pull).
		{`p (1..Float::INFINITY).lazy.first(0)`, "[]\n"},
		{`p (1..Float::INFINITY).lazy.take(0).to_a`, "[]\n"},
		// Longer mixed chain over an infinite source.
		{`p (1..Float::INFINITY).lazy.map { |x| x * 2 }.flat_map { |x| [x, x] }.select { |x| x > 4 }.first(4)`,
			"[6, 6, 8, 8]\n"},
		// chunk_while / slice_when: runs split at adjacent pairs. Truly lazy — a
		// completed run is emitted the moment its boundary is seen, so an infinite
		// source with finite runs terminates under first.
		{`p (1..Float::INFINITY).lazy.chunk_while { |a, b| b % 3 != 0 }.first(2)`, "[[1, 2], [3, 4, 5]]\n"},
		{`p [1, 1, 2, 3, 3, 4].lazy.chunk_while { |a, b| a == b }.to_a`, "[[1, 1], [2], [3, 3], [4]]\n"},
		{`p [3, 1, 4].lazy.chunk_while { |a, b| a < b }.force`, "[[3], [1, 4]]\n"},
		{`p [10].lazy.chunk_while { |a, b| true }.to_a`, "[[10]]\n"}, // single element → one run
		{`p [].lazy.chunk_while { |a, b| true }.to_a`, "[]\n"},       // empty → no runs
		{`p (1..Float::INFINITY).lazy.slice_when { |a, b| b % 3 == 0 }.first(2)`, "[[1, 2], [3, 4, 5]]\n"},
		{`p [1, 2, 4, 5, 7].lazy.slice_when { |a, b| b - a > 1 }.to_a`, "[[1, 2], [4, 5], [7]]\n"},
		// chunk: consecutive equal keys grouped as [key, [elems]]; nil/:_separator
		// drop, :_alone stands alone, lazy over an infinite source.
		{`p (1..Float::INFINITY).lazy.chunk { |x| x % 3 }.first(3)`, "[[1, [1]], [2, [2]], [0, [3]]]\n"},
		{`p [1, 2, 2, 3].lazy.chunk { |x| x }.force`, "[[1, [1]], [2, [2, 2]], [3, [3]]]\n"},
		{`p [1, 2, 3, 4, 5].lazy.chunk { |x| x < 3 ? :a : :_separator }.force`, "[[:a, [1, 2]]]\n"},
		{`p [1, 2, 3].lazy.chunk { |x| :_alone }.force`, "[[:_alone, [1]], [:_alone, [2]], [:_alone, [3]]]\n"},
		{`p [1, 2, 3].lazy.chunk { |x| nil }.force`, "[]\n"}, // all dropped
		{`p [].lazy.chunk { |x| x }.force`, "[]\n"},
		{`p [1, 2].lazy.chunk.class`, "Enumerator::Lazy\n"}, // no block → awaiting Lazy
		// chunk chained downstream: keys/runs feed further lazy ops.
		{`p (1..Float::INFINITY).lazy.chunk { |x| x % 2 }.select { |k, v| k == 0 }.first(2)`,
			"[[0, [2]], [0, [4]]]\n"},
		{`p (1..Float::INFINITY).lazy.chunk_while { |a, b| b % 3 != 0 }.map { |r| r.sum }.first(3)`,
			"[3, 12, 21]\n"},
		// slice_before / slice_after: block or pattern boundary; lazy over infinite.
		{`p (1..Float::INFINITY).lazy.slice_before { |x| x % 3 == 0 }.first(2)`, "[[1, 2], [3, 4, 5]]\n"},
		{`p [1, 2, 4, 9, 11].lazy.slice_before(&:even?).force`, "[[1], [2], [4, 9, 11]]\n"},
		{`p [1, 3, 5, 2].lazy.slice_before(->(x) { x.even? }).to_a`, "[[1, 3, 5], [2]]\n"}, // pattern arg
		{`p [1, 2, 4, 5].lazy.slice_before { |x| x > 3 }.first(1)`, "[[1, 2]]\n"},
		{`p (1..Float::INFINITY).lazy.slice_after { |x| x % 3 == 0 }.first(2)`, "[[1, 2, 3], [4, 5, 6]]\n"},
		{`p [1, 2, 4, 9, 10].lazy.slice_after(&:even?).force`, "[[1, 2], [4], [9, 10]]\n"},
		{`p [2, 4, 6].lazy.slice_after(&:even?).to_a`, "[[2], [4], [6]]\n"}, // every element a boundary
		{`p [].lazy.slice_before { |x| true }.to_a`, "[]\n"},
		{`p [].lazy.slice_after { |x| true }.to_a`, "[]\n"},
		// eager: returns an ordinary Enumerator that still drives the pipeline
		// element-by-element, so first/take stay usable on an infinite source.
		{`p (1..Float::INFINITY).lazy.eager.class`, "Enumerator\n"},
		{`p (1..Float::INFINITY).lazy.map { |x| x * 2 }.eager.first(3)`, "[2, 4, 6]\n"},
		{`p (1..Float::INFINITY).lazy.select { |x| x.even? }.eager.take(3)`, "[2, 4, 6]\n"},
		{`p [1, 2, 3].lazy.map { |x| x * 10 }.eager.to_a`, "[10, 20, 30]\n"},
		// A completed run may itself satisfy the first/take quota — both while
		// pulling (a :_alone element that first flushes the open run) and during the
		// finish flush of the last buffered run.
		{`p [1, 1, 2].lazy.chunk { |x| x == 2 ? :_alone : 1 }.first(1)`, "[[1, [1, 1]]]\n"},
		{`p [1, 3].lazy.chunk_while { |a, b| false }.first(2)`, "[[1], [3]]\n"},
		{`p [1, 1].lazy.chunk { |x| x }.first(1)`, "[[1, [1, 1]]]\n"},
		// slice_after with a pattern argument (rather than a block).
		{`p [1, 3, 4].lazy.slice_after(->(x) { x.even? }).to_a`, "[[1, 3, 4]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// A lazy transform without a block raises ArgumentError.
	for _, src := range []string{`[1, 2, 3].lazy.map`, `[1, 2, 3].lazy.flat_map`} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "without a block") {
			t.Errorf("src=%q got %v want ArgumentError (without a block)", src, err)
		}
	}
	// grep / grep_v require a pattern argument.
	for _, src := range []string{`[1, 2, 3].lazy.grep`, `[1, 2, 3].lazy.grep_v`} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "wrong number of arguments") {
			t.Errorf("src=%q got %v want ArgumentError (wrong number of arguments)", src, err)
		}
	}
	// Non-integer range endpoints can't be iterated (the same TypeError each/step
	// raise for non-integer ranges).
	for _, src := range []string{`("a".."e").lazy.first`, `(1.."z").lazy.first`} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "can't iterate") {
			t.Errorf("src=%q got %v want TypeError", src, err)
		}
	}
	// chunk_while / slice_when require a block.
	for _, src := range []string{`[1, 2, 3].lazy.chunk_while`, `[1, 2, 3].lazy.slice_when`} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "without a block") {
			t.Errorf("src=%q got %v want ArgumentError (without a block)", src, err)
		}
	}
	// slice_before / slice_after need exactly one of a block or a pattern arg.
	for _, src := range []string{
		`[1, 2, 3].lazy.slice_before`,            // neither → given 0, expected 1
		`[1, 2, 3].lazy.slice_after`,             // neither → given 0, expected 1
		`[1, 2, 3].lazy.slice_before(1, 2)`,      // two args → given 2, expected 1
		`[1, 2, 3].lazy.slice_before(1) { |x| }`, // both → given 1, expected 0
		`[1, 2, 3].lazy.slice_after(1) { |x| }`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "wrong number of arguments") {
			t.Errorf("src=%q got %v want ArgumentError (wrong number of arguments)", src, err)
		}
	}
	// chunk: a Symbol beginning with an underscore (other than :_separator /
	// :_alone) is reserved.
	if err := runErr(t, `[1].lazy.chunk { |x| :_reserved }.force`); err == nil ||
		!strings.Contains(err.Error(), "reserved") {
		t.Errorf("chunk reserved-symbol: got %v want RuntimeError (reserved)", err)
	}
}
