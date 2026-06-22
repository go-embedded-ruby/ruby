package vm_test

import (
	"strings"
	"testing"
)

// TestConformanceCluster covers a batch of MRI-method gaps closed together:
// object_id/__id__, Float#floor/#ceil with ndigits, Array#sum with a block,
// Array#to_h, and Enumerable#minmax. Asserted against MRI Ruby 4.0.5.
func TestConformanceCluster(t *testing.T) {
	cases := []struct{ src, want string }{
		// object_id: immediate values get MRI's deterministic ids.
		{`p [nil.object_id, true.object_id, false.object_id, 0.object_id, 5.object_id, (-1).object_id]`, "[4, 20, 0, 1, 11, -1]\n"},
		{`p 5.__id__`, "11\n"}, // __id__ alias
		// stable per object, distinct across objects.
		{`o = Object.new; p o.object_id == o.object_id`, "true\n"},
		{`p :sym.object_id == :sym.object_id`, "true\n"},
		{`p "a".object_id != "a".object_id`, "true\n"}, // distinct String objects
		// a bignum is a heap object: id is not 2n+1, but is stable.
		{`b = 10 ** 20; p [b.object_id == 2 * b + 1, b.object_id == b.object_id]`, "[false, true]\n"},
		// Float#floor / #ceil with ndigits (ndigits>0 stays Float, else Integer).
		{`p [3.14.floor(1), 3.14.ceil(1), (-3.14).floor(1), 3.149.floor(2), 2.5.ceil(0)]`, "[3.1, 3.2, -3.2, 3.14, 3]\n"},
		// Array#sum with and without a block.
		{`p [1, 2, 3].sum { |x| x * 2 }`, "12\n"},
		{`p ["a", "b"].sum("")`, "\"ab\"\n"},
		{`p [1, 2, 3].sum`, "6\n"},
		// Array#to_h, with and without a mapping block.
		{`p [[1, 2], [3, 4]].to_h`, "{1 => 2, 3 => 4}\n"},
		{`p [1, 2, 3].to_h { |x| [x, x * x] }`, "{1 => 1, 2 => 4, 3 => 9}\n"},
		{`p [].to_h`, "{}\n"},
		// Enumerable#minmax over Array, Range and the empty case.
		{`p [3, 1, 2].minmax`, "[1, 3]\n"},
		{`p (1..5).minmax`, "[1, 5]\n"},
		{`p [].minmax`, "[nil, nil]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`[1, 2].to_h`, "wrong element type Integer at 0 (expected array)"},
		{`[[1, 2, 3]].to_h`, "wrong array length at 0 (expected 2, was 3)"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
