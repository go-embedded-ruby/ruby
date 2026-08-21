// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestNumericCoerce covers the generic Numeric#coerce a Numeric subclass
// inherits (Integer/Float override it): a same-class argument is returned
// paired unchanged, a different-class argument pairs both operands converted
// with Kernel#Float (via #to_f), a non-convertible argument raises the same
// error Float() would, and #coerce being inherited means undef_method :coerce
// now works on a Numeric subclass. Verified against ruby 4.0.6.
func TestNumericCoerce(t *testing.T) {
	const def = "class N < Numeric\n  def initialize(v); @v = v; end\n  def to_f; @v.to_f; end\nend\n"
	cases := []struct{ src, want string }{
		// A Numeric subclass inherits #coerce.
		{def + `p N.new(3).respond_to?(:coerce)`, `true`},
		// Different class: both converted to Float (via #to_f).
		{def + `p N.new(3).coerce(5)`, `[5.0, 3.0]`},
		{def + `p N.new(3).coerce(2.5)`, `[2.5, 3.0]`},
		// Same class: returned as the pair [other, self] unchanged.
		{def + `a = N.new(3); b = N.new(5); p a.coerce(b).map(&:to_f)`, `[5.0, 3.0]`},
		// A non-convertible argument raises what Float() would.
		{def + `begin; N.new(3).coerce("x"); rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{def + `begin; N.new(3).coerce(nil); rescue TypeError => e; p e.class; end`, `TypeError`},
		// Because #coerce is now inherited, undef_method :coerce succeeds.
		{`class M < Numeric; undef_method :coerce; end; p M.new.respond_to?(:coerce)`, `false`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
