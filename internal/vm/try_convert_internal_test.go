// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestTryConvert covers Array/Hash/String/Integer.try_convert: an argument that
// is already a kind of the target (including a subclass) is returned by
// identity; an argument responding to the implicit conversion method
// (#to_ary/#to_hash/#to_str/#to_int) is converted, with a nil result passed
// through and a wrong-typed result raising TypeError; an argument that does not
// respond returns nil. Verified against ruby 4.0.6.
func TestTryConvert(t *testing.T) {
	cases := []struct{ src, want string }{
		// Already the target type: returned unchanged (by identity).
		{`x = [1, 2]; p Array.try_convert(x).equal?(x)`, `true`},
		{`x = {a: 1}; p Hash.try_convert(x).equal?(x)`, `true`},
		{`x = "s"; p String.try_convert(x).equal?(x)`, `true`},
		{`p Integer.try_convert(42)`, `42`},
		// A subclass instance is also a kind of the target.
		{`class MyArr < Array; end; x = MyArr.new; p Array.try_convert(x).equal?(x)`, `true`},
		// Converts via the implicit conversion method.
		{`class AC; def to_ary; [1, 2]; end; end; p Array.try_convert(AC.new)`, `[1, 2]`},
		{`class HC; def to_hash; {a: 1}; end; end; p Hash.try_convert(HC.new)`, `{a: 1}`},
		{`class SC; def to_str; "z"; end; end; p String.try_convert(SC.new)`, `"z"`},
		{`class IC; def to_int; 7; end; end; p Integer.try_convert(IC.new)`, `7`},
		// A conversion method returning nil is passed through.
		{`class NA; def to_ary; nil; end; end; p Array.try_convert(NA.new)`, `nil`},
		// An argument not responding to the conversion method returns nil.
		{`p Array.try_convert(Object.new)`, `nil`},
		{`p Hash.try_convert(1)`, `nil`},
		{`p String.try_convert(1)`, `nil`},
		{`p Integer.try_convert("3")`, `nil`},
		// A wrong-typed conversion result raises TypeError with the MRI message.
		{`class Bad; def to_ary; Object.new; end; end
begin; Array.try_convert(Bad.new); rescue TypeError => e; p e.message; end`,
			`"can't convert Bad into Array (Bad#to_ary gives Object)"`},
		{`class BadI; def to_int; "s"; end; end
begin; Integer.try_convert(BadI.new); rescue TypeError => e; p e.message; end`,
			`"can't convert BadI into Integer (BadI#to_int gives String)"`},
		// Regexp.try_convert (via #to_regexp) and IO.try_convert (via #to_io).
		{`p Regexp.try_convert(/x/)`, `/x/`},
		{`p Regexp.try_convert("x")`, `nil`},
		{`class RC; def to_regexp; /custom/; end; end; p Regexp.try_convert(RC.new)`, `/custom/`},
		{`p IO.try_convert($stdout).equal?($stdout)`, `true`},
		{`p IO.try_convert("x")`, `nil`},
		{`class Bad2; def to_regexp; 42; end; end
begin; Regexp.try_convert(Bad2.new); rescue TypeError => e; p e.message; end`,
			`"can't convert Bad2 into Regexp (Bad2#to_regexp gives Integer)"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
