// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestEvalCallerBinding covers that a bare eval(str) — with no explicit binding
// — evaluates against the caller's binding: it reads and assigns the caller's
// local variables, works inside a method with method-local variables, and stays
// transparent to __method__ / __callee__. An explicit binding argument still
// works. Verified against ruby 4.0.6.
func TestEvalCallerBinding(t *testing.T) {
	cases := []struct{ src, want string }{
		// Reads a caller local.
		{`x = 42; p eval("x")`, `42`},
		{`a = 10; b = 20; p eval("a + b")`, `30`},
		// Assigns back into the caller's scope.
		{`x = 5; eval("x = 99"); p x`, `99`},
		// Works with a method-local variable.
		{`def m; y = 7; eval("y * 2"); end; p m`, `14`},
		// Transparent to __method__ / __callee__.
		{`def foo; eval("__method__"); end; p foo`, `:foo`},
		{`def bar; eval("__callee__"); end; p bar`, `:bar`},
		{`p eval("__method__")`, `nil`},
		// A plain expression still evaluates.
		{`p eval("1 + 1")`, `2`},
		// An explicit binding argument is still honoured.
		{`x = 3; p eval("x * x", binding)`, `9`},
		// An explicit-receiver eval (Kernel.eval) runs against a fresh scope with no
		// caller binding, exercising the plain compile-and-run path.
		{`p Kernel.eval("3 + 4")`, `7`},
		{`begin; Kernel.eval(5); rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; Kernel.eval("def ("); rescue SyntaxError => e; p e.class; end`, `SyntaxError`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
