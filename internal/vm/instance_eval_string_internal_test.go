// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestInstanceEvalString covers the String form of BasicObject#instance_eval:
// evaluating source with the receiver as self (reading its instance variables,
// defining a singleton method), on an immediate receiver, the optional filename
// argument, the arity errors, a block-plus-arguments error, and a SyntaxError
// for bad source. Verified against ruby 4.0.6.
func TestInstanceEvalString(t *testing.T) {
	cases := []struct{ src, want string }{
		// self and instance variables resolve to the receiver.
		{`o = Object.new; o.instance_variable_set(:@x, 42); p o.instance_eval("@x")`, `42`},
		{`o = Object.new; p o.instance_eval("self").equal?(o)`, `true`},
		// A def in the source becomes a singleton method of the receiver.
		{`o = Object.new; o.instance_eval("def greet; :hi; end"); p o.greet`, `:hi`},
		// An immediate receiver can still be read through.
		{`p 5.instance_eval("self + 1")`, `6`},
		// The block form still works, and returns the block's value.
		{`o = Object.new; o.instance_variable_set(:@v, 7); p o.instance_eval { @v }`, `7`},
		// Arity: no block and no args, or too many args.
		{`begin; Object.new.instance_eval; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; Object.new.instance_eval("x", "f", 1, 2); rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		// A block together with arguments is an ArgumentError.
		{`begin; Object.new.instance_eval("x") {}; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		// The two-argument (source, filename) form is accepted.
		{`p Object.new.instance_eval("1 + 1", "myfile.rb")`, `2`},
		// Bad source raises SyntaxError.
		{`begin; Object.new.instance_eval("def ("); rescue SyntaxError => e; p e.class; end`, `SyntaxError`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
