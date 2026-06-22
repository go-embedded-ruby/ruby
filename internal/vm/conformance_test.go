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

// TestConformanceCluster2 covers a second probe batch: Integer/Float#fdiv and
// Array#transpose / #product / #combination. Asserted against MRI Ruby 4.0.5.
func TestConformanceCluster2(t *testing.T) {
	cases := []struct{ src, want string }{
		// fdiv on Integer, Float and Bignum.
		{`p [5.fdiv(2), 10.fdiv(4), 3.14.fdiv(2)]`, "[2.5, 2.5, 1.57]\n"},
		{`p (10 ** 20).fdiv(2) > 0`, "true\n"},
		// transpose.
		{`p [[1, 2], [3, 4]].transpose`, "[[1, 3], [2, 4]]\n"},
		{`p [[1, 2, 3], [4, 5, 6]].transpose`, "[[1, 4], [2, 5], [3, 6]]\n"},
		{`p [].transpose`, "[]\n"},
		// product: variadic, and the lone-receiver form.
		{`p [1, 2, 3].product([4, 5])`, "[[1, 4], [1, 5], [2, 4], [2, 5], [3, 4], [3, 5]]\n"},
		{`p [1, 2].product([3, 4], [5, 6]).length`, "8\n"},
		{`p [1, 2].product`, "[[1], [2]]\n"},
		// combination: enumerator (no block), block form, and the edge sizes.
		{`p [1, 2, 3, 4].combination(2).to_a`, "[[1, 2], [1, 3], [1, 4], [2, 3], [2, 4], [3, 4]]\n"},
		{`p [1, 2, 3].combination(0).to_a`, "[[]]\n"},
		{`p [1, 2, 3].combination(3).to_a`, "[[1, 2, 3]]\n"},
		{`p [1, 2, 3].combination(5).to_a`, "[]\n"},      // k > length
		{`p [1, 2, 3].combination(-1).to_a`, "[]\n"},     // negative
		{`r = []; [1, 2, 3].combination(2) { |c| r << c.sum }; p r`, "[3, 4, 5]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`5.fdiv("x")`, "String can't be coerced into Integer"},
		{`3.14.fdiv("x")`, "String can't be coerced into Float"},
		{`[1, 2].transpose`, "no implicit conversion of Integer into Array"},
		{`[[1, 2], [3]].transpose`, "element size differs (1 should be 2)"},
		{`[1, 2].product(3)`, "no implicit conversion of Integer into Array"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

// TestConformanceCluster3: String#succ/#next, String-range iteration (built on
// succ), and Hash[...]. Asserted against MRI Ruby 4.0.5.
func TestConformanceCluster3(t *testing.T) {
	cases := []struct{ src, want string }{
		// String#succ / #next: carry, full-carry insertion, mixed and non-alnum.
		{`p ["az".succ, "zz".succ, "a9".succ, "Zz".succ, "99".succ, "Az".succ, "z".succ]`,
			`["ba", "aaa", "b0", "AAa", "100", "Ba", "aa"]` + "\n"},
		{`p ["abc".next, "1.9".succ, "<<koala>>".succ, "##".succ, "".succ]`,
			`["abd", "2.0", "<<koalb>>", "\#$", ""]` + "\n"},
		// String ranges iterate via succ (comparison-first, post-succ length guard).
		{`p ("a".."e").to_a`, `["a", "b", "c", "d", "e"]` + "\n"},
		{`p ("a"..."d").to_a`, `["a", "b", "c"]` + "\n"},
		{`p ("az".."bb").to_a`, `["az", "ba", "bb"]` + "\n"},
		{`p ("aa".."b").to_a`, `["aa"]` + "\n"},     // post-succ length guard
		{`p ("y".."aaa").to_a`, "[]\n"},             // begin already past end
		{`p ("a".."A").to_a`, "[]\n"},               // reversed
		{`p ("".."b").to_a`, `[""]` + "\n"},         // no-progress (empty succ)
		{`p ("a".."zz").to_a.length`, "702\n"},      // 26 + 26*26
		{`p ("a".."e").map { |c| c.upcase }`, `["A", "B", "C", "D", "E"]` + "\n"},
		// Hash[...]: array-of-pairs, key/value list, hash copy, short pair.
		{`p Hash[[[:a, 1], [:b, 2]]]`, "{a: 1, b: 2}\n"},
		{`p Hash[:a, 1, :b, 2]`, "{a: 1, b: 2}\n"},
		{`p Hash["x", 9]`, `{"x" => 9}` + "\n"},
		{`p Hash[[[:a]]]`, "{a: nil}\n"},
		{`h = {a: 1}; p Hash[h]`, "{a: 1}\n"},
		{`p Hash[]`, "{}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`Hash[:a]`, "odd number of arguments for Hash"},
		{`Hash[[5]]`, "wrong element type Integer at 0 (expected array)"},
		{`Hash[[[1, 2, 3]]]`, "invalid number of elements (3 for 1..2)"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
