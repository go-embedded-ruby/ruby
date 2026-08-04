// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestMethodReflect covers Method/UnboundMethod equality, hashing, composition
// (>> / <<) and currying, plus the shared Proc composition operators. Asserted
// against MRI 3.4.
func TestMethodReflect(t *testing.T) {
	cases := []struct{ src, want string }{
		// == / eql?: same receiver + same definition; aliases equal; distinct defs not.
		{`o=Object.new; def o.foo; 1; end; p(o.method(:foo) == o.method(:foo))`, "true\n"},
		{`class C; def foo; end; alias bar foo; end; c=C.new; p(c.method(:foo) == c.method(:bar))`, "true\n"},
		{`s="hi"; p(s.method(:size) == s.method(:length))`, "true\n"},
		{`class D; def foo; end; def other; end; end; d=D.new; p(d.method(:foo) == d.method(:other))`, "false\n"},
		{`class E; def foo; end; end; a=E.new; b=E.new; p(a.method(:foo) == b.method(:foo))`, "false\n"},
		{`o=Object.new; def o.foo; end; p(o.method(:foo) == 7)`, "false\n"},
		// define_method transplanting an instance_method compares equal to the source.
		{`class F; def foo; end; end; F.send(:define_method, :baz, F.instance_method(:foo)); f=F.new; p(f.method(:foo) == f.method(:baz))`, "true\n"},
		// attr_reader getters for different ivars are distinct methods.
		{`class G; attr_reader :a, :b; end; p(G.instance_method(:a) == G.instance_method(:b))`, "false\n"},
		// eql? is an alias of == (shared record).
		{`p(Method.instance_method(:eql?) == Method.instance_method(:==))`, "true\n"},
		// proc-backed (define_method) methods compare by their shared proc body.
		{`class Q; define_method(:f){1}; end; q=Q.new; p(q.method(:f) == q.method(:f))`, "true\n"},
		{`class R; define_method(:f){1}; define_method(:g){1}; end; r=R.new; p(r.method(:f) == r.method(:g))`, "false\n"},
		// UnboundMethod#hash agrees for aliased (eql?) unbound methods.
		{`class Z; def foo; end; alias bar foo; end; p(Z.instance_method(:foo).hash == Z.instance_method(:bar).hash)`, "true\n"},
		// hash agrees for eql? methods.
		{`class H; def foo; end; alias bar foo; end; h=H.new; p(h.method(:foo).hash == h.method(:bar).hash)`, "true\n"},
		// Composition: >> runs self first, << runs the argument first.
		{`succ=->(x){x+1}; dbl=->(x){x*2}; p (succ >> dbl).call(3)`, "8\n"},
		{`succ=->(x){x+1}; dbl=->(x){x*2}; p (succ << dbl).call(3)`, "7\n"},
		{`o=Object.new; def o.inc(n); n+1; end; dbl=proc{|x| x+x}; p (o.method(:inc) >> dbl).call(3)`, "8\n"},
		{`o=Object.new; def o.inc(n); n+1; end; dbl=proc{|x| x+x}; p (o.method(:inc) << dbl).call(3)`, "7\n"},
		// Composition lambda-ness follows the first-run callable.
		{`o=Object.new; def o.pow(x); x*x; end; dbl=proc{|x| x+x}; p (o.method(:pow) >> dbl).lambda?`, "true\n"},
		{`o=Object.new; def o.pow(x); x*x; end; dbl=proc{|x| x+x}; p (o.method(:pow) << dbl).lambda?`, "false\n"},
		// Composition accepts any callable object (generic #call), and passes all args.
		{`o=Object.new; def o.inc(n); n+1; end; d=Object.new; def d.call(n); n*2; end; p (o.method(:inc) << d).call(3)`, "7\n"},
		{`o=Object.new; def o.inc(n); n+1; end; mul=proc{|n,m| n*m}; p (o.method(:inc) << mul).call(2,3)`, "7\n"},
		// Method#curry.
		{`x=Object.new; def x.foo(a,b,c); [a,b,c]; end; c=x.method(:foo).curry; p(c.is_a?(Proc)); p c.call(1).call(2,3)`, "true\n[1, 2, 3]\n"},
		{`o=Object.new; def o.two(a,b,*r); [a,b,r]; end; p(o.method(:two).curry(2).is_a?(Proc))`, "true\n"},
		// curry over a define_method (proc-backed) method with an iseq.
		{`class P1; define_method(:add){|a,b| a+b}; end; p P1.new.method(:add).curry.call(2).call(5)`, "7\n"},
		// curry over a native method returns a Proc.
		{`p("abc".method(:upcase).curry.is_a?(Proc))`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// Composition rejects non-callable arguments and does not coerce via #to_proc.
		{`(->(x){x}) >> 5`, "TypeError"},
		{`(->(x){x}) << Object.new`, "TypeError"},
		{`o=Object.new; def o.f; end; o.method(:f) >> 5`, "TypeError"},
		{`s=Object.new; def s.to_proc(x); x; end; (->(x){x}) >> s`, "TypeError"},
		// curry arity validation.
		{`o=Object.new; def o.zero; end; o.method(:zero).curry(1)`, "ArgumentError"},
		{`o=Object.new; def o.one(a); end; o.method(:one).curry(0)`, "ArgumentError"},
		{`o=Object.new; def o.reqopt(a, b=1); end; o.method(:reqopt).curry(3)`, "ArgumentError"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
