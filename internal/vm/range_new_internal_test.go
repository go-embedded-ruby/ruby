// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestRangeNew covers Range.new: an inclusive or exclusive range (the third
// argument is exclusive when truthy, not only when literally true), string
// endpoints, beginless and endless ranges, the ArgumentError for endpoints that
// #<=> reports as incomparable, an exception from #<=> propagating, the arity
// errors, and a subclass instance wrapping the range. Verified against ruby
// 4.0.6.
func TestRangeNew(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Range.new(1, 10).to_a`, `[1, 2, 3, 4, 5, 6, 7, 8, 9, 10]`},
		{`p Range.new(1, 10, true).to_a`, `[1, 2, 3, 4, 5, 6, 7, 8, 9]`},
		{`p Range.new(1, 10, false).to_a`, `[1, 2, 3, 4, 5, 6, 7, 8, 9, 10]`},
		{`p Range.new("a", "c").to_a`, `["a", "b", "c"]`},
		{`p Range.new(1, 5).sum`, `15`},
		{`p Range.new(1, 5).exclude_end?`, `false`},
		{`p Range.new(1, 5, true).exclude_end?`, `true`},
		// Any truthy third argument makes the range exclusive.
		{`p Range.new(1, 3, 1).to_a`, `[1, 2]`},
		{`p Range.new(1, 3, :x).to_a`, `[1, 2]`},
		{`p Range.new(1, 3, nil).to_a`, `[1, 2, 3]`},
		// Beginless / endless ranges skip the comparison.
		{`p Range.new(nil, 5).begin`, `nil`},
		{`p Range.new(1, nil).end`, `nil`},
		// Incomparable endpoints (#<=> gives nil) raise ArgumentError.
		{`begin; Range.new(1, Object.new); rescue ArgumentError => e; p e.message; end`, `"bad value for range"`},
		// An exception raised inside #<=> propagates (is not rescued).
		{`class Boom; def <=>(o); raise "boom"; end; end
begin; Range.new(Boom.new, Boom.new); rescue RuntimeError => e; p e.message; end`, `"boom"`},
		// Arity errors.
		{`begin; Range.new(1); rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; Range.new(1, 2, 3, 4); rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		// A subclass instance wraps the range and still works.
		{`class MyRange < Range; end; p MyRange.new(1, 3).to_a`, `[1, 2, 3]`},
		{`class MyRange2 < Range; end; p MyRange2.new(1, 3).class.superclass`, `Range`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
