package vm_test

import (
	"strings"
	"testing"
)

func TestModulesIncludeSuper(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"mixin_calls_instance_method",
			"module Greet\n  def hi\n    \"hi \" + name\n  end\nend\n" +
				"class Person\n  include Greet\n  def initialize(n)\n    @n = n\n  end\n  def name\n    @n\n  end\nend\n" +
				"puts Person.new(\"Bob\").hi", "hi Bob\n"},
		{"module_reopen",
			"module M\n  def a\n    1\n  end\nend\nmodule M\n  def b\n    2\n  end\nend\n" +
				"class C\n  include M\nend\nx = C.new\nputs x.a + x.b", "3\n"},
		{"super_bare_forwards_args",
			"class A\n  def g(x)\n    \"A\" + x\n  end\nend\n" +
				"class D < A\n  def g(x)\n    super + \"D\"\n  end\nend\nputs D.new.g(\"-\")", "A-D\n"},
		{"super_explicit_args",
			"class A\n  def g(x)\n    \"A\" + x\n  end\nend\n" +
				"class B < A\n  def g(x)\n    super(x) + \"B\"\n  end\nend\nputs B.new.g(\"-\")", "A-B\n"},
		{"super_command_form",
			"class A\n  def g(x)\n    \"A\" + x\n  end\nend\n" +
				"class B < A\n  def g(x)\n    super \"Z\"\n  end\nend\nputs B.new.g(\"-\")", "AZ\n"},
		{"super_explicit_empty",
			"class A\n  def g\n    \"A\"\n  end\nend\n" +
				"class B < A\n  def g\n    super() + \"B\"\n  end\nend\nputs B.new.g", "AB\n"},
		{"inherited_through_module_chain",
			"module M\n  def m\n    \"M\"\n  end\nend\nclass A\n  include M\nend\nclass B < A\nend\nputs B.new.m", "M\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestSuperErrors(t *testing.T) {
	tests := []struct{ src, want string }{
		{`super`, "super called outside of method"},
		{"class A\nend\nclass B < A\n  def g\n    super\n  end\nend\nB.new.g", "no superclass method"},
	}
	for _, tc := range tests {
		if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("src=%q got %v want %q", tc.src, err, tc.want)
		}
	}
}
