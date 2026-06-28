// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestPuppetBatch covers the built-in/VM additions made while advancing the
// Puppet boot: Object#public_methods, Hash#each_key/#each_value, Module#
// undef_method/#remove_method, the generic Numeric Complex-compat and arithmetic
// methods, Integer#floor/#ceil, String#each_codepoint/#codepoints, Thread thread
// variables, Regexp#options/#casefold?, and interpolated regexp literals. Every
// case is asserted against MRI Ruby 4.0.5.
func TestPuppetBatch(t *testing.T) {
	cases := []struct{ src, want string }{
		// Object#public_methods: instance (public only), and module/class form
		// (the receiver's own singleton methods minus inherited public ones).
		{`class A; def pub; end; protected; def prot; end; private; def priv; end; end
		  m = A.new.public_methods(false); p [m.include?(:pub), m.include?(:prot), m.include?(:priv)]`, "[true, false, false]\n"},
		{`module M; def self.foo; end; end; p M.public_methods(false)`, "[:foo]\n"},
		{`module J; def self.load; end; def self.dump; end; end
		  p (J.public_methods - Module.public_methods).sort`, "[:dump, :load]\n"},
		{`o = Object.new; def o.sing; end; p o.public_methods(false).include?(:sing)`, "true\n"},

		// Hash#each_key / #each_value (block and enumerator forms).
		{`r = []; {a: 1, b: 2}.each_key { |k| r << k }; p r`, "[:a, :b]\n"},
		{`r = []; {a: 1, b: 2}.each_value { |v| r << v }; p r`, "[1, 2]\n"},
		{`p [{a: 1}.each_key.class, {a: 1}.each_value.class]`, "[Enumerator, Enumerator]\n"},

		// Module#undef_method / #remove_method.
		{`class U; def f; end; end; U.send(:undef_method, :f); p U.new.respond_to?(:f)`, "false\n"},
		{`class D; def x; 1; end; end; class D2 < D; def x; 2; end; end
		  D2.send(:remove_method, :x); p D2.new.x`, "1\n"},
		{`class R; def y; end; end; p R.send(:undef_method, :y) == R`, "true\n"},
		{`class R2; def y; end; end; p R2.send(:remove_method, :y) == R2`, "true\n"},

		// Generic Numeric methods (reached on a bare Numeric subclass / via send).
		{`p [(-5).phase, (-5.0).phase, 0.phase]`, "[3.141592653589793, 3.141592653589793, 0]\n"},
		{`p (-5).polar`, "[5, 3.141592653589793]\n"},
		{`p [5.rect, 5.rectangular]`, "[[5, 0], [5, 0]]\n"},
		{`p [3.conjugate, 3.conj, 3.imaginary, 3.imag]`, "[3, 3, 0, 0]\n"},
		{`p [3.arg, 3.angle]`, "[0, 0]\n"},
		{`p [5.send(:-@), 5.send(:+@)]`, "[-5, 5]\n"},
		{`p [7.div(2), 7.fdiv(2), 7.modulo(3), 7.send(:%, 3)]`, "[3, 3.5, 1, 1]\n"},
		{`p 7.divmod(3)`, "[2, 1]\n"},
		{`p [(-3).negative?, 3.positive?, 5.abs2]`, "[true, true, 25]\n"},
		{`class TN < Numeric; end; p TN.method_defined?(:-@)`, "true\n"},

		// Integer#floor / #ceil (no-arg and negative-digits).
		{`p [7.floor, 7.ceil, 7.floor(1), 7.ceil(2)]`, "[7, 7, 7, 7]\n"},
		{`p [73.floor(-1), 73.ceil(-1), (-73).floor(-1), (-73).ceil(-1), 0.floor(-1)]`, "[70, 80, -80, -70, 0]\n"},
		{`p [7.floor(-30), 7.ceil(-30)]`, "[0, 0]\n"}, // beyond int64: 0

		// String#each_codepoint / #codepoints.
		{`r = []; "abç".each_codepoint { |c| r << c }; p r`, "[97, 98, 231]\n"},
		{`p "abç".codepoints`, "[97, 98, 231]\n"},
		{`p "abç".each_codepoint.class`, "Enumerator\n"},

		// Thread thread-variables (distinct from Thread#[]).
		{`t = Thread.current; t.thread_variable_set(:x, 10); t.thread_variable_set("y", 20)
		  p [t.thread_variable_get(:x), t.thread_variable_get("y"), t.thread_variable_get(:z)]`, "[10, 20, nil]\n"},
		{`t = Thread.current; t.thread_variable_set(:a, 1)
		  p [t.thread_variable?(:a), t.thread_variable?(:nope)]`, "[true, false]\n"},
		{`t = Thread.current; t.thread_variable_set(:m, 1); t.thread_variable_set(:k, 2)
		  p (t.thread_variables & [:m, :k]).sort`, "[:k, :m]\n"},

		// Regexp#options / #casefold?.
		{`p [//.options, /x/i.options, /x/m.options, /x/x.options, /x/imx.options]`, "[0, 1, 4, 2, 7]\n"},
		{`p [/x/i.casefold?, /x/.casefold?]`, "[true, false]\n"},

		// Interpolated regexp literals (the load-blocking semantic_puppet bug).
		{`x = "abc"; p(/\A#{x}\z/.source)`, "\"\\\\Aabc\\\\z\"\n"},
		{`x = "abc"; p(/#{x}/.match("abc") ? 1 : nil)`, "1\n"},
		{`y = "(?:a|b)"; p(/\A#{y}\z/.match("b") ? 1 : nil)`, "1\n"},
		{`n = 3; p(/x{#{n}}/.match("xxx") ? 1 : nil)`, "1\n"}, // braces inside #{} balanced
		{`p(/a\#{b}c/.source)`, "\"a\\\\\\#{b}c\"\n"},         // escaped #{ stays literal
		{`a = 1; b = 2; p(/#{a}#{b}/.source)`, "\"12\"\n"},    // consecutive interpolations
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// Numeric send errors / undef of an undefined method.
		{`class E; end; E.send(:remove_method, :nope)`, "method 'nope' not defined in E"},
		{`class E2; end; E2.send(:undef_method, :nope)`, "undefined method 'nope'"},
		// thread_variable key must be a Symbol or String.
		{`Thread.current.thread_variable_set(1, 2)`, "is not a symbol nor a string"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
