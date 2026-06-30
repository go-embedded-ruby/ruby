// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestOstructData covers the DATA surface of OpenStruct now backed by
// github.com/go-ruby-ostruct/ostruct through the native __data_* helpers
// (to_h / inspect / to_s / dig / delete_field / == / eql?). Every expectation is
// asserted against MRI Ruby 4.0.5's ostruct standard library. The dynamic
// accessor glue (method_missing readers/writers, respond_to_missing?, [], []=,
// each_pair) stays in the prelude and is exercised here too, since the data
// methods read the table it maintains.
func TestOstructData(t *testing.T) {
	cases := []struct{ src, want string }{
		// to_h: insertion order, Symbol keys, seeded + assigned + bracket-set.
		{`p OpenStruct.new(a: 1, "b" => 2).to_h`, "{a: 1, b: 2}\n"},
		{`o = OpenStruct.new(a: 1); o.c = 3; o[:b] = 2; p o.to_h`, "{a: 1, c: 3, b: 2}\n"},
		{`p OpenStruct.new.to_h`, "{}\n"},
		// to_h returns a fresh Hash (mutating it does not touch the struct).
		{`o = OpenStruct.new(a: 1); h = o.to_h; h[:z] = 9; p o.to_h`, "{a: 1}\n"},

		// inspect / to_s: MRI "#<OpenStruct k=v, ...>" form, values via #inspect.
		{`p OpenStruct.new.inspect`, "\"#<OpenStruct>\"\n"},
		{`p OpenStruct.new(a: 1).inspect`, "\"#<OpenStruct a=1>\"\n"},
		{`p OpenStruct.new(a: 1, b: "x").inspect`, "\"#<OpenStruct a=1, b=\\\"x\\\">\"\n"},
		{`p OpenStruct.new(s: "hi\nthere").inspect`, "\"#<OpenStruct s=\\\"hi\\\\nthere\\\">\"\n"},
		{`p OpenStruct.new(inner: OpenStruct.new(x: 1)).inspect`, "\"#<OpenStruct inner=#<OpenStruct x=1>>\"\n"},
		{`p OpenStruct.new(a: 1).to_s`, "\"#<OpenStruct a=1>\"\n"},
		// a value's user-defined #inspect is honoured.
		{`class K; def inspect; "K!"; end; end; p OpenStruct.new(k: K.new).inspect`, "\"#<OpenStruct k=K!>\"\n"},

		// dig: single key, missing key (nil), nested through Hash/Array, nested
		// through a sub-OpenStruct.
		{`p OpenStruct.new(a: 1).dig(:a)`, "1\n"},
		{`p OpenStruct.new(a: 1).dig(:z)`, "nil\n"},
		{`p OpenStruct.new(h: {k: [1, 2]}).dig(:h, :k, 0)`, "1\n"},
		{`p OpenStruct.new(a: OpenStruct.new(b: {c: 5})).dig(:a, :b, :c)`, "5\n"},
		// a missing intermediate value short-circuits to nil.
		{`p OpenStruct.new(a: nil).dig(:a, :b)`, "nil\n"},
		// a String key is interned to the matching Symbol field.
		{`p OpenStruct.new(a: 1).dig("a")`, "1\n"},

		// delete_field: returns the prior value and removes it in order.
		{`o = OpenStruct.new(a: 1, b: 2); p o.delete_field(:a); p o.to_h`, "1\n{b: 2}\n"},
		{`o = OpenStruct.new(a: 1, b: 2); o.delete_field(:a); o.c = 3; p o.to_h`, "{b: 2, c: 3}\n"},

		// == / eql?: same fields+values (incl. subclass), reference-typed values
		// compared by content, differing fields, non-OpenStruct.
		{`p(OpenStruct.new(a: 1) == OpenStruct.new(a: 1))`, "true\n"},
		{`p(OpenStruct.new(a: 1).eql?(OpenStruct.new(a: 1)))`, "true\n"},
		{`p(OpenStruct.new(a: "x") == OpenStruct.new(a: "x"))`, "true\n"},
		{`p(OpenStruct.new(a: [1, 2]) == OpenStruct.new(a: [1, 2]))`, "true\n"},
		{`p(OpenStruct.new(a: OpenStruct.new(b: 1)) == OpenStruct.new(a: OpenStruct.new(b: 1)))`, "true\n"},
		{`class Sub < OpenStruct; end; p(OpenStruct.new(a: 1) == Sub.new(a: 1))`, "true\n"},
		{`p(OpenStruct.new(a: 1) == OpenStruct.new(a: 2))`, "false\n"},
		{`p(OpenStruct.new(a: 1) == OpenStruct.new(b: 1))`, "false\n"},
		{`p(OpenStruct.new(a: 1, b: 2) == OpenStruct.new(a: 1))`, "false\n"},
		{`p(OpenStruct.new(a: 1) == 5)`, "false\n"},
	}
	for _, c := range cases {
		src := `require "ostruct"; ` + c.src
		if got := eval(t, src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOstructErrors covers the error branches of the native helpers, each
// surfacing the library's MRI-exact message: delete_field on an absent field
// (NameError), dig through a value without #dig (TypeError), and dig with no
// keys (ArgumentError).
func TestOstructErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`OpenStruct.new(b: 2, c: 3).delete_field(:nope)`,
			"[NameError, \"no field 'nope' in #<OpenStruct b=2, c=3>\"]\n"},
		{`OpenStruct.new(a: 1).dig(:a, :b)`,
			"[TypeError, \"Integer does not have #dig method\"]\n"},
		{`OpenStruct.new(a: 1).dig`,
			"[ArgumentError, \"wrong number of arguments (given 0, expected 1+)\"]\n"},
	}
	for _, c := range cases {
		src := `require "ostruct"; begin; ` + c.src + `; rescue => e; p [e.class, e.message]; end`
		if got := eval(t, src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
