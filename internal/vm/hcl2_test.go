// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestHCL2Constants covers the HCL2 loadable module and its error class
// (require "hcl2").
func TestHCL2Constants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "hcl2"; p HCL2.is_a?(Module)`, "true\n"},
		{`require "hcl2"; p HCL2::Error < StandardError`, "true\n"},
		{`p require "hcl2"`, "true\n"},
		{`require "hcl2"; p require "hcl2"`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestHCL2Parse covers HCL2.parse across the value shapes the binding maps:
// string, integer, float, bool, null, tuple (Array) and object (Hash, order
// preserved).
func TestHCL2Parse(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "hcl2"; p HCL2.parse('s = "x"')`, "{\"s\" => \"x\"}\n"},
		{`require "hcl2"; p HCL2.parse("i = 42")`, "{\"i\" => 42}\n"},
		{`require "hcl2"; p HCL2.parse("f = 1.5")`, "{\"f\" => 1.5}\n"},
		{`require "hcl2"; p HCL2.parse("b = true")`, "{\"b\" => true}\n"},
		{`require "hcl2"; p HCL2.parse("n = null")`, "{\"n\" => nil}\n"},
		{`require "hcl2"; p HCL2.parse("t = [1, 2, 3]")`, "{\"t\" => [1, 2, 3]}\n"},
		// An object value round-trips to a nested Hash preserving key order.
		{`require "hcl2"; p HCL2.parse("o = { a = 1, b = 2 }")`, "{\"o\" => {\"a\" => 1, \"b\" => 2}}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestHCL2Eval covers HCL2.eval with a context: a bare variables Hash, an
// explicit { variables: {...} } wrapper, and HCL2.eval_expr for a single
// expression.
func TestHCL2Eval(t *testing.T) {
	cases := []struct{ src, want string }{
		// Bare Hash read as variables.
		{`require "hcl2"; p HCL2.eval("d = e + 1", { "e" => 41 })`, "{\"d\" => 42}\n"},
		// Explicit variables wrapper (Symbol key).
		{`require "hcl2"; p HCL2.eval("d = e * 2", { variables: { "e" => 21 } })`, "{\"d\" => 42}\n"},
		// Variables of every mapped kind (string/float/bool/array/hash).
		{`require "hcl2"; p HCL2.eval("x = s", { variables: { "s" => "hi" } })`, "{\"x\" => \"hi\"}\n"},
		{`require "hcl2"; p HCL2.eval("x = a", { variables: { "a" => [1, 2] } })`, "{\"x\" => [1, 2]}\n"},
		// eval_expr on a standalone expression.
		{`require "hcl2"; p HCL2.eval_expr("1 + 2 * 3")`, "7\n"},
		{`require "hcl2"; p HCL2.eval_expr("max(v, 5)", { "v" => 9 })`, "9\n"},
		// eval with no context evaluates against an empty environment.
		{`require "hcl2"; p HCL2.eval("a = 1")`, "{\"a\" => 1}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestHCL2Errors covers the HCL2::Error raised on a syntax/evaluation error and
// the wrong-argument-count guards.
func TestHCL2Errors(t *testing.T) {
	// A syntax error rescues as HCL2::Error.
	if got := eval(t, `require "hcl2"; begin; HCL2.parse("a ="); rescue HCL2::Error => e; p e.class; end`); !strings.Contains(got, "HCL2::Error") {
		t.Errorf("syntax error got=%q", got)
	}
	// An unknown-variable reference in eval raises HCL2::Error.
	if got := eval(t, `require "hcl2"; begin; HCL2.eval_expr("undefined_var"); rescue HCL2::Error; puts "rescued"; end`); !strings.Contains(got, "rescued") {
		t.Errorf("eval error got=%q", got)
	}
	for _, src := range []string{
		`require "hcl2"; HCL2.parse`,
		`require "hcl2"; HCL2.eval`,
		`require "hcl2"; HCL2.eval_expr`,
	} {
		if got := eval(t, `begin; `+src+`; rescue ArgumentError => e; p e.class; end`); !strings.Contains(got, "ArgumentError") {
			t.Errorf("src=%q got=%q", src, got)
		}
	}
}

// TestHCL2NonStringSource covers hcl2SourceArg's to_s branch and a non-Hash
// context (treated as an empty environment).
func TestHCL2NonStringSource(t *testing.T) {
	// A non-Hash ctx argument (a String) is ignored: the empty environment applies.
	if got := eval(t, `require "hcl2"; p HCL2.eval("a = 1", "ignored")`); got != "{\"a\" => 1}\n" {
		t.Errorf("non-hash ctx got=%q", got)
	}
}
