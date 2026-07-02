package vm_test

import (
	"strings"
	"testing"
)

// TestEnumerator covers blockless each returning an Enumerator and the
// Enumerator API: next/peek/rewind, to_a/size, Enumerable via #each, with_index
// and enum_for/to_enum — asserted against MRI Ruby 4.0.5.
func TestEnumerator(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [1, 2, 3].each.class`, "Enumerator\n"},
		{`p [1, 2, 3].each`, "#<Enumerator: [1, 2, 3]:each>\n"},                   // Inspect
		{`p [1, 2, 3].each_slice(2)`, "#<Enumerator: [1, 2, 3]:each_slice(2)>\n"}, // Inspect w/ args
		{`puts [1, 2, 3].each`, "#<Enumerator: [1, 2, 3]:each>\n"},                // ToS
		{`p([1, 2, 3].each ? "yes" : "no")`, "\"yes\"\n"},                         // Truthy
		{`e = [1, 2, 3].each; p [e.next, e.next, e.next]`, "[1, 2, 3]\n"},
		{`e = [1, 2, 3].each; p e.peek; p e.next; p e.peek`, "1\n1\n2\n"},
		{`e = [1, 2]; en = e.each; en.next; en.rewind; p en.next`, "1\n"},
		{`p [10, 20, 30].each.to_a`, "[10, 20, 30]\n"},
		{`p [1, 2, 3].each.size`, "3\n"},
		{`p (1..5).each.to_a`, "[1, 2, 3, 4, 5]\n"},
		{`p({a: 1, b: 2}.each.to_a)`, "[[:a, 1], [:b, 2]]\n"}, // multi-yield -> array element
		// Enumerable mixed in through #each:
		{`p [1, 2, 3, 4].each.select { |x| x.even? }`, "[2, 4]\n"},
		{`p [1, 2, 3].each.map { |x| x * 10 }`, "[10, 20, 30]\n"},
		{`p [1, 2, 3].each.reduce(:+)`, "6\n"},
		// with_index / each_with_index:
		{`p [10, 20, 30].each.with_index.to_a`, "[[10, 0], [20, 1], [30, 2]]\n"},
		{`p [10, 20].each.with_index(1).to_a`, "[[10, 1], [20, 2]]\n"},
		{`a = [1, 2, 3]; r = a.each.with_index { |e, i| }; p r.equal?(a)`, "true\n"},
		{`x = %w[a b]; p x.each.each_with_index.to_a`, "[[\"a\", 0], [\"b\", 1]]\n"},
		// with_index forwards to the underlying method, so map collects results:
		{`p [1, 2, 3].map.with_index { |x, i| [x, i] }`, "[[1, 0], [2, 1], [3, 2]]\n"},
		{`p [10, 20].map.with_index(1) { |x, i| x + i }`, "[11, 22]\n"},
		{`p [1, 2, 3, 4].select.with_index { |x, i| i.even? }`, "[1, 3]\n"},
		// enum_for / to_enum:
		{`p [1, 2, 3].enum_for.to_a`, "[1, 2, 3]\n"},       // default method is :each
		{`p [1, 2, 3].to_enum(:each).to_a`, "[1, 2, 3]\n"}, // explicit method
		// each with a block on the enumerator forwards to the underlying method;
		// with no block it returns the enumerator itself.
		{`p [1, 2, 3].each.each.to_a`, "[1, 2, 3]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// next/peek past the end raise StopIteration.
	for _, src := range []string{
		`e = [1].each; e.next; e.next`,
		`e = [1].each; e.next; e.peek`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "StopIteration") {
			t.Errorf("src=%q got=%v want StopIteration", src, err)
		}
	}
}
