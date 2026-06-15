package vm_test

import (
	"strings"
	"testing"
)

func TestClassMethods(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"simple", "class Foo\ndef self.greet\n\"hi\"\nend\nend\np Foo.greet", "\"hi\"\n"},
		{"with_args", "class Foo\ndef self.add(a, b)\na + b\nend\nend\np Foo.add(2, 3)", "5\n"},
		{"factory", "class C\ndef self.create(n)\nc = new\nc.set(n)\nc\nend\ndef set(n)\n@n = n\nself\nend\ndef value\n@n\nend\nend\np C.create(42).value", "42\n"},
		{"inherited", "class Base\ndef self.kind\n\"base\"\nend\nend\nclass Derived < Base\nend\np Derived.kind", "\"base\"\n"},
		{"instance_still_works", "class Foo\ndef self.cm\n1\nend\ndef im\n2\nend\nend\np Foo.cm + Foo.new.im", "3\n"},
		{"overrides_inherited", "class Base\ndef self.kind\n\"base\"\nend\nend\nclass Derived < Base\ndef self.kind\n\"derived\"\nend\nend\np Derived.kind", "\"derived\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestUndefinedClassMethod(t *testing.T) {
	src := "class Foo\nend\nFoo.nope"
	if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "NoMethodError") {
		t.Fatalf("got %v want NoMethodError", err)
	}
}
