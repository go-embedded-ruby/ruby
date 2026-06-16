package vm_test

import "testing"

func TestPatternPinAndAlternative(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"alt_values", "case 2\nin 1 | 2 | 3 then p \"small\"\nend", "\"small\"\n"},
		{"alt_first", "case 1\nin 1 | 2 then p \"a\"\nend", "\"a\"\n"},
		{"alt_last", "case 3\nin 1 | 2 | 3 then p \"c\"\nend", "\"c\"\n"},
		{"alt_types", "case 7.0\nin Integer | Float then p \"num\"\nend", "\"num\"\n"},
		{"alt_strings", "case \"b\"\nin \"a\" | \"b\" | \"c\" then p \"abc\"\nend", "\"abc\"\n"},
		{"alt_no_match_else", "case 9\nin 1 | 2 then p \"x\"\nelse p \"else\"\nend", "\"else\"\n"},
		{"pin_local", "y = 5\ncase 5\nin ^y then p \"match\"\nend", "\"match\"\n"},
		{"pin_no_match", "y = 5\ncase 6\nin ^y then p \"a\"\nin _ then p \"b\"\nend", "\"b\"\n"},
		{"pin_paren", "case 4\nin ^(2 + 2) then p \"four\"\nend", "\"four\"\n"},
		{"pin_in_array", "y = 5\ncase [1, 5]\nin [^y, _] then p \"no\"\nin [_, ^y] then p \"snd\"\nend", "\"snd\"\n"},
		{"alt_in_array", "case [2, 9]\nin [1 | 2, b] then p b\nend", "9\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
