package vm_test

import (
	"strings"
	"testing"
)

func TestStringMethods(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// length / size are rune-aware; bytesize is bytes.
		{"length", `p "héllo".length`, "5\n"},
		{"size", `p "héllo".size`, "5\n"},
		{"bytesize", `p "héllo".bytesize`, "6\n"},
		{"empty_true", `p "".empty?`, "true\n"},
		{"empty_false", `p "x".empty?`, "false\n"},
		// case transforms
		{"upcase", `p "Hello".upcase`, "\"HELLO\"\n"},
		{"downcase", `p "Hello".downcase`, "\"hello\"\n"},
		{"capitalize", `p "hELLO wORLD".capitalize`, "\"Hello world\"\n"},
		{"capitalize_empty", `p "".capitalize`, "\"\"\n"},
		{"swapcase", `p "Foo Bar9".swapcase`, "\"fOO bAR9\"\n"},
		{"reverse", `p "abc".reverse`, "\"cba\"\n"},
		// strip family
		{"strip", `p "  hi  ".strip`, "\"hi\"\n"},
		{"lstrip", `p "  hi  ".lstrip`, "\"hi  \"\n"},
		{"rstrip", `p "  hi  ".rstrip`, "\"  hi\"\n"},
		{"chomp_nl", "p \"line\\n\".chomp", "\"line\"\n"},
		{"chomp_crlf", "p \"line\\r\\n\".chomp", "\"line\"\n"},
		{"chomp_cr", "p \"line\\r\".chomp", "\"line\"\n"},
		{"chomp_none", `p "line".chomp`, "\"line\"\n"},
		{"chop", `p "abc".chop`, "\"ab\"\n"},
		{"chop_crlf", "p \"ab\\r\\n\".chop", "\"ab\"\n"},
		{"chop_empty", `p "".chop`, "\"\"\n"},
		// decomposition
		{"chars", `p "abc".chars`, "[\"a\", \"b\", \"c\"]\n"},
		{"bytes", `p "AB".bytes`, "[65, 66]\n"},
		{"split_ws", `p "Hello  World ".split`, "[\"Hello\", \"World\"]\n"},
		{"split_sep", `p "a,b,c".split(",")`, "[\"a\", \"b\", \"c\"]\n"},
		// queries
		{"include_true", `p "hello".include?("ell")`, "true\n"},
		{"include_false", `p "hello".include?("z")`, "false\n"},
		{"start_with", `p "hello".start_with?("he")`, "true\n"},
		{"end_with", `p "hello".end_with?("lo")`, "true\n"},
		{"index_found", `p "hello".index("l")`, "2\n"},
		{"index_missing", `p "hello".index("z")`, "nil\n"},
		// substitution (string patterns)
		{"sub", `p "foo".sub("o", "0")`, "\"f0o\"\n"},
		{"gsub", `p "foo".gsub("o", "0")`, "\"f00\"\n"},
		// conversions
		{"to_i", `p "42abc".to_i`, "42\n"},
		{"to_i_signed_ws", `p "  -17x".to_i`, "-17\n"},
		{"to_i_none", `p "abc".to_i`, "0\n"},
		// An integer that overflows int64 promotes to a Bignum, as in MRI.
		{"to_i_overflow", `p "99999999999999999999999".to_i`, "99999999999999999999999\n"},
		{"to_f", `p "3.14x".to_f`, "3.14\n"},
		{"to_f_signed", `p "-2.5".to_f`, "-2.5\n"},
		{"to_f_exp", `p "1.5e3y".to_f`, "1500.0\n"},
		{"to_f_exp_signed", `p "1e-2".to_f`, "0.01\n"},
		{"to_f_none", `p "x".to_f`, "0.0\n"},
		{"to_s", `p "hi".to_s`, "\"hi\"\n"},
		{"to_str", `p "hi".to_str`, "\"hi\"\n"},
		{"to_sym", `p "abc".to_sym`, ":abc\n"},
		// indexing / slicing
		{"index_char", `p "abcdef"[1]`, "\"b\"\n"},
		{"index_neg", `p "abcdef"[-2]`, "\"e\"\n"},
		{"index_oob", `p "abcdef"[9]`, "nil\n"},
		{"slice_len", `p "abcdef"[1, 3]`, "\"bcd\"\n"},
		{"slice_len_clamp", `p "abcdef"[4, 9]`, "\"ef\"\n"},
		{"slice_len_oob", `p "abcdef"[9, 1]`, "nil\n"},
		{"slice_len_neg", `p "abcdef"[1, -1]`, "nil\n"},
		{"slice_range", `p "abcdef"[1..3]`, "\"bcd\"\n"},
		{"slice_range_excl", `p "abcdef"[1...3]`, "\"bc\"\n"},
		{"slice_range_neg", `p "abcdef"[-3..-1]`, "\"def\"\n"},
		{"slice_range_oob", `p "abcdef"[9..10]`, "nil\n"},
		{"slice_range_hi_clamp", `p "abcdef"[4..10]`, "\"ef\"\n"},
		{"slice_range_empty", `p "abcdef"[3..1]`, "\"\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringArgTypeError(t *testing.T) {
	if err := runErr(t, `"x".include?(5)`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("got %v want TypeError", err)
	}
}
