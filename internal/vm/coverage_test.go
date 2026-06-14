package vm_test

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
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
}

// Compile-time errors (receiver method calls are out of Phase 0 scope).
func TestCompileErrors(t *testing.T) {
	for _, src := range []string{`1.foo`, `1.foo(2)`, `x = 1; x.bar(3)`} {
		err := runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "Phase 0") {
			t.Errorf("src=%q: got %v, want a Phase 0 compile error", src, err)
		}
	}
}

// The error types expose a usable message.
func TestErrorMessages(t *testing.T) {
	_, err := parser.Parse(`if`)
	if err == nil || err.Error() == "" {
		t.Fatal("expected non-empty parse error")
	}
	prog, perr := parser.Parse(`1.foo`)
	if perr != nil {
		t.Fatalf("unexpected parse error: %v", perr)
	}
	_, cerr := compiler.Compile(prog)
	if cerr == nil || !strings.Contains(cerr.Error(), "compile error") {
		t.Fatalf("expected compile error, got %v", cerr)
	}
}
