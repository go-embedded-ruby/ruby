// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// ⛔ Ruby 4.0 replaced the ALGORITHM behind Set#divide with a 2-arity block, not
// just the pattern of yields. rbgo ran Tarjan's strongly-connected components
// over the full n x n relation — what MRI's lib/set.rb did up to 3.x.
// set_divide_arity2 (set.c:923 at v4.0.9) is union-find over each UNORDERED pair
// once, merging i and j only when the block is truthy BOTH ways.
//
// ruby/spec sees only the yields and asserts them both ways five lines apart
// (core/set/divide_spec.rb:35 from 4.0, :43 below it). It cannot see the other
// three divergences this file pins; every row was measured on MRI 4.0.7.
func TestSetDivideWithATwoArityBlockIsUnionFindOverMutualPairs(t *testing.T) {
	const pre = "require \"set\"\n"
	for _, tc := range []struct{ name, src, want string }{
		// --- the yields, which is all the spec example checks ---
		{"each unordered pair once, both orders", pre + `ret = []
Set[1, 2, 3].divide { |x, y| ret << [x, y] }
p ret`, `[[1, 2], [2, 1], [1, 3], [3, 1], [2, 3], [3, 2]]`},
		{"no self-pair is ever yielded", pre + `ret = []
Set[1, 2].divide { |x, y| ret << [x, y] }
p ret.sort`, `[[1, 2], [2, 1]]`},

		// ⛔ The && SHORT-CIRCUITS (set.c:940). A falsy first answer means the
		// reversed pair is never yielded. The spec cannot see this: its block
		// body is `ret << [x, y]`, which returns the Array and is truthy.
		{"a falsy block yields once per pair, not twice", pre + `ret = []
Set[1, 2, 3].divide { |x, y| ret << [x, y]; false }
p ret`, `[[1, 2], [1, 3], [2, 3]]`},

		// ⛔ A one-element Set has no pairs at all, so the block is never called.
		// rbgo called it once, with the element against itself.
		{"a single element yields nothing", pre + `n = 0
Set[1].divide { |x, y| n += 1; true }
p n`, `0`},
		{"an empty Set yields nothing", pre + `n = 0
Set[].divide { |x, y| n += 1; true }
p n`, `0`},

		// ⛔⛔ The RESULT changed too, and no spec covers it. A directed cycle
		// 1->2->3->1 with no reverse edge is ONE strongly-connected component and
		// has no mutually-related pair at all. SCC answers [[1, 2, 3]];
		// union-find answers three singletons, and 4.0.7 answers the latter.
		{"a directed cycle does not merge", pre + `f = ->(x, y) { [[1, 2], [2, 3], [3, 1]].include?([x, y]) }
p Set[1, 2, 3].divide(&f).map { |s| s.to_a.sort }.sort`, `[[1], [2], [3]]`},
		{"one-way truthiness does not merge", pre + `p Set[1, 2].divide { |x, y| x < y }.map { |s| s.to_a.sort }.sort`,
			`[[1], [2]]`},
		{"mutual truthiness merges", pre + `p Set[1, 2].divide { |x, y| true }.map { |s| s.to_a.sort }.sort`,
			`[[1, 2]]`},
		// The union is transitive even though 1 and 3 are unrelated.
		{"the union is transitive", pre + `f = ->(x, y) { [[1, 2], [2, 1], [2, 3], [3, 2]].include?([x, y]) }
p Set[1, 2, 3].divide(&f).map { |s| s.to_a.sort }.sort`, `[[1, 2, 3]]`},
		{"a mutual pair keeps an unrelated element out", pre + `f = ->(x, y) { [[1, 2], [2, 1], [2, 3]].include?([x, y]) }
p Set[1, 2, 3].divide(&f).map { |s| s.to_a.sort }.sort`, `[[1, 2], [3]]`},

		// The example from the spec and from MRI's own docs, which must not move.
		{"the adjacency division", pre + `p Set[1, 3, 4, 6, 9, 10, 11].divide { |x, y| (x - y).abs == 1 }.map { |s| s.to_a.sort }.sort`,
			`[[1], [3, 4], [6], [9, 10, 11]]`},

		// --- controls: the arity-1 path is a different function entirely ---
		{"arity 1 groups by block value", pre + `p Set[1, 2, 3, 4].divide { |x| x % 2 }.map { |s| s.to_a.sort }.sort`,
			`[[1, 3], [2, 4]]`},
		{"arity 1 yields once per element", pre + `n = 0
Set[1, 2, 3, 4].divide { |x| n += 1; x % 2 }
p n`, `4`},
		// Arity > 2 and -1 take the arity-1 path: only the first element arrives.
		{"arity 3 takes the one-argument path", pre + `seen = []
Set["one", "two", "three"].divide { |x, y, z| seen << [x, y, z]; x.length }
p seen.sort`, `[["one", nil, nil], ["three", nil, nil], ["two", nil, nil]]`},
		{"arity -1 receives one element", pre + `sizes = []
Set["one", "two", "three"].divide { |*xs| sizes << xs.size; xs.first.length }
p sizes`, `[1, 1, 1]`},

		// The enumerator form, and the outer Set's class on BOTH paths —
		// set_i_divide builds it with rb_cSet (set.c:1002), as does
		// set_divide_arity2 (set.c:946).
		{"no block gives an Enumerator", pre + `p Set[1, 2, 3, 4].divide.class`, `Enumerator`},
		{"the enumerator divides", pre + `p Set[1, 2, 3, 4].divide.each { |a, b| (a + b).even? }.map { |s| s.to_a.sort }.sort`,
			`[[1, 3], [2, 4]]`},
		{"the outer Set is a plain Set, arity 2", pre + `p Set[1, 2].divide { |x, y| false }.class`, `Set`},
		{"the outer Set is a plain Set, arity 1", pre + `p Set[1, 2].divide { |x| x }.class`, `Set`},

		// compare_by_identity is not retained by the outer Set
		// (core/set/divide_spec.rb:21).
		{"the outer Set drops compare_by_identity", pre + `s = Set.new.compare_by_identity
s << "a" << "b"
p s.divide { |x, y| false }.compare_by_identity?`, `false`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}
