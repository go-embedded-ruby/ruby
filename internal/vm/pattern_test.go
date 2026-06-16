package vm_test

import (
	"strings"
	"testing"
)

// TestCaseInCore covers the core case/in pattern-matching forms: value,
// variable-binding, wildcard, class/constant, array (with literals, bindings,
// splats, nesting), the `=> name` binding suffix, guards, and the else clause.
func TestCaseInCore(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"value_int", "case 42\nin 42\n puts \"yes\"\nend", "yes\n"},
		{"value_int_no", "case 5\nin 42\n puts \"yes\"\nelse\n puts \"no\"\nend", "no\n"},
		{"value_float", "case 1.5\nin 1.5\n puts \"f\"\nend", "f\n"},
		{"value_string", "case \"hi\"\nin \"hi\"\n puts \"s\"\nend", "s\n"},
		{"value_symbol", "case :ok\nin :ok\n puts \"sym\"\nend", "sym\n"},
		{"value_true", "case true\nin true\n puts \"t\"\nend", "t\n"},
		{"value_false", "case false\nin false\n puts \"f\"\nend", "f\n"},
		{"value_nil", "case nil\nin nil\n puts \"n\"\nend", "n\n"},
		{"value_range", "case 3\nin 1..5\n puts \"in range\"\nend", "in range\n"},
		{"value_range_no", "case 9\nin 1..5\n puts \"y\"\nelse\n puts \"out\"\nend", "out\n"},
		{"bind_whole", "case 99\nin x\n puts x\nend", "99\n"},
		{"wildcard", "case [1, 2]\nin [_, _]\n puts \"any two\"\nend", "any two\n"},
		{"const_match", "case 5\nin Integer\n puts \"int\"\nend", "int\n"},
		{"const_no_match", "case 5\nin String\n puts \"s\"\nelse\n puts \"not s\"\nend", "not s\n"},
		{"const_bind", "case 5\nin Integer => n\n puts n + 1\nend", "6\n"},
		{"array_pair", "case [10, 20]\nin [a, b]\n puts a + b\nend", "30\n"},
		{"array_mixed", "case [1, 99]\nin [1, x]\n puts x\nend", "99\n"},
		{"array_mixed_no", "case [2, 99]\nin [1, x]\n puts x\nelse\n puts \"miss\"\nend", "miss\n"},
		{"array_splat", "case [1, 2, 3]\nin [a, *rest]\n p [a, rest]\nend", "[1, [2, 3]]\n"},
		{"array_splat_empty", "case [1]\nin [a, *rest]\n p [a, rest]\nend", "[1, []]\n"},
		{"array_splat_unnamed", "case [1, 2, 3]\nin [a, *]\n puts a\nend", "1\n"},
		{"array_leading_splat", "case [1, 2, 3]\nin [*init, last]\n p [init, last]\nend", "[[1, 2], 3]\n"},
		{"array_mid_splat", "case [1, 2, 3, 4]\nin [a, *mid, z]\n p [a, mid, z]\nend", "[1, [2, 3], 4]\n"},
		{"array_nested", "case [1, [2, 3]]\nin [a, [b, c]]\n puts a + b + c\nend", "6\n"},
		{"array_len_mismatch", "case [1, 2, 3]\nin [a, b]\n puts \"y\"\nelse\n puts \"len\"\nend", "len\n"},
		{"array_bind_suffix", "case [1, 2]\nin [Integer, Integer] => pair\n p pair\nend", "[1, 2]\n"},
		{"array_non_array", "case 5\nin [a, b]\n puts \"y\"\nelse\n puts \"scalar\"\nend", "scalar\n"},
		{"implicit_array", "case [1, 2]\nin a, b\n puts a + b\nend", "3\n"},
		{"implicit_leading_splat", "case [1, 2, 3]\nin *a, b\n p [a, b]\nend", "[[1, 2], 3]\n"},
		{"guard_if", "case 5\nin Integer => n if n > 3\n puts \"big\"\nelse\n puts \"small\"\nend", "big\n"},
		{"guard_if_false", "case 2\nin Integer => n if n > 3\n puts \"big\"\nelse\n puts \"small\"\nend", "small\n"},
		{"guard_unless", "case 4\nin n unless n.even?\n puts \"odd\"\nelse\n puts \"even\"\nend", "even\n"},
		{"guard_pattern_fail", "case \"x\"\nin Integer => n if n > 3\n puts \"big\"\nelse\n puts \"other\"\nend", "other\n"},
		{"then_keyword", "case 1\nin 1 then puts \"one\"\nend", "one\n"},
		{"first_clause_wins", "case 3\nin Integer => n\n puts \"int #{n}\"\nin 3\n puts \"three\"\nend", "int 3\n"},
		{"result_value", "x = case 5\nin Integer\n  \"is int\"\nend\nputs x", "is int\n"},
		{"struct_deconstruct", "S = Struct.new(:a, :b)\ncase S.new(1, 2)\nin [x, y]\n puts x + y\nend", "3\n"},
		{"const_array_pattern", "S = Struct.new(:a, :b)\ncase S.new(4, 5)\nin S[x, y]\n puts x + y\nend", "9\n"},
		{"const_array_pattern_no", "S = Struct.new(:a, :b)\nT = Struct.new(:a, :b)\ncase S.new(4, 5)\nin T[x, y]\n puts \"t\"\nelse\n puts \"not t\"\nend", "not t\n"},
		{"trailing_comma", "case [1, 2]\nin [a, b,]\n puts a + b\nend", "3\n"},
		{"empty_array", "case []\nin []\n puts \"empty\"\nend", "empty\n"},
		{"empty_array_no", "case [1]\nin []\n puts \"y\"\nelse\n puts \"ne\"\nend", "ne\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestCaseInNoMatch covers NoMatchingPatternError: raised when no clause matches
// and there is no else, and that it is a rescuable StandardError.
func TestCaseInNoMatch(t *testing.T) {
	if err := runErr(t, "case 5\nin 6\n puts \"x\"\nend"); err == nil || !strings.Contains(err.Error(), "NoMatchingPatternError") {
		t.Errorf("scalar no-match: got %v", err)
	}
	if err := runErr(t, "case [1, 2, 3]\nin [a, b]\n puts \"x\"\nend"); err == nil || !strings.Contains(err.Error(), "NoMatchingPatternError") {
		t.Errorf("array no-match: got %v", err)
	}
	got := eval(t, `
begin
  case 5
  in 6
    puts "no"
  end
rescue StandardError => e
  puts "caught #{e.class}"
end`)
	if got != "caught NoMatchingPatternError\n" {
		t.Errorf("rescue: got %q", got)
	}
}

// TestCaseInParseErrors covers malformed patterns.
func TestCaseInParseErrors(t *testing.T) {
	for _, src := range []string{
		"case [1]\nin [*a, *b]\n puts 1\nend", // two splats
		"case 1\nin Integer =>\nend",          // => with no name
	} {
		if err := runErr(t, src); err == nil {
			t.Errorf("expected error for %q", src)
		}
	}
}
