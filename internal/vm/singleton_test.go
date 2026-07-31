package vm_test

import (
	"strings"
	"testing"
)

// TestDefineSingletonMethod covers define_singleton_method on objects and on
// classes, the block and explicit-Proc forms, and access to the receiver's own
// methods/ivars — asserted against MRI Ruby 4.0.5.
func TestDefineSingletonMethod(t *testing.T) {
	cases := []struct{ src, want string }{
		{"o = Object.new\no.define_singleton_method(:x) { 42 }\np o.x", "42\n"},
		{"o = Object.new\no.define_singleton_method(:add) { |a, b| a + b }\np o.add(3, 4)", "7\n"},
		{"o = Object.new\no.define_singleton_method(:y, proc { 9 })\np o.y", "9\n"},
		{"class C; def self.m; end; end\nC.define_singleton_method(:cm) { 99 }\np C.cm", "99\n"},
		// a singleton method sees the receiver's instance methods / ivars.
		{"class C\n  def initialize; @v = 7; end\n  def r; @v; end\nend\nc = C.new\nc.define_singleton_method(:double) { r * 2 }\np c.double", "14\n"},
		{"o = Object.new\np o.define_singleton_method(:z) { 1 }", ":z\n"}, // returns the symbol
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestExtend covers Object#extend (and Class#extend), plus the extended hook.
func TestExtend(t *testing.T) {
	cases := []struct{ src, want string }{
		{"module M; def hi; \"hi\"; end; end\no = Object.new\no.extend(M)\np o.hi", "\"hi\"\n"},
		{"module M; def util; :u; end; end\nclass C; extend M; end\np C.util", ":u\n"},
		{"module M; def self.extended(o); puts \"ext #{o.class}\"; end; def hi; 1; end; end\nObject.new.extend(M)", "ext Object\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSingletonClass covers `class << target; body; end` — the singleton/meta
// class opener. Bodies attach methods/constants to the target's singleton class:
// `class << self` in a class body defines class methods; `class << obj` defines
// per-object methods. Asserted against MRI.
func TestSingletonClass(t *testing.T) {
	cases := []struct{ src, want string }{
		// class << self in a class body → class methods.
		{"class C; class << self; def x; 42; end; end; end\np C.x", "42\n"},
		// class << obj → per-object methods.
		{"o = Object.new\nclass << o; def y; 7; end; end\np o.y", "7\n"},
		// constants and instance methods coexist with the singleton body.
		{"class C; X = 1; class << self; def x; 9; end; end; def inst; 3; end; end\np [C.x, C.new.inst]", "[9, 3]\n"},
		// a class method defined in `class << self` can call a sibling class method.
		{"class C; class << self; def a; b + 1; end; def b; 10; end; end; end\np C.a", "11\n"},
		// a constant defined in `class << obj` does not break the body.
		{"o = Object.new\nclass << o; def y; 7; end; CONST = 1; end\np o.y", "7\n"},
		// attr_accessor inside `class << self` reads/writes the class's own ivar.
		{"class C; @v = 5; class << self; attr_accessor :v; end; end\np C.v\nC.v = 6\np C.v", "5\n6\n"},
		// a class-level ivar set in the body is visible to a class method.
		{"class C; @items = []; def self.add(x); @items << x; end; def self.items; @items; end; end\nC.add(1)\nC.add(2)\np C.items", "[1, 2]\n"},
		// reopening the same singleton class adds more methods.
		{"o = Object.new\nclass << o; def a; 1; end; end\nclass << o; def b; 2; end; end\np [o.a, o.b]", "[1, 2]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSingletonClassOfExpression covers the `class << <expr>` form as an
// expression (gap G5): the singleton-class body is a real scope whose value is
// its last expression, so `(class << x; self; end)` evaluates to x's singleton
// class. Asserted against MRI Ruby 4.0.5 (identity via Object#equal?, so the
// object returned is the very same singleton class Object#singleton_class hands
// back — the pattern `rspec/support.rb` relies on).
func TestSingletonClassOfExpression(t *testing.T) {
	cases := []struct{ src, want string }{
		// `class << obj; self; end` IS obj's singleton class (same identity).
		{"o = Object.new\np o.singleton_class.equal?(class << o; self; end)", "true\n"},
		// same for a class receiver: `class << C; self; end` == C.singleton_class.
		{"class C; end\np C.singleton_class.equal?(class << C; self; end)", "true\n"},
		// the receiver may be an arbitrary expression, not just self / a local.
		{"p((class << Object.new; self; end).class)", "Class\n"},
		{"p((class << [1, 2]; self; end).is_a?(Class))", "true\n"},
		// the body is a real scope; its value is the last expression.
		{"x = (class << Object.new; 40; 2; end)\np x", "2\n"},
		// module class-level accessor via `class << self` (shared-examples shape).
		{"module M; class << self; def reg; \"R\"; end; end; end\nputs M.reg", "R\n"},
		// def on the singleton returned by `class << obj; self; end` targets obj.
		{"o = Object.new\nsc = (class << o; self; end)\nsc.send(:define_method, :hi) { 5 }\np o.hi", "5\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSingletonClassErrors covers `class << target` for a target with no
// singleton class (an immediate value), which MRI rejects with a TypeError.
func TestSingletonClassErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"x = 5\nclass << x; def z; 1; end; end", "can't define singleton"},
		{"class << :sym; def z; 1; end; end", "can't define singleton"},
		{"class << nil; def z; 1; end; end", "can't define singleton"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestSingletonErrors covers the raising paths of both methods.
func TestSingletonErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Object.new.define_singleton_method(:x)`, "ArgumentError"}, // no block
		{`Object.new.define_singleton_method(:x, 5)`, "TypeError"},  // non-Proc body
		{`5.define_singleton_method(:x) { 1 }`, "TypeError"},        // not an object/class
		{`Object.new.extend(5)`, "TypeError"},                       // not a module
		{`5.extend(Comparable)`, "TypeError"},                       // can't extend a Fixnum
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
