package vm_test

import (
	"strings"
	"testing"
)

// %w[…] / %i[…] word- and symbol-array literals (non-interpolating).
func TestPercentArrays(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"words", `p(%w[a b c])`, "[\"a\", \"b\", \"c\"]\n"},
		{"symbols", `p(%i[a b c])`, "[:a, :b, :c]\n"},
		{"paren_delim", `p(%w(one two))`, "[\"one\", \"two\"]\n"},
		{"brace_delim", `p(%w{foo bar})`, "[\"foo\", \"bar\"]\n"},
		{"angle_delim", `p(%w<l r>)`, "[\"l\", \"r\"]\n"},
		{"pipe_delim", `p(%i|s1 s2|)`, "[:s1, :s2]\n"},
		{"bang_delim", `p(%w!a b!)`, "[\"a\", \"b\"]\n"},
		{"slash_delim", `p(%w/a b/)`, "[\"a\", \"b\"]\n"},
		{"empty", `p(%w[])`, "[]\n"},
		{"blank", `p(%w[   ])`, "[]\n"},
		{"extra_space", `p(%w[  lots   of   space  ])`, "[\"lots\", \"of\", \"space\"]\n"},
		{"assign_rhs", "a = %w[x y]\np a", "[\"x\", \"y\"]\n"},
		{"stmt_start", "%w[a b]\np 1", "1\n"},
		{"with_block", `p(%w[a b c].map { it.upcase })`, "[\"A\", \"B\", \"C\"]\n"},
		{"nested_in_array", `p([%w[a b], %i[c d]])`, "[[\"a\", \"b\"], [:c, :d]]\n"},
		{"in_hash", `p({x: %w[1 2]})`, "{x: [\"1\", \"2\"]}\n"},
		{"nested_bracket", `p(%w[ab[1] cd])`, "[\"ab[1]\", \"cd\"]\n"},
		{"multiline", "p(%w[a\n  b\n  c])", "[\"a\", \"b\", \"c\"]\n"},
		{"length", "p(%w[red green blue].length)", "3\n"},
		// Modulo is unaffected by the %-literal lexing.
		{"modulo_still_works", "m = 10\np(m % 3)", "1\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestPercentArrayUnterminated(t *testing.T) {
	err := runErr(t, `p(%w[a b c)`)
	if err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("got err=%v, want 'unterminated'", err)
	}
}

// A `%` at expression-begin that does not open a %w/%i literal falls through to
// the modulo operator (here a parse error). These exercise the non-literal
// branches of the lexer's percent-array detection: a non-w/i letter (`%5`),
// end-of-input right after the kind letter (`%w`), and a kind letter not
// followed by a delimiter (`%wx`).
func TestPercentNotAnArray(t *testing.T) {
	for _, src := range []string{`%5`, `%w`, `%wx`} {
		if err := runErr(t, src); err == nil {
			t.Errorf("src=%q: expected a parse error, got none", src)
		}
	}
}
