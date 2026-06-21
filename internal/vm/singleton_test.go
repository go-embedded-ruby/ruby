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

// TestSingletonErrors covers the raising paths of both methods.
func TestSingletonErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Object.new.define_singleton_method(:x)`, "ArgumentError"},   // no block
		{`Object.new.define_singleton_method(:x, 5)`, "TypeError"},    // non-Proc body
		{`5.define_singleton_method(:x) { 1 }`, "TypeError"},          // not an object/class
		{`Object.new.extend(5)`, "TypeError"},                         // not a module
		{`5.extend(Comparable)`, "TypeError"},                         // can't extend a Fixnum
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
