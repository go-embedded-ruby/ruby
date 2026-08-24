// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestPreludeInternalHelpersPrivate checks that the prelude's internal helper
// methods (Comparable#__compare and Enumerable's __each_packed/__pack/
// __enum_int_arg) are private, so they do not leak into the modules' public
// instance_methods as MRI's do not — while still working through the operators
// and iterators that call them. Verified against ruby 4.0.6.
func TestPreludeInternalHelpersPrivate(t *testing.T) {
	cases := []struct{ src, want string }{
		// Public instance method lists match MRI (no __-prefixed helpers).
		{`p Comparable.instance_methods(false).sort`, `[:<, :<=, :==, :>, :>=, :between?, :clamp]`},
		{`p Enumerable.instance_methods(false).grep(/^__/)`, `[]`},
		// The helpers are private: not answered by respond_to?, no explicit-receiver call.
		{`p 5.respond_to?(:__compare)`, `false`},
		{`begin; 5.__compare(3); rescue NoMethodError => e; p e.class; end`, `NoMethodError`},
		// ...but still reachable through send (private) and the operators that use them.
		{`p 5.send(:__compare, 3)`, `1`},
		{`p 5 < 10`, `true`},
		{`p 3.clamp(1, 2)`, `2`},
		// Enumerable iteration still works (it goes through the now-private helpers).
		{`p (1..3).to_a`, `[1, 2, 3]`},
		{`p [3, 1, 2].sort`, `[1, 2, 3]`},
		{`p [1, 2, 3, 4].first(2)`, `[1, 2]`},
		{`p [1, 2, 3].map { |x| x * 2 }`, `[2, 4, 6]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
