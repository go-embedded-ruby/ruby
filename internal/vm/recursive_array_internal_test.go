// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestAnArrayThatContainsItself covers Array#<=>, Array#hash and Array#flatten
// on a self-referential array.
//
//	a = []
//	a << a
//
// #inspect and #== already remembered where they had been; these three did not,
// and each cost about 1.1 GB and never finished. core/array/comparison_spec.rb,
// hash_spec.rb and flatten_spec.rb all build one through
// ArraySpecs.empty_recursive_array, which is why all three read 1.08 GB.
//
// Verified against ruby 3, including that flatten *raises* rather than
// answering.
func TestAnArrayThatContainsItself(t *testing.T) {
	cases := []struct{ src, want string }{
		// Comparing a pair already being compared is equal at that point.
		{`a = []; a << a; p(a <=> a)`, `0`},
		{`a = []; a << a; b = []; b << b; p(a <=> b)`, `0`},
		{`c = [1, [2]]; c << c; p(c <=> c)`, `0`},
		// The spec's own three, which need the shorter/longer rules to survive
		// the guard.
		{`e = []; e << e; p(e <=> [])`, `1`},
		{`e = []; e << e; p([] <=> e)`, `-1`},
		// Hashing ends, and is still a hash: a repeat contributes a constant
		// rather than being dropped, so these two differ.
		{`a = []; a << a; p a.hash.class`, `Integer`},
		{`a = []; a << a; b = [a]; c = [a, a]; p(b.hash == c.hash)`, `false`},
		// Flattening a ring has no answer, and MRI says so.
		{`a = []; a << a
begin; a.flatten; rescue ArgumentError => e; p e.message; end`,
			`"tried to flatten recursive array"`},
		{`a = []; a << a
begin; a.flatten!; rescue ArgumentError => e; p e.message; end`,
			`"tried to flatten recursive array"`},
		// An array that merely repeats a sibling is not a ring, and still
		// flattens.
		{`x = [1]; p [x, x].flatten`, `[1, 1]`},
		// None of the ordinary behaviour moves.
		{`p([1, [2, [3]]].flatten)`, `[1, 2, 3]`},
		{`p([1, [2, [3]]].flatten(1))`, `[1, 2, [3]]`},
		{`p([1, 2] <=> [1, 3])`, `-1`},
		{`p([1, 2] <=> [1, 2])`, `0`},
		{`p([1, 2] <=> [1])`, `1`},
		{`p([1, 2] <=> "x")`, `nil`},
		{`p([[1]].hash == [[1]].hash)`, `true`},
		{`p([1].hash == [2].hash)`, `false`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
