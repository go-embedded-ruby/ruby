package vm_test

import (
	"strings"
	"testing"
)

func TestOneLinePatterns(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"rightward_hash", "{name: \"Alice\", age: 30} => {name:, age:}\np name\np age", "\"Alice\"\n30\n"},
		{"rightward_array", "[1, 2, 3] => [a, *rest]\np a\np rest", "1\n[2, 3]\n"},
		{"in_true", "r = (5 in Integer)\np r", "true\n"},
		{"in_false", "r = (\"x\" in Integer)\np r", "false\n"},
		{"in_binds", "[1, 2] in [x, y]\np [x, y]", "[1, 2]\n"},
		{"in_array_splat", "r = ([1, 2, 3] in [1, *rest])\np r\np rest", "true\n[2, 3]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestRightwardAssignNoMatch(t *testing.T) {
	if err := runErr(t, "[1, 2] => [a, b, c]"); err == nil || !strings.Contains(err.Error(), "NoMatchingPatternError") {
		t.Fatalf("got %v want NoMatchingPatternError", err)
	}
}
