package vm_test

import (
	"strings"
	"testing"
)

// TestArrayMutators covers the in-place Array methods pop/shift/unshift/prepend/
// delete/delete_if/concat/clear/rotate!/reverse_each. Asserted against MRI Ruby
// 4.0.5.
func TestArrayMutators(t *testing.T) {
	cases := []struct{ src, want string }{
		// pop / shift: single, count form, count clamp, and empty.
		{`a = [1, 2, 3]; p [a.pop, a]`, "[3, [1, 2]]\n"},
		{`a = [1, 2, 3, 4]; p [a.pop(2), a]`, "[[3, 4], [1, 2]]\n"},
		{`a = [1, 2]; p [a.pop(5), a]`, "[[1, 2], []]\n"}, // count clamps to length
		{`a = [1, 2, 3]; p [a.shift, a]`, "[1, [2, 3]]\n"},
		{`a = [1, 2, 3, 4]; p [a.shift(2), a]`, "[[1, 2], [3, 4]]\n"},
		{`a = [1, 2]; p [a.shift(5), a]`, "[[1, 2], []]\n"}, // count clamps to length
		{`p [[].pop, [].shift]`, "[nil, nil]\n"},
		// unshift / prepend.
		{`a = [1, 2]; a.unshift(0); a.prepend(-2, -1); p a`, "[-2, -1, 0, 1, 2]\n"},
		// delete: found returns the value; missing returns nil or a block's value.
		{`a = [1, 2, 2, 3]; p [a.delete(2), a]`, "[2, [1, 3]]\n"},
		{`p [[1, 2].delete(9), [1, 2].delete(9) { :none }]`, "[nil, :none]\n"},
		// delete_if: block filters in place; no block yields an Enumerator.
		{`a = [1, 2, 3, 4]; a.delete_if(&:even?); p a`, "[1, 3]\n"},
		{`p [1, 2].delete_if.class`, "Enumerator\n"},
		// concat: several arrays; clear empties.
		{`a = [1, 2]; a.concat([3, 4], [5]); p a`, "[1, 2, 3, 4, 5]\n"},
		{`a = [1, 2, 3]; p [a.clear, a]`, "[[], []]\n"},
		// rotate!: default, explicit, negative, and empty.
		{`a = [1, 2, 3, 4]; a.rotate!; p a`, "[2, 3, 4, 1]\n"},
		{`a = [1, 2, 3, 4]; a.rotate!(-1); p a`, "[4, 1, 2, 3]\n"},
		{`a = []; a.rotate!; p a`, "[]\n"},
		// reverse_each: block iterates in reverse; no block yields an Enumerator.
		{`r = []; [1, 2, 3].reverse_each { |x| r << x }; p r`, "[3, 2, 1]\n"},
		{`p [[1, 2, 3].reverse_each.to_a, [1, 2, 3].reverse_each.map { |x| x * 2 }]`, "[[3, 2, 1], [6, 4, 2]]\n"},
		// flatten!: mutates in place, returning self when it flattened, nil when
		// there was nothing to flatten (including a depth-0 / empty / flat array),
		// and an optional depth limits the recursion.
		{`a = [1, [2, [3, 4]], 5]; p [a.flatten!, a]`, "[[1, 2, 3, 4, 5], [1, 2, 3, 4, 5]]\n"},
		{`a = [1, 2, 3]; p [a.flatten!, a]`, "[nil, [1, 2, 3]]\n"},
		{`p [[].flatten!, [1, [2]].flatten!(0)]`, "[nil, nil]\n"},
		{`a = [1, [2, [3]]]; p [a.flatten!(1), a]`, "[[1, 2, [3]], [1, 2, [3]]]\n"},
		// flatten! detects a change even when the length is unchanged ([[1]] -> [1]).
		{`a = [[1]]; p [a.flatten!, a]`, "[[1], [1]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`[1, 2].pop(-1)`, "negative array size"},
		{`[1, 2].shift(-1)`, "negative array size"},
		{`[1, 2].concat(5)`, "no implicit conversion of Integer into Array"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
