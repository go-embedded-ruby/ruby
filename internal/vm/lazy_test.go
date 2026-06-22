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
		{`p (1...5).lazy.to_a`, "[1, 2, 3, 4]\n"},      // exclusive bounded range
		{`p (1..5.0).lazy.to_a`, "[1, 2, 3, 4, 5]\n"}, // float (non-infinite) range end
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
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// A lazy transform without a block raises ArgumentError.
	if err := runErr(t, `[1, 2, 3].lazy.map`); err == nil || !strings.Contains(err.Error(), "without a block") {
		t.Errorf("lazy map no block: got %v want ArgumentError", err)
	}
	// Non-integer range endpoints can't be iterated (the same TypeError each/step
	// raise for non-integer ranges).
	for _, src := range []string{`("a".."e").lazy.first`, `(1.."z").lazy.first`} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "can't iterate") {
			t.Errorf("src=%q got %v want TypeError", src, err)
		}
	}
}
