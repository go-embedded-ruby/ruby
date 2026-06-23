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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
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
