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
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
		}
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
