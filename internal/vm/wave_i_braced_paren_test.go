// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"
)

// TestWaveIBracedHashAtACallSite pins the observable half of ast.HashLit.Braced
// (go-ruby-parser v0.9.0): a hash WRITTEN with braces is a positional argument,
// and a bare `k: v` list is keywords.
//
// Every `want` below was read off ruby 4.0.5 before the fix, under BOTH
// `--parser=parse.y` and `--parser=prism`, which agree on all of it. rbgo used
// to answer the keyword reading for every braced row.
//
// The probe varies with its input by construction: the rows come in pairs that
// differ only by the braces, plus `f(h)` — a VARIABLE holding a hash, which was
// already correct — so a change that made every trailing hash positional would
// fail all six keyword rows, and one that made none positional would fail all
// the braced ones.
func TestWaveIBracedHashAtACallSite(t *testing.T) {
	const prelude = "def f(*a, **k) = [a, k]\nh = {y: 2}\n"
	for _, c := range []struct{ src, want string }{
		// --- braced: POSITIONAL ---
		{`p f({x: 1})`, `[[{x: 1}], {}]`},
		{`p f({})`, `[[{}], {}]`}, // and NOT f(), which passes nothing
		{`p f({**h})`, `[[{y: 2}], {}]`},
		{`p f(1, {x: 1})`, `[[1, {x: 1}], {}]`},
		{`p f({x: 1}, {y: 2})`, `[[{x: 1}, {y: 2}], {}]`},
		{`p f(*[1], {x: 1})`, `[[1, {x: 1}], {}]`},
		{`p f({x: 1}) { }`, `[[{x: 1}], {}]`},
		{`p f({"s" => 1})`, `[[{"s" => 1}], {}]`},
		// --- bare: KEYWORDS (unchanged) ---
		{`p f(x: 1)`, `[[], {x: 1}]`},
		{`p f(**h)`, `[[], {y: 2}]`},
		{`p f(1, x: 1)`, `[[1], {x: 1}]`},
		{`p f(*[1], x: 1)`, `[[1], {x: 1}]`},
		{`p f("s" => 1)`, `[[], {"s" => 1}]`},
		{`p f()`, `[[], {}]`},
		// --- a VARIABLE holding a hash was ALWAYS positional: the honest scope ---
		{`p f(h)`, `[[{y: 2}], {}]`},
		// --- and the arity a braced hash now changes ---
		{`def k2(**k) = k
begin; k2({x: 1}); rescue => e; p [e.class, e.message]; end`,
			`[ArgumentError, "wrong number of arguments (given 1, expected 0)"]`},
		{`def p1(a = 9, **k) = [a, k]
p p1({x: 1})`, `[{x: 1}, {}]`},
		{`def p1(a = 9, **k) = [a, k]
p p1(x: 1)`, `[9, {x: 1}]`},
	} {
		if got := strings.TrimSpace(eval(t, prelude+c.src)); got != c.want {
			t.Errorf("%s\n got %s\nwant %s", c.src, got, c.want)
		}
	}
}

// TestWaveIBracedHashElsewhere: Braced says only "written with braces", exactly
// as MRI's nd_brace does — it is not conditional on being an argument
// (parse.y-ruby_4_0:4415-4419). So a braced hash away from a call site, and a
// yield or a super, must keep behaving as before. Read off ruby 4.0.5.
func TestWaveIBracedHashElsewhere(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"x = {a: 1}\np x", `{a: 1}`},
		{"p [{a: 1}]", `[{a: 1}]`},
		{"h = {a: 1}\np({**h, b: 2})", `{a: 1, b: 2}`},
		// yield is an ordinary call site and follows the same rule.
		{"def y1; yield({x: 1}); end\ny1 { |*a, **k| p [a, k] }", `[[{x: 1}], {}]`},
		{"def y1; yield(x: 1); end\ny1 { |*a, **k| p [a, k] }", `[[], {x: 1}]`},
		// and so does super.
		{`class A; def m(*a, **k) = [a, k]; end
class B < A; def m = super({x: 1}); end
p B.new.m`, `[[{x: 1}], {}]`},
		{`class A; def m(*a, **k) = [a, k]; end
class B < A; def m = super(x: 1); end
p B.new.m`, `[[], {x: 1}]`},
	} {
		if got := strings.TrimSpace(eval(t, c.src)); got != c.want {
			t.Errorf("%s\n got %s\nwant %s", c.src, got, c.want)
		}
	}
}

// TestWaveIParenSeparatesVCallFromFCall pins ast.Call.Paren end to end: `nope`
// is MRI's NODE_VCALL and raises NameError, `nope()` is an FCALL and raises
// NoMethodError (parse.y-ruby_4_0:13086 against 3572-3577 + 5242-5248). Read off
// ruby 4.0.5 under both parsers, which agree.
//
// Only the FIRST row is a VCALL. The rest are the near-miss shapes, and they are
// the known-bad control: a change that raised NameError for every receiver-less
// name fails all nine of them.
func TestWaveIParenSeparatesVCallFromFCall(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`nope_xyz`, "NameError"},
		{`nope_xyz()`, "NoMethodError"},
		{`nope_xyz!`, "NoMethodError"},
		{`nope_xyz?`, "NoMethodError"},
		{`nope_xyz!()`, "NoMethodError"},
		{`nope_xyz?()`, "NoMethodError"},
		{`nope_xyz { }`, "NoMethodError"},
		{`nope_xyz() { }`, "NoMethodError"},
		{`self.nope_xyz`, "NoMethodError"},
		{`nope_xyz(1)`, "NoMethodError"},
	} {
		src := "begin; " + c.src + "; rescue Exception => e; p e.class; end"
		if got := strings.TrimSpace(eval(t, src)); got != c.want {
			t.Errorf("%s: got %s, want %s", c.src, got, c.want)
		}
	}
	// The message moves with the class, and only for the VCALL.
	for _, c := range []struct{ src, want string }{
		{`nope_xyz`, `"undefined local variable or method 'nope_xyz' for main"`},
		{`nope_xyz()`, `"undefined method 'nope_xyz' for main"`},
	} {
		src := "begin; " + c.src + "; rescue Exception => e; p e.message; end"
		if got := strings.TrimSpace(eval(t, src)); got != c.want {
			t.Errorf("%s: got %s, want %s", c.src, got, c.want)
		}
	}
}
