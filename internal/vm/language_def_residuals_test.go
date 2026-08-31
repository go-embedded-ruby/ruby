package vm_test

import (
	"strings"
	"testing"
)

// TestLanguageDefResiduals covers language-def/method/super/keyword-argument
// residuals fixed together: post-*splat positional binding, implicit `super`
// (zsuper) re-reading the CURRENT parameter locals, and the missing-before-
// unknown keyword-error ordering. All expectations verified against MRI 4.0.
func TestLanguageDefResiduals(t *testing.T) {
	cases := []struct{ src, want string }{
		// --- post-splat positional binding (def m(a,*b,c)) ---
		{`def m(a,*b,c); [a,b,c]; end; p m(1,2,3,4)`, "[1, [2, 3], 4]\n"},
		{`def m(a,*b,c); [a,b,c]; end; p m(1,2)`, "[1, [], 2]\n"},
		// two post params, empty middle
		{`def m(a,*b,c,d); [a,b,c,d]; end; p m(1,2,3)`, "[1, [], 2, 3]\n"},
		// leading optional + splat still works (no post)
		{`def m(a,b=9,*c); [a,b,c]; end; p m(1)`, "[1, 9, []]\n"},

		// --- zsuper: forwards CURRENT parameter values ---
		// optional param with a default is passed to the parent
		{`class A; def m(a,b,c); [a,b,c]; end; end
		  class B < A; def m(a,b,c=30); super; end; end
		  p B.new.m(1,2)`, "[1, 2, 30]\n"},
		// a reassigned *rest is forwarded with modifications
		{`class F; def m(*a); a; end; end
		  class G < F; def m(*a); a[0]=99; super; end; end
		  p G.new.m(1,2)`, "[99, 2]\n"},
		// keyword defaults are forwarded even when not originally passed
		{`class D; def m(a:1,b:2); [a,b]; end; end
		  class E < D; def m(a:10,b:20); super; end; end
		  p E.new.m(a:5)`, "[5, 20]\n"},
		// post-splat params forwarded in order by zsuper
		{`class A; def m(a,*b,c); [a,b,c]; end; end
		  class B < A; def m(a,*b,c); super; end; end
		  p B.new.m(1,2,3,4)`, "[1, [2, 3], 4]\n"},
		// a plain unmodified super is unchanged (regression)
		{`class A; def m(a,b); [a,b]; end; end
		  class B < A; def m(a,b); super; end; end
		  p B.new.m(7,8)`, "[7, 8]\n"},
		// **kwrest forwarded by zsuper
		{`class A; def m(**k); k; end; end
		  class B < A; def m(**k); super; end; end
		  p B.new.m(x:1,y:2)`, "{x: 1, y: 2}\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
		}
	}
}

// TestAlwaysPrivateMethodDefs covers the core hook methods MRI always defines
// with private visibility regardless of the surrounding default.
func TestAlwaysPrivateMethodDefs(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class C; def initialize; end; def initialize_copy(o); end
		  def initialize_dup(o); end; def initialize_clone(o); end
		  def respond_to_missing?(a,b); end; def normal; end; end
		  p C.private_instance_methods(false).sort`,
			"[:initialize, :initialize_clone, :initialize_copy, :initialize_dup, :respond_to_missing?]\n"},
		// a normal method is still public
		{`class C; def initialize; end; def normal; end; end
		  p C.public_instance_methods(false).include?(:normal)`, "true\n"},
		// reopening a class with a normal def stays public
		{`class C; end; class C; def foo; end; end
		  p C.public_instance_methods(false).include?(:foo)`, "true\n"},
		// a def executed INSIDE a method body is always public, even when the
		// class default visibility was set to private in the class body
		{`class C; private; def do_def; def new_def; 1; end; end; end
		  o = C.new; o.send(:do_def)
		  p C.public_instance_methods(false).include?(:new_def)`, "true\n"},
		// the private default still governs the directly-written method
		{`class C; private; def do_def; def new_def; 1; end; end; end
		  p C.private_instance_methods(false).include?(:do_def)`, "true\n"},
		// a def inside a block at class-body level DOES follow the class default
		{`class D; private; [1].each { def blk_def; 2; end }; end
		  p D.private_instance_methods(false).include?(:blk_def)`, "true\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
		}
	}
}

// TestSplatToArrayNilToA covers the splat (*obj) coercion when the object's
// #to_a returns nil: MRI wraps the object in a one-element array rather than
// raising, while a non-nil non-Array #to_a result is still a TypeError.
func TestSplatToArrayNilToA(t *testing.T) {
	if got := eval(t, `class M; def to_a; nil; end; end
	  a = [*M.new]; p a.size; p a[0].class`); got != "1\nM\n" {
		t.Errorf("nil to_a splat: got %q", got)
	}
	err := runErr(t, `class N; def to_a; 5; end; end; [*N.new]`)
	if err == nil || !strings.Contains(err.Error(), "can't convert N to Array (N#to_a gives Integer)") {
		t.Errorf("non-array to_a: got %v", err)
	}
}

// TestSuperFallsBackToMethodMissing covers a `super` that finds no callable
// superclass method: it must not invoke an `undef` tombstone (which has a nil
// body and crashed), and it routes to a user-defined method_missing while a
// plain failure still raises the super-specific NoMethodError.
func TestSuperFallsBackToMethodMissing(t *testing.T) {
	// undef'd ancestor method + user method_missing: super reaches method_missing.
	got := eval(t, `class A; undef_method :is_a?; end
	  class B < A
	    def is_a?(x); super; end
	    def method_missing(*a); "mm:#{a[0]}"; end
	  end
	  p B.new.is_a?(Hash)`)
	if got != "\"mm:is_a?\"\n" {
		t.Errorf("super->method_missing: got %q", got)
	}
	// no super method and only the default method_missing: super-specific error.
	err := runErr(t, `class A; def f; super; end; end; A.new.f`)
	if err == nil || !strings.Contains(err.Error(), "super: no superclass method 'f'") {
		t.Errorf("plain super fail: got %v", err)
	}
}

// TestKeywordAndArityErrors covers the keyword/arity error paths: a missing
// required keyword is reported before an unknown one, and the post-splat arity
// minimum is enforced.
func TestKeywordAndArityErrors(t *testing.T) {
	cases := []struct{ src, wantMsg string }{
		// missing required keyword takes precedence over an unknown one
		{`def m(a:); a; end; m(b: 1)`, "missing keyword: :a"},
		{`def m(a:, kw:); [a,kw]; end; m(x: 1)`, "missing keywords: :a, :kw"},
		// unknown keyword still raised when nothing is missing
		{`def m(a:); a; end; m(a: 1, b: 2)`, "unknown keyword: :b"},
		// post-splat arity minimum: def m(a,*b,c) needs at least 2 args
		{`def m(a,*b,c); [a,b,c]; end; m(1)`, "given 1, expected 2+"},
	}
	for _, tc := range cases {
		err := runErr(t, tc.src)
		if err == nil {
			t.Errorf("src=%q: expected error %q, got nil", tc.src, tc.wantMsg)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantMsg) {
			t.Errorf("src=%q\n got=%q\nwant substring %q", tc.src, err.Error(), tc.wantMsg)
		}
	}
}
