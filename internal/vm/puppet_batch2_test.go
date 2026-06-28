// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestNumericSubclassGeneric exercises the generic Numeric instance methods on a
// bare Numeric subclass (where Integer/Float's faster overrides do not apply), so
// every delegating branch — abs via -@, fdiv, div/modulo/%/divmod, +@,
// negative?/positive? — runs. Asserted against MRI Ruby 4.0.5.
func TestNumericSubclassGeneric(t *testing.T) {
	const klass = `
class N < Numeric
  attr_reader :v
  def initialize(v); @v = v; end
  def <=>(o); @v <=> (o.is_a?(N) ? o.v : o); end
  def -@; N.new(-@v); end
  def +(o); N.new(@v + (o.is_a?(N) ? o.v : o)); end
  def -(o); N.new(@v - (o.is_a?(N) ? o.v : o)); end
  def *(o); N.new(@v * (o.is_a?(N) ? o.v : o)); end
  def /(o); N.new(@v / (o.is_a?(N) ? o.v : o)); end
  def to_f; @v.to_f; end
  def floor; N.new(@v.floor); end
  def to_s; "N(#{@v})"; end
  def inspect; to_s; end
end
`
	cases := []struct{ src, want string }{
		{klass + `p N.new(7).abs.to_s`, "\"N(7)\"\n"},
		{klass + `p N.new(-3).abs.to_s`, "\"N(3)\"\n"},
		{klass + `p N.new(-3).magnitude.to_s`, "\"N(3)\"\n"},
		{klass + `p [N.new(-3).negative?, N.new(7).positive?, N.new(0).negative?]`, "[true, true, false]\n"},
		{klass + `p N.new(7).send(:+@).to_s`, "\"N(7)\"\n"},
		{klass + `p N.new(7).abs2.to_s`, "\"N(49)\"\n"},
		{klass + `p N.new(7).fdiv(N.new(2))`, "3.5\n"},
		{klass + `p N.new(7).div(N.new(2)).to_s`, "\"N(3)\"\n"},
		{klass + `p N.new(7).modulo(N.new(3)).to_s`, "\"N(1)\"\n"},
		{klass + `p (N.new(7) % N.new(3)).to_s`, "\"N(1)\"\n"},
		{klass + `p N.new(7).divmod(N.new(3)).map(&:to_s)`, "[\"N(2)\", \"N(1)\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// cmpZero raises ArgumentError when <=> with 0 does not yield an Integer.
	bad := `class B < Numeric
  def <=>(o); nil; end
  def -@; self; end
end
B.new.abs`
	if err := runErr(t, bad); err == nil || !strings.Contains(err.Error(), "comparison of B with 0 failed") {
		t.Errorf("cmpZero err=%v", err)
	}
}

// TestRegexpInterpolation covers interpolated regexp literals end to end: the
// load-blocking semantic_puppet pattern, nested braces inside #{}, consecutive
// interpolations, an escaped #{ kept literal, and a malformed #{} segment that
// compiles to an empty match. Asserted against MRI Ruby 4.0.5.
func TestRegexpInterpolation(t *testing.T) {
	cases := []struct{ src, want string }{
		{`x = "abc"; p(/\A#{x}\z/.source)`, "\"\\\\Aabc\\\\z\"\n"},
		{`x = "abc"; p(/#{x}/.match("abc") ? 1 : nil)`, "1\n"},
		{`a = 1; b = 2; p(/#{a}#{b}/.source)`, "\"12\"\n"},
		{`p(/#{ {a: 1}.size }/.source)`, "\"1\"\n"},   // braces inside the #{} are balanced
		{`p(/a\#{b}c/.source)`, "\"a\\\\\\#{b}c\"\n"}, // escaped #{ stays literal
		// A pure literal regexp keeps the static path (no interpolation).
		{`p(/plain/.source)`, "\"plain\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// A syntactically invalid #{...} segment compiles to an empty fragment (the
	// segment contributes nothing), so /#{(}/ becomes // and matches everywhere.
	if got := eval(t, "p(/x#{(}y/.match('xy') ? 1 : nil)"); got != "1\n" {
		t.Errorf("malformed interp segment: got %q", got)
	}
}

// TestEmptyKwSplatDrop covers the keyword-separation fix: forwarding a `**kw`
// that is empty at runtime passes no argument (so a zero-arg method is callable),
// while a non-empty splat and a braced positional hash are unaffected. Asserted
// against MRI Ruby 4.0.5.
func TestEmptyKwSplatDrop(t *testing.T) {
	cases := []struct{ src, want string }{
		// Empty **kwargs forwarded to a no-arg method: dropped, so the call works.
		{`def zero; "ok"; end
		  def fwd(*a, **k); zero(*a, **k); end
		  p fwd`, "\"ok\"\n"},
		{`def zero; "ok"; end
		  def fwd2(**k); zero(**k); end
		  p fwd2`, "\"ok\"\n"},
		// Non-empty keyword splat still binds keywords.
		{`def m(a, k: 5); [a, k]; end; p m(1, **{k: 9})`, "[1, 9]\n"},
		{`def m(a, k: 5); [a, k]; end; p m(1, **{})`, "[1, 5]\n"},
		// A braced positional hash (no ** entry) is always passed, even when empty.
		{`def takeshash(h); h; end; p takeshash({})`, "{}\n"},
		{`def takeshash(h); h; end; p takeshash({a: 1})`, "{a: 1}\n"},
		// A positional splat alongside an empty kwsplat.
		{`def s(*a); a; end; p s(1, 2, **{})`, "[1, 2]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDefinedOperatorMethod covers defined?(recv op arg) routing an operator name
// with no method-table entry (a plain object) through operatorOpcode, including %.
func TestDefinedOperatorMethod(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class Plain; end; p defined?(Plain.new % 1)`, "\"method\"\n"},
		{`class Plain; end; p defined?(Plain.new + 1)`, "\"method\"\n"},
		{`class Plain; end; p defined?(Plain.new != 1)`, "\"method\"\n"},
		// Unary ~/+@/-@ resolve as real numeric methods, so defined? reports them.
		{`p defined?(5.~)`, "\"method\"\n"},
		{`p defined?(5.send(:-@))`, "\"method\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
