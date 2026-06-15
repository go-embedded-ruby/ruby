package vm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/parser"
	"github.com/go-embedded-ruby/ruby/internal/vm"
)

// eval runs src through the full pipeline and returns captured stdout.
func eval(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var buf bytes.Buffer
	if _, err := vm.New(&buf).Run(iseq); err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	return buf.String()
}

func TestPhase0(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"add", `puts 1 + 2`, "3\n"},
		{"precedence", `puts 2 + 3 * 4`, "14\n"},
		{"paren", `puts (2 + 3) * 4`, "20\n"},
		{"int_div_floor", `puts(-7 / 2)`, "-4\n"},
		{"int_mod", `puts(-7 % 3)`, "2\n"},
		{"float", `puts 7.0 / 2`, "3.5\n"},
		{"float_fmt", `puts 1.0`, "1.0\n"},
		{"string_concat", `puts "a" + "b"`, "ab\n"},
		{"string_repeat", `puts "ab" * 3`, "ababab\n"},
		{"unary_minus", `puts(-5)`, "-5\n"},
		{"not", `puts(!nil)`, "true\n"},
		{"comparison", `puts(3 < 5)`, "true\n"},
		{"equality", `puts(2 == 2.0)`, "true\n"},
		{"local", "x = 41\nputs x + 1", "42\n"},
		{"if_true", "if 1 < 2\n  puts \"y\"\nelse\n  puts \"n\"\nend", "y\n"},
		{"elsif", "x = 2\nif x == 1\n  puts 1\nelsif x == 2\n  puts 2\nelse\n  puts 3\nend", "2\n"},
		{"while", "i = 0\nwhile i < 3\n  puts i\n  i = i + 1\nend", "0\n1\n2\n"},
		{"def_call", "def sq(n)\n  n * n\nend\nputs sq(9)", "81\n"},
		{"command_call", `puts 1 + 2, 3 + 4`, "3\n7\n"},
		{"implicit_return", "def last\n  1\n  2\n  3\nend\nputs last", "3\n"},
		{"explicit_return", "def f(n)\n  return 0 if false\n  n\nend\nputs f(5)", "5\n"},
		{"recursion_fib", "def fib(n)\n  if n < 2\n    n\n  else\n    fib(n-1) + fib(n-2)\n  end\nend\nputs fib(10)", "55\n"},
		{"inspect_p", `p "hi"`, "\"hi\"\n"},
		{"chained_assign", "a = b = 7\nputs a + b", "14\n"},
		{"comment", "puts 1 # trailing\n# full line\nputs 2", "1\n2\n"},
		{"modifier_if", "puts 1 if 2 > 1\nputs 2 if 1 > 2", "1\n"},
		{"modifier_unless", "puts 1 unless false\nputs 2 unless true", "1\n"},
		{"modifier_while", "i = 0\ni = i + 1 while i < 3\nputs i", "3\n"},
		{"block_unless", "unless false\n  puts \"y\"\nelse\n  puts \"n\"\nend", "y\n"},
		{"block_until", "i = 0\nuntil i >= 3\n  puts i\n  i = i + 1\nend", "0\n1\n2\n"},
		{"return_modifier", "def f(n)\n  return 0 if n < 0\n  n\nend\nputs f(-3)\nputs f(3)", "0\n3\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// runErr returns the runtime error (or parse/compile error) for src.
func runErr(t *testing.T, src string) error {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		return err
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		return err
	}
	_, err = vm.New(&bytes.Buffer{}).Run(iseq)
	return err
}

func TestRuntimeErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"zero_div", `puts 1 / 0`, "ZeroDivisionError"},
		{"no_method", `frobnicate 1`, "NoMethodError"},
		{"arity", "def f(a)\n a\nend\nf 1, 2", "ArgumentError"},
		{"type_coerce", `puts 1 + "x"`, "TypeError"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	for _, src := range []string{
		`def`, `if 1`, `puts (1`, `1 +`,
		`@`,    // bare @ → ILLEGAL from lexIvar
		`$`,    // bare $ (no name) → ILLEGAL from lexGvar
		"`",    // backtick: unknown character → ILLEGAL from the main lexer switch
		`1.`,   // trailing dot: float lookahead hits EOF, then '.' has no method
		`1 2`,  // two primaries with no separator
		`module`, // module without a name
		`class`,  // class without a name
	} {
		if _, err := parser.Parse(src); err == nil {
			t.Errorf("expected parse error for %q", src)
		}
	}
}

func TestCommandAndParenArgs(t *testing.T) {
	cases := []struct{ src, want string }{
		{`puts -1`, "-1\n"},                                   // command call with unary-minus arg
		{"def s(a, b)\n  a + b\nend\nputs s(1, 2)", "3\n"},    // paren call, multiple args
		{"puts self", "main\n"},                               // self (push_self) at top level
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}
