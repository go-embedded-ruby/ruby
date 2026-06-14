package vm_test

import (
	"strings"
	"testing"
)

// Exercises the float/string operator branches and the p/print builtins that
// the core Phase 0 table does not reach.
func TestNumericAndStringOps(t *testing.T) {
	tests := []struct{ src, want string }{
		{`puts 5.0 - 1.5`, "3.5\n"},
		{`puts 2.0 * 3.0`, "6.0\n"},
		{`puts 7.5 % 2.0`, "1.5\n"},
		{`puts(1.5 < 2.0)`, "true\n"},
		{`puts(2.5 > 2.0)`, "true\n"},
		{`puts(2.0 <= 2.0)`, "true\n"},
		{`puts(2.0 >= 3.0)`, "false\n"},
		{`puts(-(2.0))`, "-2.0\n"},
		{`puts 1.0 / 0.0`, "Infinity\n"},
		{`puts(2 != 3)`, "true\n"},
		{`puts(nil == nil)`, "true\n"},
		{`puts(true == false)`, "false\n"},
		{`puts("a" == "a")`, "true\n"},
		{`puts("a" < "b")`, "true\n"},
		{`puts("b" > "a")`, "true\n"},
		{`puts("a" <= "a")`, "true\n"},
		{`puts("b" >= "a")`, "true\n"},
		{`print "a", "b"`, "ab"},
		{`p 1, 2`, "1\n2\n"},
		{`p()`, ""},
		{`puts`, "\n"},
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

func TestMoreRuntimeErrors(t *testing.T) {
	tests := []struct{ src, want string }{
		{`puts "a" + 1`, "TypeError"},
		{`puts "a" * "b"`, "TypeError"},
		{`puts "a" * (-1)`, "ArgumentError"},
		{`puts("a" < 1)`, "ArgumentError"},
		{`puts(-nil)`, "NoMethodError"},
		{`puts(true + 1)`, "TypeError"},
	}
	for _, tc := range tests {
		err := runErr(t, tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("src=%q got err=%v want %q", tc.src, err, tc.want)
		}
	}
}

func TestLexerEdgeCases(t *testing.T) {
	// underscores in numbers and a line continuation across a binary expression.
	out := eval(t, "x = 1_000\nputs x + \\\n  1")
	if out != "1001\n" {
		t.Errorf("underscore/line-continuation failed: %q", out)
	}
	// string escapes: \t \n \\ \" \r \e \0 and an unknown escape (passes char through).
	out = eval(t, `puts "a\tb"`)
	if out != "a\tb\n" {
		t.Errorf("escape failed: %q", out)
	}
}
