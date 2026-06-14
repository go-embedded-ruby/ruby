package vm_test

import (
	"strings"
	"testing"
)

func TestObjectModel(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"class_ivar_method",
			"class C\n  def initialize(x)\n    @x = x\n  end\n  def get\n    @x\n  end\nend\nputs C.new(7).get", "7\n"},
		{"inheritance_override",
			"class A\n  def speak\n    \"a\"\n  end\nend\nclass B < A\n  def speak\n    \"b\"\n  end\nend\nputs B.new.speak", "b\n"},
		{"inherited_method",
			"class A\n  def hi\n    \"hi\"\n  end\nend\nclass B < A\nend\nputs B.new.hi", "hi\n"},
		{"default_initialize",
			"class E\nend\np E.new.nil?", "false\n"},
		{"reopen_class",
			"class A\n  def a\n    1\n  end\nend\nclass A\n  def b\n    2\n  end\nend\nx = A.new\nputs x.a + x.b", "3\n"},
		{"ivar_unset_is_nil",
			"class Z\n  def g\n    @missing\n  end\nend\np Z.new.g", "nil\n"},
		{"ivar_assign_returns_value",
			"class R\n  def s\n    @v = 9\n  end\nend\nputs R.new.s", "9\n"},
		{"method_missing_override",
			"class G\n  def method_missing(n)\n    \"got \" + n\n  end\nend\nputs G.new.zzz", "got zzz\n"},
		{"const_ref_to_class",
			"class K\nend\nputs K", "K\n"},
		// Top-level @ivars live on `main`, which has no ivar storage in Phase 1:
		// the set is a no-op and the read is nil. (Documented quirk.)
		{"toplevel_ivar_quirk", "@t = 5\np @t", "nil\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestClassOfNames(t *testing.T) {
	cases := []struct{ expr, want string }{
		{"1", "Integer"},
		{"1.0", "Float"},
		{`"s"`, "String"},
		{"true", "TrueClass"},
		{"false", "FalseClass"},
		{"nil", "NilClass"},
		{"self", "Object"},          // top-level self is main (an Object)
		{"Integer", "Class"},        // the class of a class is Class
	}
	for _, c := range cases {
		src := "puts (" + c.expr + ").class"
		if got := eval(t, src); got != c.want+"\n" {
			t.Errorf("%s.class = %q want %q", c.expr, got, c.want)
		}
	}
}

func TestKernelMethods(t *testing.T) {
	if got := eval(t, `puts 5.to_s`); got != "5\n" {
		t.Errorf("to_s: %q", got)
	}
	if got := eval(t, `puts "x".inspect`); got != "\"x\"\n" {
		t.Errorf("inspect: %q", got)
	}
	if got := eval(t, `puts nil.nil?`); got != "true\n" {
		t.Errorf("nil?: %q", got)
	}
	if got := eval(t, `puts 1.nil?`); got != "false\n" {
		t.Errorf("1.nil?: %q", got)
	}
}

func TestValueReprAndTruthy(t *testing.T) {
	cases := []struct{ src, want string }{
		{"class E\nend\nputs E.new", "#<E>\n"},   // RObject.ToS
		{"class E\nend\np E.new", "#<E>\n"},       // RObject.Inspect
		{"class E\nend\nputs(!E.new)", "false\n"}, // RObject.Truthy (objects are truthy)
		{"class K\nend\np K", "K\n"},              // RClass.Inspect
		{"class K\nend\nputs(!K)", "false\n"},     // RClass.Truthy
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

func TestObjectModelErrors(t *testing.T) {
	tests := []struct{ src, want string }{
		{`puts Nope`, "NameError"},               // uninitialized constant
		{"class X < Nope\nend", "NameError"},     // unknown superclass
		{`1.frobnicate`, "NoMethodError"},        // method_missing default
	}
	for _, tc := range tests {
		if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("src=%q got %v want %s", tc.src, err, tc.want)
		}
	}
}
