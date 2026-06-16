package vm_test

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/ast"
	"github.com/go-embedded-ruby/ruby/internal/parser"
)

// Covers grammar/compile corners not reached by the main behavioural table.
func TestGrammarCorners(t *testing.T) {
	tests := []struct{ src, want string }{
		{`puts(+5)`, "5\n"},                                   // unary plus
		{"i = 0\ni = i + 1 until i >= 3\nputs i", "3\n"},      // until modifier
		{"def add a, b\n  a + b\nend\nputs add 1, 2", "3\n"},  // paren-less params + command call
		{"def nine()\n  9\nend\nputs nine()", "9\n"},          // empty parens both sides
		{"def empty?\n  true\nend\nputs empty?", "true\n"},    // ? in method name
		{"def go!\n  1\nend\nputs go!", "1\n"},                // ! in method name
		{"def h\n  return\nend\np h", "nil\n"},                // bare return
		{"x = 5\ndef f\n  3\nend\nputs f\nputs x", "3\n5\n"},  // def is a hard scope boundary
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

func TestStringEscapes(t *testing.T) {
	// \r \e \0 and an unknown escape (\q -> q).
	out := eval(t, `print "A\rB\eC\0D\qE"`)
	want := "A\rB\x1bC\x00DqE"
	if out != want {
		t.Errorf("escapes: got %q want %q", out, want)
	}
	// \n \t \\ \" — the remaining escape arms.
	out = eval(t, `print "a\nb\tc\\d\"e"`)
	want = "a\nb\tc\\d\"e"
	if out != want {
		t.Errorf("escapes2: got %q want %q", out, want)
	}
}

// Parser-only corners (these need not run): a Bignum literal, the LPAREN
// command-arg arm, and the "space then non-argument token" path of
// canStartCommandArg.
func TestParserCorners(t *testing.T) {
	prog, err := parser.Parse(`99999999999999999999999`)
	if err != nil {
		t.Errorf("unexpected error parsing bignum literal: %v", err)
	} else if _, ok := prog.Body[0].(*ast.BignumLit); !ok {
		t.Errorf("expected *ast.BignumLit, got %T", prog.Body[0])
	}
	if _, err := parser.Parse("foo * 2"); err != nil { // command arg declined → foo() * 2
		t.Errorf("unexpected error parsing command/operator ambiguity: %v", err)
	}
	if got := eval(t, `puts (1)`); got != "1\n" { // LPAREN starts a command argument
		t.Errorf("puts (1) = %q", got)
	}
}

// Sending an unknown selector to a built-in value routes to method_missing,
// whose default raises NoMethodError.
func TestUnknownMethod(t *testing.T) {
	for _, src := range []string{`1.foo`, `1.bar(2)`, "x = \"s\"\nx.nope"} {
		err := runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "NoMethodError") {
			t.Errorf("src=%q: got %v, want NoMethodError", src, err)
		}
	}
}

// The error types expose a usable message.
func TestErrorMessages(t *testing.T) {
	_, err := parser.Parse(`if`)
	if err == nil || err.Error() == "" {
		t.Fatal("expected non-empty parse error")
	}
	if e := runErr(t, `1.foo`); e == nil || e.Error() != "NoMethodError: undefined method 'foo' for Integer" {
		t.Fatalf("runtime error message = %v", e)
	}
}
