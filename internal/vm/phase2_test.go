package vm_test

import (
	"strings"
	"testing"
)

func TestSymbols(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"inspect", `p :hello`, ":hello\n"},
		{"command_arg", `puts :hello`, "hello\n"},
		{"to_s", `puts :hello.to_s`, "hello\n"},
		{"class", `puts :hello.class`, "Symbol\n"},
		{"eq_true", `puts(:a == :a)`, "true\n"},
		{"eq_false", `puts(:a == :b)`, "false\n"},
		{"eq_other_type", `puts(:a == "a")`, "false\n"},
		{"to_sym", `p :a.to_sym`, ":a\n"},
		{"question_suffix", `p :empty?`, ":empty?\n"},
		{"bang_suffix", `p :save!`, ":save!\n"},
		{"local_assign", "x = :greeting\np x", ":greeting\n"},
		{"method_missing_gets_symbol",
			"class G\n  def method_missing(n)\n    n.class\n  end\nend\np G.new.foo", "Symbol\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestSymbolStringCoercion(t *testing.T) {
	// method_missing now receives a Symbol, so concatenating it to a String
	// raises TypeError — matching MRI (the Phase 1 String compromise is gone).
	src := "class G\n  def method_missing(n)\n    \"x\" + n\n  end\nend\nG.new.foo"
	if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("got %v want TypeError", err)
	}
}
