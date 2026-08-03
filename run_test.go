package ruby_test

import (
	"bytes"
	"strings"
	"testing"

	ruby "github.com/go-embedded-ruby/ruby"
)

func TestRun(t *testing.T) {
	// Success: output is written, no error.
	var out bytes.Buffer
	if err := ruby.Run("p [1, 2, 3].sum", &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := out.String(); got != "6\n" {
		t.Fatalf("output = %q, want %q", got, "6\n")
	}

	// Parse error.
	if err := ruby.Run("p (", &bytes.Buffer{}); err == nil {
		t.Fatal("expected a parse error")
	}

	// Runtime error surfaces.
	err := ruby.Run("raise 'boom'", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("runtime error = %v, want one mentioning boom", err)
	}
}

// TestRunMultilineTernary exercises a ternary whose `?`/`:` and arms are split
// across lines (parser >= v0.1.3). The newline before the `:` used to fail to
// parse; each form below now parses and evaluates, matching MRI.
func TestRunMultilineTernary(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"p (true ?\n 1 :\n 2)", "1\n"},
		{"p (false ?\n \"a\"\n: \"b\")", "\"b\"\n"},
	} {
		var out bytes.Buffer
		if err := ruby.Run(tc.src, &out); err != nil {
			t.Fatalf("Run(%q): %v", tc.src, err)
		}
		if got := out.String(); got != tc.want {
			t.Errorf("Run(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}
