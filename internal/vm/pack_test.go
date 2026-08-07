package vm_test

import (
	"strings"
	"testing"
)

// TestPackUnpack exercises every supported Array#pack / String#unpack directive
// and round-trip, asserting on byte content (via .bytes) so binary strings can
// be compared without depending on String#inspect of non-default encodings.
func TestPackUnpack(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// C / c (unsigned/signed 8-bit)
		{"pack_C_star", `p [65,66,67].pack("C*").bytes`, "[65, 66, 67]\n"},
		{"pack_C_count", `p [65,66,67].pack("C2").bytes`, "[65, 66]\n"},
		{"pack_C_one", `p [65].pack("C").bytes`, "[65]\n"},
		{"pack_c_neg", `p [-1].pack("c").bytes`, "[255]\n"},
		{"unpack_C_star", `p [65,66,67].pack("C*").unpack("C*")`, "[65, 66, 67]\n"},
		{"unpack_c_signed", `p [255].pack("C").unpack("c")`, "[-1]\n"},
		{"unpack_C_unsigned", `p [255].pack("C").unpack("C")`, "[255]\n"},

		// S / s (native little-endian 16-bit)
		{"pack_S", `p [258].pack("S").bytes`, "[2, 1]\n"},
		{"pack_s", `p [258].pack("s").bytes`, "[2, 1]\n"},
		{"unpack_S", `p [258].pack("S").unpack("S")`, "[258]\n"},
		{"unpack_s_neg", `p [-2].pack("s").unpack("s")`, "[-2]\n"},

		// L / l (native little-endian 32-bit)
		{"pack_L", `p [16909060].pack("L").bytes`, "[4, 3, 2, 1]\n"},
		{"unpack_L", `p [16909060].pack("L").unpack("L")`, "[16909060]\n"},
		{"unpack_l_neg", `p [-3].pack("l").unpack("l")`, "[-3]\n"},

		// Q / q (native little-endian 64-bit)
		{"pack_Q", `p [1].pack("Q").bytes`, "[1, 0, 0, 0, 0, 0, 0, 0]\n"},
		{"unpack_Q", `p [123456789].pack("Q").unpack("Q")`, "[123456789]\n"},
		{"unpack_q_neg", `p [-5].pack("q").unpack("q")`, "[-5]\n"},

		// n / N (big-endian unsigned 16/32-bit)
		{"pack_n", `p [1].pack("n").bytes`, "[0, 1]\n"},
		{"pack_N", `p [1].pack("N").bytes`, "[0, 0, 0, 1]\n"},
		{"pack_N_star", `p [1,2,3].pack("N*").bytes`, "[0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3]\n"},
		{"unpack_n", `p [513].pack("n").unpack("n")`, "[513]\n"},
		{"unpack_N", `p [1].pack("N").unpack("N")`, "[1]\n"},

		// v / V (little-endian unsigned 16/32-bit)
		{"pack_v", `p [1].pack("v").bytes`, "[1, 0]\n"},
		{"pack_V", `p [1].pack("V").bytes`, "[1, 0, 0, 0]\n"},
		{"unpack_v", `p [513].pack("v").unpack("v")`, "[513]\n"},
		{"unpack_V", `p [70000].pack("V").unpack("V")`, "[70000]\n"},

		// a / A / Z (binary / space-padded / null-terminated)
		{"pack_a5", `p ["hi"].pack("a5").bytes`, "[104, 105, 0, 0, 0]\n"},
		{"pack_a_trunc", `p ["hello"].pack("a2").bytes`, "[104, 101]\n"},
		{"pack_a_star", `p ["hi"].pack("a*").bytes`, "[104, 105]\n"},
		{"pack_A5", `p ["hi"].pack("A5").bytes`, "[104, 105, 32, 32, 32]\n"},
		{"pack_Z_star", `p ["hi"].pack("Z*").bytes`, "[104, 105, 0]\n"},
		{"pack_Z5", `p ["hi"].pack("Z5").bytes`, "[104, 105, 0, 0, 0]\n"},
		{"unpack_a_keeps_nul", `p [104,105,0,0].pack("C*").unpack("a*")[0].bytes`, "[104, 105, 0, 0]\n"},
		{"unpack_A_strips", `p [104,105,0,0].pack("C*").unpack("A*")`, "[\"hi\"]\n"},
		{"unpack_A_strips_space", `p [104,105,32,32].pack("C*").unpack("A*")`, "[\"hi\"]\n"},
		{"unpack_Z_stops", `p [104,105,0,120].pack("C*").unpack("Z*")`, "[\"hi\"]\n"},
		{"unpack_Z_no_nul", `p [65,66,67].pack("C*").unpack("Z*")`, "[\"ABC\"]\n"},
		{"unpack_Z_count", `p [65,66,67].pack("C*").unpack("Z2")`, "[\"AB\"]\n"},
		{"unpack_a2a2", `p "ABCD".unpack("a2a2")`, "[\"AB\", \"CD\"]\n"},
		{"unpack_a_count", `p "ABCD".unpack("a2")`, "[\"AB\"]\n"},
		{"unpack_a_overrun", `p "AB".unpack("a5")`, "[\"AB\"]\n"},

		// H / h (hex high/low nibble first)
		{"pack_H_star", `p ["ff0a"].pack("H*").bytes`, "[255, 10]\n"},
		{"pack_h_star", `p ["ff0a"].pack("h*").bytes`, "[255, 160]\n"},
		{"pack_H_odd", `p ["f"].pack("H*").bytes`, "[240]\n"},
		{"pack_h_odd", `p ["f"].pack("h*").bytes`, "[15]\n"},
		{"pack_H_count", `p ["abcd"].pack("H2").bytes`, "[171]\n"},
		{"pack_H_upper", `p ["FF"].pack("H*").bytes`, "[255]\n"},
		{"pack_H_nonhex", `p ["zz"].pack("H*").bytes`, "[51]\n"},
		{"unpack_H_star", `p [255,10].pack("C*").unpack("H*")`, "[\"ff0a\"]\n"},
		{"unpack_h_star", `p [240,160].pack("C*").unpack("h*")`, "[\"0f0a\"]\n"},
		{"unpack_H_count", `p [171,205].pack("C*").unpack("H3")`, "[\"abc\"]\n"},
		{"unpack_h_count", `p [171].pack("C*").unpack("h1")`, "[\"b\"]\n"},

		// U (UTF-8 character / codepoint)
		{"pack_U", `p [0x3042].pack("U").bytes`, "[227, 129, 130]\n"},
		{"pack_U_star", `p [65,0x3042].pack("U*").bytes`, "[65, 227, 129, 130]\n"},
		{"unpack_U_star", `p [0x3042,0x3043].pack("U*").unpack("U*")`, "[12354, 12355]\n"},
		{"unpack_U_count", `p [65,66,67].pack("U*").unpack("U2")`, "[65, 66]\n"},

		// spaces are ignored in the format
		{"spaces_ignored", `p "ABCDEF".unpack("a2 a2")`, "[\"AB\", \"CD\"]\n"},

		// unpack1 returns the first element or nil
		{"unpack1_N", `p [1].pack("N").unpack1("N")`, "1\n"},
		{"unpack1_first", `p "ABCD".unpack1("a2a2")`, "\"AB\"\n"},
		{"unpack1_nil_elem", `p "".unpack1("N")`, "nil\n"},
		{"unpack1_no_elems", `p "".unpack1("C*")`, "nil\n"},

		// short data yields nil for integer directives
		{"unpack_short_nil", `p "x".unpack("NN")`, "[nil, nil]\n"},
		{"unpack_U_empty", `p "".unpack("U*")`, "[]\n"},
		{"unpack_a_empty_star", `p "".unpack("a*")`, "[\"\"]\n"},

		// Float directives f/F/d/D (native, host LE), e/E (LE), g/G (BE).
		{"pack_f", `p [1.5].pack("f").bytes`, "[0, 0, 192, 63]\n"},
		{"pack_F_alias", `p [1.5].pack("F").bytes`, "[0, 0, 192, 63]\n"},
		{"pack_e_le", `p [1.5].pack("e").bytes`, "[0, 0, 192, 63]\n"},
		{"pack_g_be", `p [1.5].pack("g").bytes`, "[63, 192, 0, 0]\n"},
		{"pack_d", `p [1.5].pack("d").bytes`, "[0, 0, 0, 0, 0, 0, 248, 63]\n"},
		{"pack_D_alias", `p [1.5].pack("D").bytes`, "[0, 0, 0, 0, 0, 0, 248, 63]\n"},
		{"pack_E_le", `p [1.5].pack("E").bytes`, "[0, 0, 0, 0, 0, 0, 248, 63]\n"},
		{"pack_G_be", `p [1.5].pack("G").bytes`, "[63, 248, 0, 0, 0, 0, 0, 0]\n"},
		{"roundtrip_d_star", `p [1.5, -2.25].pack("d*").unpack("d*")`, "[1.5, -2.25]\n"},
		{"roundtrip_f_count", `p [1.5, 2.5].pack("f2").unpack("f2")`, "[1.5, 2.5]\n"},
		{"roundtrip_g_G", `p [-0.5].pack("g").unpack("g") == [-0.5].pack("G").unpack("G")`, "true\n"},
		{"f_single_precision", `p [3.14].pack("f").unpack("f")`, "[3.140000104904175]\n"},
		{"pack_f_int_arg", `p [3].pack("f").unpack("f")`, "[3.0]\n"},
		{"pack_f_bignum_arg", `p [2**70].pack("d").unpack("d")`, "[1.1805916207174113e+21]\n"},
		{"pack_f_rational_arg", `p [Rational(3, 2)].pack("f").unpack("f")`, "[1.5]\n"},
		{"unpack_f_short_nil", `p ("ab").unpack("f")`, "[nil]\n"},
		{"unpack_f_star", `p [1.0, 2.0, 3.0].pack("f*").unpack("f*")`, "[1.0, 2.0, 3.0]\n"},

		// x (null pad), X (back up), @ (absolute position) — pack.
		{"pack_x3", `p [].pack("x3").bytes`, "[0, 0, 0]\n"},
		{"pack_x_star_empty", `p [].pack("x*").bytes`, "[]\n"},
		{"pack_CxC", `p [1, 2].pack("CxC").bytes`, "[1, 0, 2]\n"},
		{"pack_X_back", `p [1, 2, 3].pack("C2XC").bytes`, "[1, 3]\n"},
		{"pack_X_star_noop", `p [1, 2, 3].pack("C2X*C").bytes`, "[1, 2, 3]\n"},
		{"pack_at_pad", `p [1, 2, 3].pack("C@3C").bytes`, "[1, 0, 0, 2]\n"},
		{"pack_at_truncate", `p [1, 2, 3, 4, 5].pack("C4@2C").bytes`, "[1, 2, 5]\n"},
		{"pack_at_default_one", `p [1, 2, 3].pack("C*@").bytes`, "[1]\n"},
		{"pack_at_from_empty", `p [1, 2, 3].pack("@3C*").bytes`, "[0, 0, 0, 1, 2, 3]\n"},
		{"pack_at_star_noop", `p [1, 2, 3].pack("C2@*C").bytes`, "[1, 2, 3]\n"},
		// x/X/@ — unpack.
		{"unpack_x_skip", `p "\x01\x02\x03\x04".unpack("Cx2C")`, "[1, 4]\n"},
		{"unpack_x_star_to_end", `p "\x01\x02\x03\x04".unpack("Cx*C")`, "[1, nil]\n"},
		{"unpack_X_back", `p "\x01\x02\x03\x04".unpack("C3X2C")`, "[1, 2, 3, 2]\n"},
		{"unpack_X_one", `p "\x01\x02\x03\x04".unpack("C3XC")`, "[1, 2, 3, 3]\n"},
		{"unpack_X_star", `p "abcd".unpack("C3X*C")`, "[97, 98, 99, 99]\n"},
		{"unpack_at_abs", `p "\x01\x02\x03\x04".unpack("C3@2C")`, "[1, 2, 3, 3]\n"},
		{"unpack_at_default_zero", `p "\x01\x02\x03\x04".unpack("C2@C")`, "[1, 2, 1]\n"},
		{"unpack_at_star_noop", `p "\x01\x02\x03\x04".unpack("C2@*C")`, "[1, 2, 3]\n"},
		{"unpack_at_beyond", `p "\x01\x02\x03\x04".unpack("C2@4C")`, "[1, 2, nil]\n"},

		// b/B bit strings: B is MSB-first, b is LSB-first; bit = char & 1.
		{"pack_B_one", `p ["1"].pack("B").bytes`, "[128]\n"},
		{"pack_b_one", `p ["1"].pack("b").bytes`, "[1]\n"},
		{"pack_B_byte", `p ["01000001"].pack("B*")`, "\"A\"\n"},
		{"pack_b_byte", `p ["10000010"].pack("b*")`, "\"A\"\n"},
		{"pack_B_star_multibyte", `p ["1101010011000011"].pack("B*").bytes`, "[212, 195]\n"},
		{"unpack_B8", `p "\x83".unpack("B8")`, "[\"10000011\"]\n"},
		{"unpack_b8", `p "\x83".unpack("b8")`, "[\"11000001\"]\n"},
		{"unpack_B_cap", `p "\x83".unpack("B25")`, "[\"10000011\"]\n"},
		{"unpack_B_multi", `p "\xd4\xc3".unpack("B5B*")`, "[\"11010\", \"11000011\"]\n"},
		{"unpack_b_star", `p "A".unpack("b*")`, "[\"10000010\"]\n"},
		{"roundtrip_B", `p "hello".unpack("B*").pack("B*")`, "\"hello\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestPackUnpackModifiers exercises the integer byte-order ('<' / '>') and
// native-size ('!' / '_') modifiers, the i/I/j/J directives, comments and
// whitespace, and Bignum-valued 64-bit round-trips. Host is little-endian
// (arm64 / amd64), so the native directives decode little-endian.
func TestPackUnpackModifiers(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// '<' / '>' byte-order overrides
		{"unpack_s_le", `p "\x00\xff".unpack("s<")`, "[-256]\n"},
		{"unpack_s_be", `p "\x00\xff".unpack("s>")`, "[255]\n"},
		{"unpack_L_le", `p "abcd".unpack("L<")`, "[1684234849]\n"},
		{"unpack_L_be", `p "abcd".unpack("L>")`, "[1633837924]\n"},
		{"pack_S_le", `p [258].pack("S<").bytes`, "[2, 1]\n"},
		{"pack_S_be", `p [258].pack("S>").bytes`, "[1, 2]\n"},
		{"pack_L_be", `p [16909060].pack("L>").bytes`, "[1, 2, 3, 4]\n"},
		{"pack_Q_be", `p [1].pack("Q>").bytes`, "[0, 0, 0, 0, 0, 0, 0, 1]\n"},
		// modifier order and combinations
		{"unpack_L_le_bang", `p "abcdefgh".unpack("L<_")`, "[7523094288207667809]\n"},
		{"unpack_L_bang_le", `p "abcdefgh".unpack("L_<")`, "[7523094288207667809]\n"},
		{"unpack_L_le_native", `p "abcdefgh".unpack("L<!")`, "[7523094288207667809]\n"},
		// '!' / '_' widen l/L to the C long (8 bytes on LP64)
		{"pack_L_bang_width", `p [1].pack("L!").bytes.length`, "8\n"},
		{"pack_l_under_width", `p [1].pack("l_").bytes.length`, "8\n"},
		// '!' leaves the other integer directives at their native width
		{"pack_S_bang_width", `p [1].pack("S!").bytes.length`, "2\n"},
		{"pack_I_bang_width", `p [1].pack("I!").bytes.length`, "4\n"},
		{"pack_Q_bang_width", `p [1].pack("Q!").bytes.length`, "8\n"},
		{"pack_j_bang_width", `p [1].pack("j!").bytes.length`, "8\n"},
		// i / I (native C int, 32-bit) and j / J (intptr_t, 64-bit)
		{"pack_i", `p [1].pack("i").bytes`, "[1, 0, 0, 0]\n"},
		{"pack_I_be", `p [1].pack("I>").bytes`, "[0, 0, 0, 1]\n"},
		{"unpack_i_le", `p "abcd".unpack("i<")`, "[1684234849]\n"},
		{"unpack_I_le", `p "abcd".unpack("I<")`, "[1684234849]\n"},
		{"pack_j_width", `p [1].pack("j").bytes.length`, "8\n"},
		{"unpack_j_le", `p "abcdefgh".unpack("j<")`, "[7523094288207667809]\n"},
		{"unpack_J_le", `p "abcdefgh".unpack("J<")`, "[7523094288207667809]\n"},
		// unsigned 64-bit above 2**63 decodes to a Bignum
		{"unpack_Q_big", `p "\x00\xcc\x00\xbb\x00\xaa\x00\xff".unpack("Q<")`, "[18374873399785737216]\n"},
		{"unpack_q_neg64", `p "\x00\xcc\x00\xbb\x00\xaa\x00\xff".unpack("q<")`, "[-71870673923814400]\n"},
		// Bignum pack argument (value above 2**63) round-trips
		{"pack_bignum_Q", `p [0xdef0abcd34127856].pack("Q<").unpack("Q<")`, "[16064528768660830294]\n"},
		{"pack_negint_Q", `p [-1].pack("Q<").unpack("Q<")`, "[18446744073709551615]\n"},
		// whitespace (all ISSPACE kinds) is ignored
		{"whitespace_all", `p "ABCD".unpack("a2\t\n\v\f\ra2")`, "[\"AB\", \"CD\"]\n"},
		// comments run to end of line, or to end of string
		{"comment_nl", "p [65].pack(\"C # note\n\").bytes", "[65]\n"},
		{"comment_eof", `p [65].pack("C#tail").bytes`, "[65]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestPackUnpackCoercion covers the #to_str format coercion, #to_str element
// coercion for a/A/Z, #to_int and Float element coercion for the integer
// directives.
func TestPackUnpackCoercion(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"fmt_to_str", `o = Object.new; def o.to_str; "C"; end; p [65].pack(o).bytes`, "[65]\n"},
		{"unpack_fmt_to_str", `o = Object.new; def o.to_str; "C"; end; p "A".unpack(o)`, "[65]\n"},
		{"a_to_str", `o = Object.new; def o.to_str; "hi"; end; p [o].pack("a2").bytes`, "[104, 105]\n"},
		{"int_to_int", `o = Object.new; def o.to_int; 65; end; p [o].pack("C").bytes`, "[65]\n"},
		{"int_float_trunc", `p [1.9].pack("C").bytes`, "[1]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestPackUnpackModifierErrors covers the modifier validation raises.
func TestPackUnpackModifierErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"lt_on_C", `"x".unpack("C<")`, "ArgumentError"},
		{"gt_on_C", `"x".unpack("C>")`, "ArgumentError"},
		{"bang_on_C", `"x".unpack("C!")`, "ArgumentError"},
		{"bang_on_N", `"x".unpack("N!")`, "ArgumentError"},
		{"lt_on_N", `"x".unpack("N<")`, "ArgumentError"},
		{"both_lt_gt", `"abcd".unpack("L<>")`, "ArgumentError"},
		{"fmt_to_str_bad", `o = Object.new; def o.to_str; 5; end; [1].pack(o)`, "TypeError"},
		{"a_to_str_bad", `o = Object.new; def o.to_str; 5; end; [o].pack("a2")`, "TypeError"},
		{"int_no_coerce", `["x"].pack("C")`, "TypeError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}

// TestPackUnpackErrors covers the raising paths: unknown directive, missing
// format argument, wrong argument types, and too few pack arguments.
func TestPackUnpackErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"pack_unknown_dir", `[1].pack("Y")`, "ArgumentError"},
		{"unpack_unknown_dir", `"x".unpack("Y")`, "ArgumentError"},
		{"unpack1_unknown_dir", `"x".unpack1("Y")`, "ArgumentError"},
		{"pack_no_arg", `[1].pack`, "ArgumentError"},
		{"unpack_no_arg", `"x".unpack`, "ArgumentError"},
		{"unpack1_no_arg", `"x".unpack1`, "ArgumentError"},
		{"pack_fmt_not_string", `[1].pack(1)`, "TypeError"},
		{"unpack_fmt_not_string", `"x".unpack(1)`, "TypeError"},
		{"pack_too_few", `[].pack("N")`, "ArgumentError"},
		{"pack_too_few_str", `[].pack("a3")`, "ArgumentError"},
		{"pack_too_few_hex", `[].pack("H2")`, "ArgumentError"},
		{"pack_too_few_U", `[].pack("U")`, "ArgumentError"},
		{"pack_a_not_string", `[1].pack("a3")`, "TypeError"},
		{"pack_H_not_string", `[1].pack("H2")`, "TypeError"},
		// Float directives reject non-Numeric args (nil, String, Object) — pack
		// does not parse numeric strings or call #to_f on non-Numerics.
		{"pack_f_nil", `[nil].pack("f")`, "can't convert nil into Float"},
		{"pack_f_string", `["1.5"].pack("f")`, "can't convert String into Float"},
		{"pack_f_object", `[Object.new].pack("d")`, "can't convert Object into Float"},
		// x/X/@ position-directive errors.
		{"pack_X_underflow", `[1, 2, 3].pack("XC")`, "X outside of string"},
		{"pack_X_count_over", `[1, 2, 3].pack("C2X3")`, "X outside of string"},
		{"unpack_x_over", `"ab".unpack("Cx5")`, "x outside of string"},
		{"unpack_X_over", `"abcd".unpack("C2X3C")`, "X outside of string"},
		{"unpack_at_over", `"\x01\x02\x03\x04".unpack("@5C")`, "@ outside of string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}
