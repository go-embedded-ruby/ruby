// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestPostParamsArityAndParameters pins the POST positional parameters —
// MRI's param.post_num — on every callable that reports a parameter shape.
//
// rb_iseq_min_max_arity's minimum is lead_num + post_num (proc.c v3_4_0:1079)
// and rb_iseq_parameters emits :req for [post_start, post_start+post_num)
// (iseq.c v3_4_0:3629-3641). Neither reads has_rest: post parameters exist in
// BOTH shapes — after a *splat, and, with no splat, as the trailing required
// run of `def m(a=1, b)`.
//
// rbgo had each half of that wrong in a different place, so the two answers
// disagreed with MRI in opposite directions and #650, which fixed Proc#arity
// for the no-splat shape, could not have found either:
//
//   - the METHOD side (methodArity, buildParamsList) knew only the after-splat
//     post run, so `def m(a=1, b)` answered arity -1 for MRI's -2 and
//     parameters [[:opt, :a], [:opt, :b]] for MRI's [[:opt, :a], [:req, :b]];
//   - the PROC side (Proc#arityVal) read is.PostCount directly, which is 0
//     whenever there IS a splat, so `proc { |a, *b, c| }` answered -2 for -3.
//
// iseqPostCount already reported both; every reader now goes through it.
func TestPostParamsArityAndParameters(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// No splat, post run of 1. The shape the issue names.
		{`def m(a=1, b); end; p method(:m).arity`, "-2\n"},
		{`def m(a=1, b); end; p method(:m).parameters`, "[[:opt, :a], [:req, :b]]\n"},
		{`def m(a=1, b); end; p self.class.instance_method(:m).arity`, "-2\n"},
		{`def m(a=1, b); end; p self.class.instance_method(:m).parameters`,
			"[[:opt, :a], [:req, :b]]\n"},
		{`def m(a=1, b); end; p method(:m).to_proc.arity`, "-2\n"},

		// Leading required, an optional, then a post run.
		{`def m(a, b=1, c); end; p method(:m).arity`, "-3\n"},
		{`def m(a, b=1, c); end; p method(:m).parameters`,
			"[[:req, :a], [:opt, :b], [:req, :c]]\n"},
		{`def m(a=1, b=2, c, d); end; p method(:m).arity`, "-3\n"},
		{`def m(a=1, b=2, c, d); end; p method(:m).parameters`,
			"[[:opt, :a], [:opt, :b], [:req, :c], [:req, :d]]\n"},

		// A define_method body reports the same shape as a def.
		{`c = Class.new { define_method(:dm) { |a=1, b| } }
p [c.instance_method(:dm).arity, c.instance_method(:dm).parameters]`,
			"[-2, [[:opt, :a], [:req, :b]]]\n"},

		// The splat shape, which the PROC half got wrong.
		{`p proc { |a, *b, c| }.arity`, "-3\n"},
		{`p lambda { |a, *b, c| }.arity`, "-3\n"},
		{`p lambda { |a=1, b| }.arity`, "-2\n"},
		{`p lambda { |a=1, b| }.parameters`, "[[:opt, :a], [:req, :b]]\n"},

		// A non-lambda proc enforces no positional, so it reports its post
		// parameters as :opt and stays on the POSITIVE required count — the
		// is_proc branch of rb_iseq_parameters (iseq.c v3_4_0:3610-3628).
		{`p proc { |a=1, b| }.arity`, "1\n"},
		{`p proc { |a=1, b| }.parameters`, "[[:opt, :a], [:opt, :b]]\n"},
		{`p proc { |a, *b, c| }.parameters`, "[[:opt, :a], [:rest, :b], [:opt, :c]]\n"},

		// Everything together, unchanged: the shapes that were already right.
		{`def m(a, b=1, *r, c, d:, **k, &bl); end; p method(:m).arity`, "-4\n"},
		{`def m(a, b=1, *r, c, d:, **k, &bl); end; p method(:m).parameters`,
			"[[:req, :a], [:opt, :b], [:rest, :r], [:req, :c], [:keyreq, :d], " +
				"[:keyrest, :k], [:block, :bl]]\n"},
		{`def m(a, b); end; p [method(:m).arity, method(:m).parameters]`,
			"[2, [[:req, :a], [:req, :b]]]\n"},
		{`def m(a=1); end; p [method(:m).arity, method(:m).parameters]`,
			"[-1, [[:opt, :a]]]\n"},
		{`def m(*a); end; p [method(:m).arity, method(:m).parameters]`,
			"[-1, [[:rest, :a]]]\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestPostParamsBindFromTheTail is the CONTROL for the reflection above: the
// counts are only worth pinning if the binder agrees with them. A post
// parameter binds from the END of the argument list before the optionals are
// filled from what is left (setup_parameters_complex, vm_args.c v3_4_0:878-892),
// so `m(9)` gives the post parameter the 9 and leaves the optional on its
// default — the opposite of front-to-back binding, which would report the same
// arity while running the wrong body.
func TestPostParamsBindFromTheTail(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`def m(a=1, b); [a, b]; end; p m(9)`, "[1, 9]\n"},
		{`def m(a=1, b); [a, b]; end; p m(8, 9)`, "[8, 9]\n"},
		{`def m(a, b=2, c); [a, b, c]; end; p m(1, 3)`, "[1, 2, 3]\n"},
		{`def m(a=1, b); end; begin; m; rescue ArgumentError => e; p e.message; end`,
			"\"wrong number of arguments (given 0, expected 1..2)\"\n"},
		{`p proc { |a=5, b, c, d| [a, b, c, d] }.call(1, 2)`, "[5, 1, 2, nil]\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
