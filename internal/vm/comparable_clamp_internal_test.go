// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestComparableClamp covers Comparable#clamp comparing through <=> (so bounds or
// self that define only <=> — not the Comparable </> operators — still clamp),
// an endless exclusive range being allowed while a finite exclusive one is
// rejected, the min>max and nil-comparison ArgumentErrors, and the ordinary
// two-argument / Range forms. Verified against ruby 4.0.6.
func TestComparableClamp(t *testing.T) {
	// OC defines only <=> and does NOT include Comparable (so it has no `<`/`>`),
	// used as clamp bounds. W includes Comparable (so it has #clamp) with the same
	// <=>. MRI clamps a W against OC bounds purely via <=>.
	onlyCmp := `class OC; attr_reader :v; def initialize(v); @v = v; end; def <=>(o); o.nil? ? nil : v <=> o.v; end; end; ` +
		`class W < OC; include Comparable; end; `
	cases := []struct{ src, want string }{
		// Ordinary two-argument and Range forms.
		{`p 5.clamp(1, 10)`, "5"},
		{`p 0.clamp(1, 10)`, "1"},
		{`p 20.clamp(1, 10)`, "10"},
		{`p 5.clamp(1..10)`, "5"},
		// A nil bound leaves that side unbounded.
		{`p 5.clamp(8, nil)`, "8"},
		{`p 5.clamp(nil, 3)`, "3"},
		// Bounds that only define <=> still clamp (via <=>, not `<`/`>`).
		{onlyCmp + `p W.new(5).clamp(OC.new(1), OC.new(10)).v`, "5"},
		{onlyCmp + `p W.new(0).clamp(OC.new(1), OC.new(10)).v`, "1"},
		{onlyCmp + `p W.new(9).clamp(OC.new(1), OC.new(3)).v`, "3"},
		// An endless exclusive range is allowed; a finite exclusive one is not.
		{`p 5.clamp(1...)`, "5"},
		{`p (-3).clamp(1...)`, "1"},
		{`p (5.clamp(1...10)) rescue p $!.class`, "ArgumentError"},
		// min > max raises ArgumentError (compared through <=>).
		{`p (5.clamp(10, 1)) rescue p $!.class`, "ArgumentError"},
		// A nil comparison (incomparable operands) raises ArgumentError.
		{`p (5.clamp("a", "z")) rescue p $!.class`, "ArgumentError"},
		{`p (5.clamp(1, "z")) rescue p $!.class`, "ArgumentError"},
		// A single non-Range argument is a TypeError; too many is an ArgumentError.
		{`p (5.clamp(3)) rescue p $!.class`, "TypeError"},
		{`p (5.clamp(1, 2, 3)) rescue p $!.class`, "ArgumentError"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
