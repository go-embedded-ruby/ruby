package vm_test

import "testing"

// %W[…] / %I[…] interpolating word- and symbol-array literals.
func TestPercentInterpArrays(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"words_plain", `p(%W[a b c])`, "[\"a\", \"b\", \"c\"]\n"},
		{"symbols_plain", `p(%I[a b c])`, "[:a, :b, :c]\n"},
		{"words_interp", "n = 2\np(%W[x#{n} y#{n + 1}])", "[\"x2\", \"y3\"]\n"},
		{"symbols_interp", "n = 2\np(%I[sym#{n} plain])", "[:sym2, :plain]\n"},
		{"symbols_all_interp", "n = 2\np(%I[a#{n}b])", "[:a2b]\n"},
		{"space_inside_interp", `p(%W[#{"a b"} c])`, "[\"a b\", \"c\"]\n"},
		{"method_in_interp", "g = \"hi\"\np(%W[#{g.upcase}!])", "[\"HI!\"]\n"},
		{"nested_braces_interp", `p(%W[#{ {1 => 2}.size }x])`, "[\"1x\"]\n"},
		{"empty_W", `p(%W[])`, "[]\n"},
		{"empty_I", `p(%I[])`, "[]\n"},
		{"extra_whitespace", `p(%W[  one   two  ])`, "[\"one\", \"two\"]\n"},
		{"length", "x = %W[p#{1 + 1}q r]\np x.length", "2\n"},
		{"paren_delim", `p(%W(a#{1} b))`, "[\"a1\", \"b\"]\n"},
		{"assign_rhs", "a = %W[x#{1}]\np a", "[\"x1\"]\n"},
		// %w/%i stay literal: #{ is not interpolation there.
		{"lowercase_literal", "n = 2\np(%w[a#{n} b])", "[\"a\\#{n}\", \"b\"]\n"},
		// A tab between words still splits.
		{"tab_separator", "p(%W[a\tb])", "[\"a\", \"b\"]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
