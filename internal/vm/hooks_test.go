package vm_test

import "testing"

// TestClassHooks covers the metaprogramming hooks fired by the VM — inherited
// (on subclassing), included (on include), and method_added (on instance-method
// definition) — each asserted against MRI Ruby 4.0.5.
func TestClassHooks(t *testing.T) {
	cases := []struct{ src, want string }{
		// inherited(subclass): fired when a subclass is created.
		{"class A\n  def self.inherited(s); puts \"inherited #{s}\"; end\nend\nclass B < A; end", "inherited B\n"},
		// included(base): fired when the module is included.
		{"module M\n  def self.included(b); puts \"included #{b}\"; end\nend\nclass C; include M; end", "included C\n"},
		// method_added(:name): fired per instance-method def, with a Symbol.
		{"class D\n  def self.method_added(n); p n; end\n  def x; end\n  def y; end\nend", ":x\n:y\n"},
		// the hook only fires for methods defined AFTER it is installed.
		{"class E\n  def before; end\n  def self.method_added(n); puts \"after #{n}\"; end\n  def aft; end\nend", "after aft\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestHooksAbsentAreNoOps guards the false branch: classes/modules without the
// hooks behave normally (no firing, no error).
func TestHooksAbsentAreNoOps(t *testing.T) {
	cases := []struct{ src, want string }{
		{"class P; def f; 1; end; end\np P.new.f", "1\n"},
		{"module Q; def g; 2; end; end\nclass R; include Q; end\np R.new.g", "2\n"},
		{"class S; end\nclass U < S; def h; 3; end; end\np U.new.h", "3\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
