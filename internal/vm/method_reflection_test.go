// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestMethodParameters covers Method#parameters and UnboundMethod#parameters for
// every parameter kind — required (leading and post-splat), optional, rest,
// keyword (required and optional), keyword-rest, block, the **nil "no keywords"
// marker, and the anonymous / forwarded (*, **, &) spellings — plus the
// catch-all shape reported for a native method. Asserted against MRI Ruby 4.0.5.
func TestMethodParameters(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class C; def z; end; end; p C.new.method(:z).parameters`, "[]\n"},
		{`class C; def f(a); end; end; p C.new.method(:f).parameters`, "[[:req, :a]]\n"},
		{`class C; def f(a, b); end; end; p C.new.method(:f).parameters`, "[[:req, :a], [:req, :b]]\n"},
		{`class C; def f(a, b=1); end; end; p C.new.method(:f).parameters`, "[[:req, :a], [:opt, :b]]\n"},
		{`class C; def f(a, *b); end; end; p C.new.method(:f).parameters`, "[[:req, :a], [:rest, :b]]\n"},
		{`class C; def f(*a, b); end; end; p C.new.method(:f).parameters`, "[[:rest, :a], [:req, :b]]\n"},
		{`class C; def f(a, b=1, *c, d, &e); end; end; p C.new.method(:f).parameters`,
			"[[:req, :a], [:opt, :b], [:rest, :c], [:req, :d], [:block, :e]]\n"},
		{`class C; def f(&blk); end; end; p C.new.method(:f).parameters`, "[[:block, :blk]]\n"},
		{`class C; def f(a:, b: 2, **r); end; end; p C.new.method(:f).parameters`,
			"[[:keyreq, :a], [:key, :b], [:keyrest, :r]]\n"},
		{`class C; def f(**nil); end; end; p C.new.method(:f).parameters`, "[[:nokey]]\n"},
		{`class C; def f(*); end; end; p C.new.method(:f).parameters`, "[[:rest, :*]]\n"},
		{`class C; def f(**); end; end; p C.new.method(:f).parameters`, "[[:keyrest, :**]]\n"},
		{`class C; def f(&); end; end; p C.new.method(:f).parameters`, "[[:block, :&]]\n"},
		{`class C; def f(...); end; end; p C.new.method(:f).parameters`,
			"[[:rest, :*], [:keyrest, :**], [:block, :&]]\n"},
		// UnboundMethod shares the same shape.
		{`class C; def f(a, *b, c); end; end; p C.instance_method(:f).parameters`,
			"[[:req, :a], [:rest, :b], [:req, :c]]\n"},
		// A define_method (proc-backed) method reads its block's parameters.
		{`class C; define_method(:f){|x, y=1| }; end; p C.new.method(:f).parameters`,
			"[[:req, :x], [:opt, :y]]\n"},
		// A native method reports a single catch-all rest.
		{`p "abc".method(:upcase).parameters`, "[[:rest]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestMethodArity covers Method#arity across every MRI category: fixed positional,
// optional / splat / keyword-rest driven variadic, required-keyword (which stays
// non-negative even beside optional keywords or a **rest), and native (-1).
func TestMethodArity(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class C; def f(a); end; end; p C.new.method(:f).arity`, "1\n"},
		{`class C; def f(a, k: 1); end; end; p C.new.method(:f).arity`, "-2\n"},
		{`class C; def f(a, k:); end; end; p C.new.method(:f).arity`, "2\n"},
		{`class C; def f(a, b=1); end; end; p C.new.method(:f).arity`, "-2\n"},
		{`class C; def f(a, *b); end; end; p C.new.method(:f).arity`, "-2\n"},
		{`class C; def f(a, **k); end; end; p C.new.method(:f).arity`, "-2\n"},
		{`class C; def f(a, k:, j: 1); end; end; p C.new.method(:f).arity`, "2\n"},
		{`class C; def f(**k); end; end; p C.new.method(:f).arity`, "-1\n"},
		{`class C; def f(a, b, k1:, k2:); end; end; p C.new.method(:f).arity`, "3\n"},
		{`class C; def f(a:, b: 2, **r); end; end; p C.new.method(:f).arity`, "1\n"},
		{`class C; def f(*a, b); end; end; p C.new.method(:f).arity`, "-2\n"},
		{`p "x".method(:upcase).arity`, "-1\n"},
		// UnboundMethod#arity agrees.
		{`class C; def f(a, b=1); end; end; p C.instance_method(:f).arity`, "-2\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMethodSourceLocation covers Method/UnboundMethod#source_location: nil for a
// native method (or a method compiled without a source file), and [file, 0] for a
// method loaded from a named script (the VM tracks no per-line info, so the line
// is 0).
func TestMethodSourceLocation(t *testing.T) {
	if got := eval(t, `p "x".method(:upcase).source_location`); got != "nil\n" {
		t.Errorf("native source_location got %q", got)
	}
	if got := eval(t, `class C; def f; end; end; p C.new.method(:f).source_location`); got != "nil\n" {
		t.Errorf("fileless source_location got %q", got)
	}
	got, err := runScript(t, "class C; def f; end; end\np C.new.method(:f).source_location\np C.instance_method(:f).source_location", "/scripts/main.rb")
	if err != nil || got != "[\"/scripts/main.rb\", 0]\n[\"/scripts/main.rb\", 0]\n" {
		t.Errorf("source_location got=%q err=%v", got, err)
	}
}

// TestMethodOriginalName covers Method/UnboundMethod#original_name: the plain
// name, the original preserved through `alias`, and the source name preserved
// through a define_method transplant of a native and of an instance method.
func TestMethodOriginalName(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "abc".method(:upcase).original_name`, ":upcase\n"},
		{`class C; def foo; end; alias_method :bar, :foo; end; p C.new.method(:bar).original_name`, ":foo\n"},
		{`class C; def foo; end; alias_method :bar, :foo; end; p C.instance_method(:bar).original_name`, ":foo\n"},
		// A native body transplanted under a new name keeps its source name.
		{`class C; define_method(:mine, ::Kernel.instance_method(:is_a?)); end; p C.new.method(:mine).original_name`, ":is_a?\n"},
		// A block-defined method's original name is the name it was defined under.
		{`class C; define_method(:f){}; end; p C.new.method(:f).original_name`, ":f\n"},
		// An instance-method transplant keeps the iseq's original name.
		{`class S; def orig; end; end; class C; define_method(:copy, S.instance_method(:orig)); end; p C.new.method(:copy).original_name`, ":orig\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMethodSuperMethod covers Method/UnboundMethod#super_method: the definition a
// `super` would reach up the receiver's ancestry (including an extended module),
// nil at the top of the chain, nil after the parent is undef'd, and nil for a
// class method whose owner is off the instance ancestor chain.
func TestMethodSuperMethod(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class A; def x; end; end; class B < A; def x; end; end; p B.new.method(:x).super_method.owner`, "A\n"},
		{`class A; def x; end; end; p A.new.method(:x).super_method`, "nil\n"},
		// Extended module: super reaches the class implementation.
		{`module M; def x; :m; end; end; class A; def x; :a; end; end; o=A.new; o.extend(M); p o.method(:x).super_method.owner`, "A\n"},
		{`class A; def x; end; end; class B < A; def x; end; end; p B.new.method(:x).super_method.receiver.class`, "B\n"},
		// UnboundMethod#super_method.
		{`class A; def x; end; end; class B < A; def x; end; end; p B.instance_method(:x).super_method == A.instance_method(:x)`, "true\n"},
		{`class A; def x; end; end; p A.instance_method(:x).super_method`, "nil\n"},
		// nil after the parent method is removed.
		{`k=Class.new{def m;end}; s=Class.new(k){def m;end}; o=s.new; meth=o.method(:m); k.class_eval{undef :m}; p meth.super_method`, "nil\n"},
		// A class method's owner is not on the instance ancestor chain → nil.
		{`class C; def self.cm; end; end; p C.method(:cm).super_method`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMethodToSInspect covers the #<Method: …> / #<UnboundMethod: …> rendering:
// the owner shown in parentheses only when it differs from the receiver class,
// the original name annotated for an alias, the per-object singleton form, the
// native "(*)" signature, and that #inspect is the very same method as #to_s.
func TestMethodToSInspect(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class C; def f(a, b=1, *c, &blk); end; end; p C.new.method(:f).to_s`, "\"#<Method: C#f(a, b=..., *c, &blk)>\"\n"},
		{`module M; def bar; end; end; class S; include M; end; p S.new.method(:bar).inspect`, "\"#<Method: S(M)#bar()>\"\n"},
		{`module M; def bar; end; end; class S; include M; end; p S.instance_method(:bar).inspect`, "\"#<UnboundMethod: M#bar()>\"\n"},
		{`class K; def orig; end; alias_method :ren, :orig; end; p K.new.method(:ren).inspect`, "\"#<Method: K#ren(orig)()>\"\n"},
		{`class C; def f(a:, b: 2, **r); end; end; p C.new.method(:f).inspect`, "\"#<Method: C#f(a:, b: ..., **r)>\"\n"},
		{`class C; def f(*, **, &); end; end; p C.new.method(:f).inspect`, "\"#<Method: C#f(*, **, &)>\"\n"},
		{`p "x".method(:upcase).inspect`, "\"#<Method: String#upcase(*)>\"\n"},
		// #inspect is an alias of #to_s (one shared record) for both classes.
		{`p(Method.instance_method(:inspect) == Method.instance_method(:to_s))`, "true\n"},
		{`p(UnboundMethod.instance_method(:inspect) == UnboundMethod.instance_method(:to_s))`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// A per-object singleton method renders as <receiver-inspect>.name.
	if got := eval(t, `o=Object.new; def o.sm; end; p o.method(:sm).inspect`); !strings.Contains(got, ">.sm()>") {
		t.Errorf("singleton inspect got %q", got)
	}
	// A method loaded from a file carries a location suffix.
	got, err := runScript(t, "class C; def f; end; end\nputs C.new.method(:f).inspect", "/scripts/main.rb")
	if err != nil || got != "#<Method: C#f() /scripts/main.rb:0>\n" {
		t.Errorf("located inspect got=%q err=%v", got, err)
	}
}

// TestPublicAndSingletonMethod covers Object#public_method (a Method for a public
// target, NameError for a private/protected/missing one) and Object#singleton_method
// (only singleton methods resolve; instance-only or missing names raise NameError).
func TestPublicAndSingletonMethod(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "abc".public_method(:upcase).call`, "\"ABC\"\n"},
		{`o=Object.new; def o.s; 42; end; p o.singleton_method(:s).call`, "42\n"},
		{`o=Object.new; def o.s; end; p o.singleton_method(:s).class`, "Method\n"},
		// A class/module's own class method resolves as a singleton method.
		{`class C; def self.cm; 7; end; end; p C.singleton_method(:cm).call`, "7\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	errs := []struct{ src, want string }{
		{`class C; private def sec; end; end; C.new.public_method(:sec)`, "NameError"},
		{`class C; protected def pro; end; end; C.new.public_method(:pro)`, "NameError"},
		{`Object.new.public_method(:nope)`, "NameError"},
		// singleton_method never returns an ordinary instance method.
		{`class C; def foo; end; end; C.new.singleton_method(:foo)`, "NameError"},
		// missing on an object that has a singleton class.
		{`o=Object.new; def o.a; end; o.singleton_method(:b)`, "NameError"},
		// missing on a value with no singleton at all.
		{`Object.new.singleton_method(:nope)`, "NameError"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want %q", c.src, err, c.want)
		}
	}
}
