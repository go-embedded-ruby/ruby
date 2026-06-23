package vm_test

import (
	"strings"
	"testing"
)

// TestEnumeratorNew covers the generator form Enumerator.new { |y| … }: the
// yielder (<< chaining, #yield, inspect, truthiness) and the enumerator's
// materialisation (to_a/next/map/each/with_index). Asserted against MRI Ruby 4.0.5.
func TestEnumeratorNew(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Enumerator.new { |y| y << 1; y << 2 }.to_a`, "[1, 2]\n"},
		{`e = Enumerator.new { |y| y << 1; y << 2 }; p [e.next, e.next]`, "[1, 2]\n"},
		{`p Enumerator.new { |y| y.yield(10); y.yield(20) }.to_a`, "[10, 20]\n"}, // #yield
		{`p Enumerator.new { |y| y << 1 << 2 << 3 }.to_a`, "[1, 2, 3]\n"},        // << chains
		{`p Enumerator.new { |y| 3.times { |i| y << i * i } }.map { |x| x + 1 }`, "[1, 2, 5]\n"},
		{`r = []; Enumerator.new { |y| y << :a; y << :b }.each { |x| r << x }; p r`, "[:a, :b]\n"},
		{`p Enumerator.new { |y| y << [1, 2]; y << [3, 4] }.to_a`, "[[1, 2], [3, 4]]\n"}, // multi-value
		{`r = []; Enumerator.new { |y| y << 5; y << 6 }.each_with_index { |x, i| r << [x, i] }; p r`, "[[5, 0], [6, 1]]\n"},
		{`p Enumerator.new { |y| y << 1; y << 2 }.with_index(10).to_a`, "[[1, 10], [2, 11]]\n"},
		// No block: #each returns the enumerator itself.
		{`e = Enumerator.new { |y| y << 1 }; p e.each.equal?(e)`, "true\n"},
		// Yielder truthiness and inspect.
		{`p Enumerator.new { |y| y << (y ? :yes : :no); y << y.inspect }.to_a`, "[:yes, \"#<Enumerator::Yielder>\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// Enumerator.new requires a block.
	if err := runErr(t, `Enumerator.new`); err == nil || !strings.Contains(err.Error(), "wrong number of arguments") {
		t.Errorf("Enumerator.new (no block) err=%v, want an ArgumentError", err)
	}
}
