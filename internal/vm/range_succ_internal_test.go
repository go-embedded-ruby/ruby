// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestRangeSucc covers iterating a Range whose begin is neither Integer nor
// String — a Symbol, a Date, or any object with #succ and #<=> — through the
// materialising methods (#to_a, #each, #map, #first, #min, #max) and the endless
// form, plus the exclusive boundary and the TypeError when begin has no #succ.
// Verified against ruby 4.0.6.
func TestRangeSucc(t *testing.T) {
	const s = `class S; include Comparable; attr_reader :n; def initialize(n); @n = n; end; def succ; S.new(@n + 1); end; def <=>(o); @n <=> o.n; end; def to_s; "S#{@n}"; end; end; `
	cases := []struct{ src, want string }{
		// Symbol ranges materialise via #succ.
		{`p (:a..:e).to_a`, `[:a, :b, :c, :d, :e]`},
		{`p (:a...:d).to_a`, `[:a, :b, :c]`},
		{`p (:a..:c).map(&:to_s)`, `["a", "b", "c"]`},
		{`p (:a..:e).first(3)`, `[:a, :b, :c]`},
		{`p (:a..:e).min`, `:a`},
		{`p (:a..:e).max`, `:e`},
		{`a = []; (:a..:c).each { |x| a << x }; p a`, `[:a, :b, :c]`},
		// A descending range yields nothing.
		{`p (:z..:a).to_a`, `[]`},
		// A custom Comparable/#succ object works.
		{s + `p (S.new(1)..S.new(3)).map(&:to_s)`, `["S1", "S2", "S3"]`},
		{s + `p (S.new(1)...S.new(3)).map(&:to_s)`, `["S1", "S2"]`},
		// An endless Symbol range is walked lazily until the block breaks.
		{`a = []; (:a..).each { |x| break if x > :c; a << x }; p a`, `[:a, :b, :c]`},
		// Integer ranges are unaffected (fast path).
		{`p (1..5).to_a`, `[1, 2, 3, 4, 5]`},
		// A single-byte String (or Symbol) range walks by byte value, so it crosses
		// the punctuation between 'Z' and 'a' rather than stopping at 'Z' (#succ).
		{`p ("a".."d").to_a`, `["a", "b", "c", "d"]`},
		{`p ("A".."z").to_a.size`, `58`},
		{`p (:A..:z).to_a.size`, `58`},
		{`p ("A"..."C").to_a`, `["A", "B"]`},
		// A multi-character String range still walks by #succ with the length guard.
		{`p ("az".."bb").to_a`, `["az", "ba", "bb"]`},
		// A begin with no successor cannot be iterated.
		{`begin; (1.0..3.0).to_a; rescue TypeError => e; p e.class; end`, `TypeError`},
		// Materialising an endless Symbol range raises RangeError, as any endless.
		{`begin; (:a..).to_a; rescue RangeError => e; p e.class; end`, `RangeError`},
		// Iteration stops if #<=> stops ordering the walked value against end.
		{s + `class Q; include Comparable; def initialize(n); @n = n; end; def succ; Q.new(@n + 1); end; def <=>(o); @n < 2 ? -1 : nil; end; def to_s; "Q#{@n}"; end; end; p (Q.new(0)..Q.new(9)).map(&:to_s)`, `["Q0", "Q1"]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
