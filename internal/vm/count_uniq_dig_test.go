package vm_test

import "testing"

// TestCountUniqDig covers Enumerable#count with an item/block, Array#uniq /
// #uniq! with a block, and Array#dig. Asserted against MRI Ruby 4.0.5.
func TestCountUniqDig(t *testing.T) {
	cases := []struct{ src, want string }{
		// count: no arg (length), an item (==), or a block (truthy).
		{`p [[1, 2, 2, 3, 2].count(2), [1, 2, 3, 4].count { |x| x.even? }, [1, 2, 3].count]`, "[3, 2, 3]\n"},
		{`p [(1..10).count { |x| x % 3 == 0 }, {a: 1, b: 2, c: 1}.count { |k, v| v == 1 }]`, "[3, 2]\n"},
		{`p ["x", "x", "y"].count("x")`, "2\n"},
		// uniq / uniq! with a block: distinguish by the block's value.
		{`p [[1, 2, 3, 4, 5].uniq { |x| x % 2 }, ["a", "bb", "cc", "d"].uniq { |s| s.length }]`, "[[1, 2], [\"a\", \"bb\"]]\n"},
		{`a = [1, 2, 3, 4]; a.uniq! { |x| x.even? }; p a`, "[1, 2]\n"},
		{`p [1, 2, 3].uniq! { |x| x }`, "nil\n"}, // no duplicates -> nil
		// plain uniq / uniq! unchanged.
		{`p [1, 1, 2, 3, 3].uniq`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].uniq!`, "nil\n"},
		// dig through nested arrays and hashes.
		{`p [[[1, [2, 3]]].dig(0, 1, 0), [[1, 2], [3, 4]].dig(1, 0), [1, 2, 3].dig(5), [{a: [1, 2]}].dig(0, :a, 1)]`, "[2, 3, nil, 2]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
