package vm_test

import "testing"

// Radix integer literals: 0x/0X hex, 0o/0O and bare-leading-zero octal, 0b/0B
// binary, 0d/0D explicit decimal, with underscores.
func TestRadixLiterals(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"hex_upper", `p 0xFF`, "255\n"},
		{"hex_lower", `p 0xff`, "255\n"},
		{"hex_mixed_prefix", `p 0XfF`, "255\n"},
		{"octal_o", `p 0o17`, "15\n"},
		{"octal_O", `p 0O17`, "15\n"},
		{"octal_bare", `p 017`, "15\n"},
		{"octal_bare_arith", `p 010 + 1`, "9\n"},
		{"binary", `p 0b1010`, "10\n"},
		{"binary_B", `p 0B1101`, "13\n"},
		{"decimal_prefix", `p 0d123`, "123\n"},
		{"decimal_prefix_D", `p 0D45`, "45\n"},
		{"hex_underscore", `p 0xFF_FF`, "65535\n"},
		{"binary_underscore", `p 0b1010_1010`, "170\n"},
		{"hex_bitand", `p 0xff & 0x0f`, "15\n"},
		{"hex_to_s_base", `p 0x1F.to_s(2)`, "\"11111\"\n"},
		{"hex_bignum", `p 0xFFFFFFFFFFFFFFFFFF`, "4722366482869645213695\n"},
		{"shift_hex_amount", `p 1 << 0x4`, "16\n"},
		{"plain_decimal_unaffected", `p 100`, "100\n"},
		{"decimal_underscore_unaffected", `p 1_000_000`, "1000000\n"},
		{"zero", `p 0`, "0\n"},
		{"float_unaffected", `p 0.5`, "0.5\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestRadixLiteralErrors(t *testing.T) {
	// 08/09 are invalid octal; an empty radix body is malformed.
	for _, src := range []string{`p 08`, `p 09`, `p 0x`, `p 0xG`, `p 0b2`, `p 0o9`} {
		if err := runErr(t, src); err == nil {
			t.Errorf("src=%q: expected a parse error, got none", src)
		}
	}
}
