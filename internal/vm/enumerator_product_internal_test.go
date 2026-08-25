// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestEnumeratorProduct covers Enumerator.product: the Cartesian product of its
// enumerable arguments (any arity, including none), an Enumerator::Product
// instance, laziness over an infinite source, a #size that is the product of the
// sources' sizes (Float::INFINITY when any is infinite), the block form returning
// nil, keyword-argument rejection, use of #each_entry (drained once), and the
// NoMethodError when an argument lacks #each_entry. Verified against ruby 4.0.6.
func TestEnumeratorProduct(t *testing.T) {
	const inf = `e = Enumerator.new { |y| i = 0; loop { y << (i += 1) } }; `
	const oneShot = `a = [1, 2, 3]; i = 0; en = Enumerator.new { |y| while i < a.size; y << a[i]; i += 1; end }; `
	const obj = `o = Object.new; def o.each_entry; yield 1; yield 2; end; `
	cases := []struct{ src, want string }{
		// Cartesian product of any arity.
		{`p Enumerator.product(1..2, ["A", "B"]).to_a`, `[[1, "A"], [1, "B"], [2, "A"], [2, "B"]]`},
		{`p Enumerator.product(1..2).to_a`, `[[1], [2]]`},
		{`p Enumerator.product(2..3, ["A"], ["B"], ["C"]).to_a`, `[[2, "A", "B", "C"], [3, "A", "B", "C"]]`},
		// No arguments yields one empty array.
		{`p Enumerator.product.to_a`, `[[]]`},
		// The result is an Enumerator::Product.
		{`p Enumerator.product.class`, `Enumerator::Product`},
		{`p Enumerator.product(1..2).is_a?(Enumerator)`, `true`},
		// Laziness: an infinite source yields an infinite product.
		{inf + `p Enumerator.product(e, ["A", "B"]).take(5)`, `[[1, "A"], [1, "B"], [2, "A"], [2, "B"], [3, "A"]]`},
		// #size is the product of the sizes; infinite when a source is infinite.
		{`p Enumerator.product(1..2, ["A", "B"]).size`, `4`},
		{`p Enumerator.product.size`, `1`},
		{inf + `p Enumerator.product(e, ["A", "B"]).size`, `nil`},
		// #size on an infinite source is Float::INFINITY (computed, not iterated).
		{`p Enumerator.product(1.., ["A", "B"]).size`, `Infinity`},
		// A block iterates the product and returns nil.
		{`elems = []; Enumerator.product(1..2, ["X", "Y"]) { |c| elems << c }; p elems`, `[[1, "X"], [1, "Y"], [2, "X"], [2, "Y"]]`},
		{`r = Enumerator.product(1..2) {}; p r`, `nil`},
		// Keyword arguments are rejected.
		{`begin; Enumerator.product(1..3, foo: 1, bar: 2); rescue ArgumentError => ex; p ex.message; end`, `"unknown keywords: :foo, :bar"`},
		// Sources are consumed through #each_entry, each drained only once.
		{obj + `p Enumerator.product(o, ["A"]).to_a`, `[[1, "A"], [2, "A"]]`},
		{oneShot + `p Enumerator.product(["a", "b"], en).to_a`, `[["a", 1], ["a", 2], ["a", 3]]`},
		// A multi-value each_entry yield keeps its first value; a bare yield gives nil.
		{`o = Object.new; def o.each_entry; yield 1, 2; yield 3; end; p Enumerator.product(o, ["A"]).to_a`, `[[1, "A"], [3, "A"]]`},
		{`o = Object.new; def o.each_entry; yield; yield 5; end; p Enumerator.product(o, ["A"]).to_a`, `[[nil, "A"], [5, "A"]]`},
		// An argument without #each_entry raises NoMethodError.
		{`begin; Enumerator.product(Object.new).to_a; rescue NoMethodError => ex; p ex.class; end`, `NoMethodError`},
		// #each_entry is called lazily — building the enumerator does not raise.
		{`p Enumerator.product(Object.new).is_a?(Enumerator)`, `true`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
