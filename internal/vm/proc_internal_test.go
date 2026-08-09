// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"
)

// TestProcParameters covers Proc#parameters: the proc/lambda :opt-vs-:req
// distinction, the lambda: keyword override, destructuring, anonymous rest, a
// native (Symbol#to_proc) proc, and the keyword / block descriptors — all
// verified against ruby 4.0.5.
func TestProcParameters(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"proc_empty", `p proc{}.parameters`, "[]\n"},
		{"proc_req_as_opt", `p proc{|a,b| }.parameters`, "[[:opt, :a], [:opt, :b]]\n"},
		{"lambda_req", `p lambda{|a,b| }.parameters`, "[[:req, :a], [:req, :b]]\n"},
		{"proc_mixed", `p proc{|a,b,c=1,*r,k:,l:2,**o,&bl| }.parameters`,
			"[[:opt, :a], [:opt, :b], [:opt, :c], [:rest, :r], [:keyreq, :k], [:key, :l], [:keyrest, :o], [:block, :bl]]\n"},
		{"lambda_mixed", `p lambda{|a,b,c=1,*r,k:,l:2,**o,&bl| }.parameters`,
			"[[:req, :a], [:req, :b], [:opt, :c], [:rest, :r], [:keyreq, :k], [:key, :l], [:keyrest, :o], [:block, :bl]]\n"},
		{"proc_postsplat", `p proc{|a,*b,c| }.parameters`, "[[:opt, :a], [:rest, :b], [:opt, :c]]\n"},
		{"lambda_postsplat", `p lambda{|a,*b,c| }.parameters`, "[[:req, :a], [:rest, :b], [:req, :c]]\n"},
		{"destructure", `p proc{|(a,b),c| }.parameters`, "[[:opt], [:opt, :c]]\n"},
		{"anon_rest", `p(->(*){}.parameters)`, "[[:rest, :*]]\n"},
		{"anon_block", `p(->(&){}.parameters)`, "[[:block, :&]]\n"},
		// The lambda: keyword overrides the receiver's own lambda-ness.
		{"kw_true_on_proc", `p proc{|x| }.parameters(lambda: true)`, "[[:req, :x]]\n"},
		{"kw_false_on_lambda", `p(->x{}.parameters(lambda: false))`, "[[:opt, :x]]\n"},
		{"kw_truthy_int", `p proc{|x| }.parameters(lambda: 123)`, "[[:req, :x]]\n"},
		{"kw_nil_ignored_proc", `p proc{|x| }.parameters(lambda: nil)`, "[[:opt, :x]]\n"},
		{"kw_nil_ignored_lambda", `p(->x{}.parameters(lambda: nil))`, "[[:req, :x]]\n"},
		// A native proc (Symbol#to_proc has no iseq): MRI reports a catch-all rest.
		{"native_proc", `p :foo.to_proc.parameters`, "[[:rest]]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestProcArity covers Proc#arity for both proc and lambda, including the
// keyword-aware rules (a required keyword adds one; an optional positional,
// optional keyword or keyword-rest makes a lambda variadic while a proc stays
// fixed) and a native proc. Verified against ruby 4.0.5.
func TestProcArity(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"proc_two", `p proc{|a,b| }.arity`, "2\n"},
		{"proc_splat", `p proc{|*a| }.arity`, "-1\n"},
		{"proc_opt", `p proc{|a,b=1| }.arity`, "1\n"},
		{"lambda_opt", `p lambda{|a,b=1| }.arity`, "-2\n"},
		{"lambda_zero", `p(->(){}.arity)`, "0\n"},
		{"lambda_block_only", `p(->(&b){}.arity)`, "0\n"},
		{"lambda_anon_splat", `p(->(*){}.arity)`, "-1\n"},
		{"lambda_reqkw", `p(->(a:){}.arity)`, "1\n"},
		{"lambda_two_reqkw", `p(->(a:,b:){}.arity)`, "1\n"},
		{"lambda_pos_reqkw", `p(->(a,b:){}.arity)`, "2\n"},
		{"lambda_optkw", `p(->(a: 1){}.arity)`, "-1\n"},
		{"lambda_pos_optkw", `p(->(a, b: 1){}.arity)`, "-2\n"},
		{"lambda_kwrest", `p(->(**k){}.arity)`, "-1\n"},
		{"lambda_pos_kwrest", `p(->(a, **k){}.arity)`, "-2\n"},
		{"lambda_optpos_reqkw", `p(->(a=1, b:){}.arity)`, "-2\n"},
		{"proc_reqkw", `p proc{|a:| }.arity`, "1\n"},
		{"proc_optkw", `p proc{|a: 1| }.arity`, "0\n"},
		{"proc_pos_reqkw", `p proc{|a, b:| }.arity`, "2\n"},
		{"proc_kwrest", `p proc{|**k| }.arity`, "0\n"},
		{"proc_optpos_reqkw", `p proc{|a=1, b:| }.arity`, "1\n"},
		{"native_arity", `p :foo.to_proc.arity`, "-2\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestProcConversions covers Proc#to_proc, #binding, #lambda?, #source_location,
// #curry, #>>, #<<, #===, #hash/#== and #to_s/#inspect.
func TestProcConversions(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"to_proc_self", `pr = proc{}; p pr.to_proc.equal?(pr)`, "true\n"},
		{"to_proc_lambda_self", `l = ->{}; p l.to_proc.equal?(l)`, "true\n"},
		{"lambda_pred", `p [proc{}.lambda?, lambda{}.lambda?]`, "[false, true]\n"},
		{"binding_class", `p proc{}.binding.class`, "Binding\n"},
		{"binding_reaches_locals", `def m(x); ->{}.binding; end; p eval("x", m(41))`, "41\n"},
		{"curry_proc", `f = proc{|a,b,c| a+b+c}; p f.curry[1][2][3]`, "6\n"},
		{"curry_lambda", `f = ->(a,b,c){ a+b+c}; p f.curry[1][2][3]`, "6\n"},
		{"compose_forward", `f = proc{|x|x+1}; g = proc{|x|x*2}; p (f >> g).call(3)`, "8\n"},
		{"compose_backward", `f = proc{|x|x+1}; g = proc{|x|x*2}; p (f << g).call(3)`, "7\n"},
		{"case_compare", `even = proc{|n| n.even?}; p(even === 4)`, "true\n"},
		{"hash_stable", `pr = proc{}; p pr.hash == pr.hash`, "true\n"},
		{"equal_self", `pr = proc{}; p pr == pr`, "true\n"},
		{"to_s_proc", `p proc{}.to_s`, "\"#<Proc>\"\n"},
		{"to_s_lambda", `p lambda{}.to_s`, "\"#<Proc (lambda)>\"\n"},
		{"to_s_symproc", `p :foo.to_proc.to_s`, "\"#<Proc (&:foo)>\"\n"},
		{"inspect_alias", `p(Proc.instance_method(:inspect) == Proc.instance_method(:to_s))`, "true\n"},
		{"ruby2_keywords_self", `f = ->(*a){}; p f.ruby2_keywords.equal?(f)`, "true\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestProcCallSemantics covers the call-shape differences between a proc and a
// lambda: a proc pads/drops/auto-splats leniently while a lambda binds with
// method semantics and raises ArgumentError on a mismatch.
func TestProcCallSemantics(t *testing.T) {
	// Proc: lenient positional shaping.
	oks := []struct{ name, src, want string }{
		{"proc_pad", `p proc{|a,b| [a,b]}.call(1)`, "[1, nil]\n"},
		{"proc_drop", `p proc{|a| a}.call(1,2,3)`, "1\n"},
		{"proc_autosplat", `p proc{|a,b| [a,b]}.call([1,2])`, "[1, 2]\n"},
	}
	for _, tc := range oks {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
	// Lambda: strict arity, no auto-splat — each raises ArgumentError.
	errs := []struct{ name, src string }{
		{"lambda_too_few", `lambda{|a,b|}.call(1)`},
		{"lambda_too_many", `lambda{|a|}.call(1,2)`},
		{"lambda_no_autosplat", `lambda{|a,b|}.call([1,2])`},
		{"lambda_curry_strict", `lambda{|a,b,c|}.curry[1,2,3,4]`},
	}
	for _, tc := range errs {
		t.Run(tc.name, func(t *testing.T) {
			class, _ := evalErr(t, tc.src)
			if class != "ArgumentError" {
				t.Errorf("src=%q: got %s, want ArgumentError", tc.src, class)
			}
		})
	}
}

// TestProcReturnSemantics covers the non-local return: a bare proc's `return`
// returns from the method that created it (LocalJumpError once that frame is
// gone), while a lambda's `return` returns only from the lambda.
func TestProcReturnSemantics(t *testing.T) {
	if got := eval(t, "def m; f = proc{ return 42 }; f.call; 99; end; p m"); got != "42\n" {
		t.Errorf("proc return: got %q, want \"42\\n\"", got)
	}
	if got := eval(t, "def m; f = lambda{ return 7 }; f.call + 1; end; p m"); got != "8\n" {
		t.Errorf("lambda return: got %q, want \"8\\n\"", got)
	}
	class, _ := evalErr(t, "def make; proc{ return 1 }; end; make.call")
	if class != "LocalJumpError" {
		t.Errorf("orphan proc return: got %s, want LocalJumpError", class)
	}
}

// TestProcBindingNativeRaises covers Proc#binding on a synthesized (native)
// proc, which has no Ruby frame to capture.
func TestProcBindingNativeRaises(t *testing.T) {
	class, msg := evalErr(t, ":foo.to_proc.binding")
	if class != "ArgumentError" || msg != "Can't create Binding from C level Proc" {
		t.Errorf("got (%s, %q), want (ArgumentError, %q)", class, msg, "Can't create Binding from C level Proc")
	}
}

// TestProcRuby2KeywordsShapes exercises every branch of the markability check: a
// markable *rest-only proc (no warning) and each non-markable shape (no splat, a
// keyword, a keyword-rest, a post-splat positional, and a native proc). All
// return self; a non-markable one additionally prints the skip warning (the test
// VM routes warn to the captured buffer, so it precedes the "true" line).
func TestProcRuby2KeywordsShapes(t *testing.T) {
	const warn = "Skipping set of ruby2_keywords flag for"
	cases := []struct {
		src      string
		markable bool
	}{
		{`f = ->(*a){}; p f.ruby2_keywords.equal?(f)`, true},       // markable: no warning
		{`f = ->(a){}; p f.ruby2_keywords.equal?(f)`, false},       // no splat
		{`f = ->(*a, k:){}; p f.ruby2_keywords.equal?(f)`, false},  // has keyword
		{`f = ->(*a, **k){}; p f.ruby2_keywords.equal?(f)`, false}, // has keyword-rest
		{`f = ->(*a, b){}; p f.ruby2_keywords.equal?(f)`, false},   // post-splat positional
		{`f = :foo.to_proc; p f.ruby2_keywords.equal?(f)`, false},  // native (no iseq)
	}
	for _, tc := range cases {
		got := eval(t, tc.src)
		if !strings.HasSuffix(got, "true\n") {
			t.Errorf("src=%q: got %q, want it to return self (true)", tc.src, got)
		}
		if warned := strings.Contains(got, warn); warned == tc.markable {
			t.Errorf("src=%q: markable=%v but warning-present=%v (out=%q)", tc.src, tc.markable, warned, got)
		}
	}
}
