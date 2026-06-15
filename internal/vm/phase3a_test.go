package vm_test

import (
	"strings"
	"testing"
)

func TestTypeIntrospection(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"is_a_exact", `p 5.is_a?(Integer)`, "true\n"},
		{"is_a_super", `p 5.is_a?(Object)`, "true\n"},
		{"is_a_false", `p 5.is_a?(String)`, "false\n"},
		{"is_a_module", `p "x".is_a?(Comparable)`, "true\n"},
		{"is_a_array_enum", `p [].is_a?(Enumerable)`, "true\n"},
		{"kind_of", `p 5.kind_of?(Integer)`, "true\n"},
		{"instance_of_true", `p 5.instance_of?(Integer)`, "true\n"},
		{"instance_of_false", `p 5.instance_of?(Object)`, "false\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestExceptionObjects(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"message", `p RuntimeError.new("boom").message`, "\"boom\"\n"},
		{"to_s", `p StandardError.new("oops").to_s`, "\"oops\"\n"},
		{"message_default", `p TypeError.new.message`, "\"TypeError\"\n"},
		{"class", `p StandardError.new("x").class`, "StandardError\n"},
		{"hierarchy_runtime", `p RuntimeError.new("x").is_a?(StandardError)`, "true\n"},
		{"hierarchy_exception", `p ArgumentError.new("x").is_a?(Exception)`, "true\n"},
		{"hierarchy_nomethod", `p NoMethodError.new.is_a?(NameError)`, "true\n"},
		{"hierarchy_keyerror", `p KeyError.new.is_a?(IndexError)`, "true\n"},
		{"not_a", `p TypeError.new.is_a?(ArgumentError)`, "false\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestRaise(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"string", `raise "boom"`, "RuntimeError: boom"},
		{"class", `raise TypeError`, "TypeError: TypeError"},
		{"class_message", `raise ArgumentError, "bad arg"`, "ArgumentError: bad arg"},
		{"instance", "e = RuntimeError.new(\"oops\")\nraise e", "RuntimeError: oops"},
		{"bare", `raise`, "RuntimeError: unhandled exception"},
		{"non_exception", `raise 5`, "TypeError"},
		{"is_a_bad_arg", `5.is_a?(3)`, "TypeError"},
		{"instance_of_bad_arg", `5.instance_of?("x")`, "TypeError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want containing %q", tc.src, err, tc.want)
			}
		})
	}
}
