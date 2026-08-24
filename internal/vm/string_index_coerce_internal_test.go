// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringIndexCoerce covers String#[] applying MRI's implicit conversions to
// its index/length/range arguments: a Float index or length truncates toward
// zero, a #to_int object converts, and a Range argument may be a Range subclass
// or carry Float/#to_int bounds. A lone String argument (the substring form) and
// the plain Integer/Regexp forms are unaffected. The 2-arg (index, length) form
// with a non-convertible argument raises TypeError, as does a Range passed where
// an index is expected. Symbol#[] inherits every case by delegation. Verified
// against ruby 4.0.6.
func TestStringIndexCoerce(t *testing.T) {
	cases := []struct{ src, want string }{
		// Float index / length truncate toward zero.
		{`p "hello"[1.5]`, `"e"`},
		{`p "hello"[-1.9]`, `"o"`},
		{`p "hello"[1.5, 2.9]`, `"el"`},
		{`p "hello"[1.0, 3.0]`, `"ell"`},
		// #to_int object as index / length.
		{`class I; def to_int; 2; end; end; p "hello"[I.new]`, `"l"`},
		{`class I; def to_int; 1; end; end; p "hello"[I.new, I.new]`, `"e"`},
		// Range with Float bounds.
		{`p "hello"[1.0..3.0]`, `"ell"`},
		{`p "hello"[1.5..3.9]`, `"ell"`},
		{`p "hello"[(1.5)..]`, `"ello"`},
		{`p "hello"[..2.9]`, `"hel"`},
		{`p "hello"[1.0...3.0]`, `"el"`},
		// Range subclass argument (an RObject wrapping *object.Range).
		{`class R < Range; end; p "hello"[R.new(1, 3)]`, `"ell"`},
		{`class R < Range; end; p "hello"[R.new(1, 3, true)]`, `"el"`},
		// Plain forms are unchanged.
		{`p "hello"[1]`, `"e"`},
		{`p "hello"[1, 3]`, `"ell"`},
		{`p "hello"[1..3]`, `"ell"`},
		{`p "hello"["ell"]`, `"ell"`},
		{`p "hello"["xyz"]`, `nil`},
		{`p "hello"[/l+/]`, `"ll"`},
		// Non-convertible arguments raise TypeError.
		{`begin; "hello"[Object.new]; rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; "hello"["e", 2]; rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; "hello"[(1..2), 3]; rescue TypeError => e; p e.class; end`, `TypeError`},
		// Wrong arity raises ArgumentError, for every form (1..2 args).
		{`begin; "hello"[1, 2, 3]; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; "hello"[/l/, 0, 9]; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		// Symbol#[] inherits the coercion by delegation.
		{`p :hello[1.5]`, `"e"`},
		{`p :hello[1.0..3.0]`, `"ell"`},
		{`class I; def to_int; 2; end; end; p :hello[I.new]`, `"l"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
