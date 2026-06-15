package vm_test

import (
	"strings"
	"testing"
)

// Phase 6c: String#sub and String#gsub extended to accept a Regexp (or a
// literal String) pattern, with a replacement template (\0/\&, \1..\9,
// \k<name>, \`, \') or a block. Pinned against MRI (Ruby 4.0). The Ruby source
// strings below are Go interpreted-string literals, so each backslash that
// should reach the Ruby program is written as \\.

func TestStringGsubRegexp(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"sub_first", `p "hello".sub(/l/, "L")`, "\"heLlo\"\n"},
		{"gsub_all", `p "hello".gsub(/l/, "L")`, "\"heLLo\"\n"},
		{"sub_no_match", `p "abc".sub(/z/, "X")`, "\"abc\"\n"},
		{"gsub_no_match", `p "abc".gsub(/z/, "X")`, "\"abc\"\n"},
		{"backref_groups", "p \"2026-06\".sub(/(\\d+)-(\\d+)/, \"\\\\1/\\\\2\")", "\"2026/06\"\n"},
		{"backref_whole_0", "p \"2026\".gsub(/(\\d)/, \"[\\\\0]\")", "\"[2][0][2][6]\"\n"},
		{"backref_amp", "p \"abc\".gsub(/b/, \"<\\\\&>\")", "\"a<b>c\"\n"},
		{"backref_named", "p \"2026-06\".sub(/(?<y>\\d+)-(?<m>\\d+)/, \"\\\\k<m>/\\\\k<y>\")",
			"\"06/2026\"\n"},
		{"backref_nonparticipating", "p \"ac\".gsub(/(a)(b)?(c)/, \"\\\\1\\\\2\\\\3\")", "\"ac\"\n"},
		{"backref_out_of_range", "p \"ab\".gsub(/(a)/, \"\\\\2\")", "\"b\"\n"},
		{"double_backslash", "p \"x\".gsub(/x/, \"\\\\\\\\\")", "\"\\\\\"\n"},
		{"amp_twice", "p \"a-b\".gsub(/-/, \"\\\\&\\\\&\")", "\"a--b\"\n"},
		{"prematch", "p \"abcd\".gsub(/b/, \"[\\\\`]\")", "\"a[a]cd\"\n"},
		{"postmatch", "p \"abcd\".gsub(/b/, \"[\\\\']\")", "\"a[cd]cd\"\n"},
		{"prematch_across", "p \"axbxc\".gsub(/x/, \"[\\\\`]\")", "\"a[a]b[axb]c\"\n"},
		{"malformed_k_no_bracket", "p \"x\".gsub(/x/, \"\\\\k\")", "\"\\\\k\"\n"},
		{"trailing_backslash", "p \"x\".gsub(/x/, \"a\\\\\")", "\"a\\\\\"\n"},
		{"backslash_other", "p \"x\".gsub(/x/, \"\\\\q\")", "\"\\\\q\"\n"},
		{"digit_then_literal", "p \"ab\".gsub(/(a)(b)/, \"\\\\12\")", "\"a2\"\n"},
		{"empty_pattern", `p "abc".gsub(//, "-")`, "\"-a-b-c-\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringGsubBlock(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"gsub_block", `p "hello".gsub(/l/) { |m| m.upcase }`, "\"heLLo\"\n"},
		{"gsub_block_compute", "p \"a1b2\".gsub(/\\d/) { |m| (m.to_i * 2).to_s }",
			"\"a2b4\"\n"},
		{"sub_block", `p "hello".sub(/l/) { |m| "[#{m}]" }`, "\"he[l]lo\"\n"},
		{"block_to_s_coerce", `p "5".gsub(/5/) { 42 }`, "\"42\"\n"},
		{"string_pattern_block", `p "hello".gsub("l") { |m| m.upcase }`, "\"heLLo\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringGsubStringPattern(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"literal_dot", `p "a.b".gsub(".", "X")`, "\"aXb\"\n"},
		{"literal_char", `p "aaa".gsub("a", "b")`, "\"bbb\"\n"},
		{"string_pattern_amp", "p \"abc\".gsub(\"b\", \"<\\\\&>\")", "\"a<b>c\"\n"},
		{"sub_string", `p "aaa".sub("a", "b")`, "\"baa\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringGsubErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"named_unknown", "\"x\".gsub(/x/, \"\\\\k<no>\")", "IndexError"},
		{"malformed_k_open", "\"x\".gsub(/x/, \"\\\\k<no\")", "RuntimeError"},
		{"no_replacement", `"x".gsub(/x/)`, "ArgumentError"},
		{"sub_no_replacement", `"x".sub(/x/)`, "ArgumentError"},
		{"nonstring_replacement", `"x".gsub(/x/, 123)`, "TypeError"},
		{"nonregex_pattern", `"x".gsub(123, "y")`, "TypeError"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q\n got err=%v\nwant contains %q", tc.src, err, tc.want)
			}
		})
	}
}

// TestStringGsubMultibyte confirms gsub is byte-correct on multibyte input with
// a literal multibyte pattern (matched substrings are representation-
// independent).
func TestStringGsubMultibyte(t *testing.T) {
	if got := eval(t, `p "café".gsub("é", "e")`); got != "\"cafe\"\n" {
		t.Errorf("multibyte gsub: got %q", got)
	}
	if got := eval(t, `p "héllo".gsub(/héllo/, "world")`); got != "\"world\"\n" {
		t.Errorf("multibyte regexp gsub: got %q", got)
	}
}
