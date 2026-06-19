package vm_test

import (
	"strings"
	"testing"
)

// Operator-method symbols (:+, :<<, :[]=, :<=>, …) and their use with send and
// reduce/inject.
func TestOperatorSymbols(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"plus", `p :+`, ":+\n"},
		{"shovel", `p :<<`, ":<<\n"},
		{"index_set", `p :[]=`, ":[]=\n"},
		{"index", `p :[]`, ":[]\n"},
		{"spaceship", `p :<=>`, ":<=>\n"},
		{"eq", `p :==`, ":==\n"},
		{"compare_lt", `p :<`, ":<\n"},
		{"pow", `p :**`, ":**\n"},
		{"tilde", `p :~`, ":~\n"},
		{"to_s", `p :+.to_s`, "\"+\"\n"},
		{"array_of_ops", `p [:+, :-, :*]`, "[:+, :-, :*]\n"},
		{"ternary_with_symbols", `p(1 > 2 ? :yes : :no)`, ":no\n"},
		{"still_ident_symbol", `p :foo`, ":foo\n"},
		{"still_predicate_symbol", `p :empty?`, ":empty?\n"},
		// send routes fast-path operators (which are not real methods).
		{"send_plus", `p 1.send(:+, 2)`, "3\n"},
		{"send_string_plus", `p "a".send(:+, "b")`, "\"ab\"\n"},
		{"send_mod", `p 7.send(:%, 3)`, "1\n"},
		{"send_lt", `p 5.send(:<, 3)`, "false\n"},
		{"send_eq", `p 5.send(:==, 5)`, "true\n"},
		{"send_neq", `p 5.send(:!=, 5)`, "false\n"},
		// A Symbol has no Comparable mix-in, so send(:==) reaches the operator
		// fast-path fallback rather than a method.
		{"send_eq_symbol", `p :a.send(:==, :a)`, "true\n"},
		{"send_eq_symbol_false", `p :a.send(:==, :b)`, "false\n"},
		{"send_minus", `p 9.send(:-, 4)`, "5\n"},
		{"send_star", `p 3.send(:*, 4)`, "12\n"},
		{"send_slash", `p 8.send(:/, 2)`, "4\n"},
		{"send_gt", `p 5.send(:>, 3)`, "true\n"},
		{"send_le", `p 3.send(:<=, 3)`, "true\n"},
		{"send_ge", `p 5.send(:>=, 9)`, "false\n"},
		{"send_real_method", `p 5.send(:<=>, 3)`, "1\n"},
		// reduce / inject with an operator symbol.
		{"reduce_sum", `p [1, 2, 3, 4].reduce(:+)`, "10\n"},
		{"reduce_product", `p [1, 2, 3, 4].reduce(:*)`, "24\n"},
		{"reduce_with_init", `p [1, 2, 3].reduce(10, :+)`, "16\n"},
		{"reduce_block", `p [1, 2, 3].reduce { |a, b| a + b }`, "6\n"},
		{"reduce_block_init", `p [1, 2, 3].reduce(100) { |a, b| a + b }`, "106\n"},
		{"inject_sym", `p [1, 2, 3].inject(:+)`, "6\n"},
		{"reduce_strings", `p ["a", "b", "c"].reduce(:+)`, "\"abc\"\n"},
		{"reduce_empty", `p [].reduce(:+)`, "nil\n"},
		{"reduce_single", `p [42].reduce(:+)`, "42\n"},
		{"reduce_range", `p (1..5).reduce(:+)`, "15\n"},
		{"reduce_named_method", `p [12, 8, 6].reduce(:gcd)`, "2\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// A non-operator unknown method still raises NoMethodError (the operator
// fallback does not swallow it), and a bare trailing `:` is not a symbol.
func TestOperatorSymbolNonMatches(t *testing.T) {
	if err := runErr(t, `1.no_such_method(2)`); err == nil || !strings.Contains(err.Error(), "NoMethodError") {
		t.Errorf("got %v, want NoMethodError", err)
	}
	if err := runErr(t, `:`); err == nil {
		t.Error("expected a parse error for a bare colon")
	}
}
