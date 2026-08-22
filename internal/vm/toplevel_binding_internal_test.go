// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestTopLevelBinding covers the TOPLEVEL_BINDING constant: it is a Binding
// whose receiver is main and whose definee is Object, so evaluating code through
// it defines methods and constants at the top level and reports main as self.
// Verified against ruby 4.0.6.
func TestTopLevelBinding(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p TOPLEVEL_BINDING.class`, `Binding`},
		{`p TOPLEVEL_BINDING.eval("self")`, `main`},
		{`p TOPLEVEL_BINDING.receiver`, `main`},
		{`p TOPLEVEL_BINDING.eval("2 * 21")`, `42`},
		// A def evaluated through it becomes a top-level (Object) method.
		{`TOPLEVEL_BINDING.eval("def tlb_defined; 7; end"); p tlb_defined`, `7`},
		// A constant assigned through it is a top-level constant.
		{`TOPLEVEL_BINDING.eval("TLB_CONST = 99"); p TLB_CONST`, `99`},
		// It starts with no locals of its own.
		{`p TOPLEVEL_BINDING.local_variables`, `[]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
