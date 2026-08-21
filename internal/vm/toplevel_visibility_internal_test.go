// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestTopLevelVisibility covers the top-level private/public/protected methods:
// at the top level self is `main`, whose default definee is Object, so a
// name-argument form sets the visibility of the named Object methods and a bare
// form sets the default visibility of subsequent top-level defs. Verified
// against ruby 4.0.6.
func TestTopLevelVisibility(t *testing.T) {
	cases := []struct{ src, want string }{
		// private :name hides the method from a public (no-receiver-style) call.
		{`def a1; end; private :a1; p respond_to?(:a1)`, `false`},
		{`def a2; end; private :a2; p respond_to?(:a2, true)`, `true`},
		// public :name (re-)exposes it.
		{`def b1; end; private :b1; public :b1; p respond_to?(:b1)`, `true`},
		// A bare `private` sets the default visibility of following defs.
		{`private; def c1; end; p respond_to?(:c1)`, `false`},
		{`private; def d1; end; public; def d2; end; p [respond_to?(:d1), respond_to?(:d2)]`, `[false, true]`},
		// protected :name likewise hides it from a plain call.
		{`def e1; end; protected :e1; p respond_to?(:e1)`, `false`},
		{`def e2; end; protected :e2; p respond_to?(:e2, true)`, `true`},
		// The name-argument form returns its argument (a single symbol).
		{`def f1; end; p(private :f1)`, `:f1`},
		// An Array argument marks each and is returned.
		{`def g1; end; def g2; end; p(private [:g1, :g2])`, `[:g1, :g2]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
