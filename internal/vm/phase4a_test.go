package vm_test

import (
	"strings"
	"testing"
)

const reflClass = "class Foo\n  def greet(n)\n    \"hi #{n}\"\n  end\nend\n"

func TestReflectionMethods(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"send_args", reflClass + `p Foo.new.send(:greet, "bob")`, "\"hi bob\"\n"},
		{"send_builtin", `p "hello".send(:upcase)`, "\"HELLO\"\n"},
		{"send_block", `p [1, 2, 3].send(:map) { |x| x * 10 }`, "[10, 20, 30]\n"},
		{"public_send", reflClass + `p Foo.new.public_send(:greet, "al")`, "\"hi al\"\n"},
		{"respond_to_true", reflClass + `p Foo.new.respond_to?(:greet)`, "true\n"},
		{"respond_to_false", reflClass + `p Foo.new.respond_to?(:nope)`, "false\n"},
		{"itself", `p 5.itself`, "5\n"},
		{"tap", "r = []\nv = [1, 2, 3].tap { |a| r << a.length }\np v\np r", "[1, 2, 3]\n[3]\n"},
		{"then", `p 5.then { |n| n * 2 }`, "10\n"},
		{"yield_self", `p "ab".yield_self { |s| s.upcase }`, "\"AB\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestReflectionErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"tap_no_block", `5.tap`, "LocalJumpError"},
		{"then_no_block", `5.then`, "LocalJumpError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %s", tc.src, err, tc.want)
			}
		})
	}
}
