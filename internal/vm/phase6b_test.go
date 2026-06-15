package vm_test

import (
	"strings"
	"testing"
)

// Phase 6b: String#scan. Behaviour pinned against MRI (Ruby 4.0). With no
// capture groups each result is the whole match; with groups each result is the
// array of captures. The block form yields each result and returns the
// receiver.

func TestStringScan(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"no_groups", `p "hello world".scan(/\w+/)`, "[\"hello\", \"world\"]\n"},
		{"groups", `p "a1b2c3".scan(/([a-z])(\d)/)`,
			"[[\"a\", \"1\"], [\"b\", \"2\"], [\"c\", \"3\"]]\n"},
		{"single_group", `p "a1b2".scan(/([a-z])/)`, "[[\"a\"], [\"b\"]]\n"},
		{"repeated", `p "aaa".scan(/a/)`, "[\"a\", \"a\", \"a\"]\n"},
		{"empty_subject", `p "".scan(/x/)`, "[]\n"},
		{"no_match", `p "abc".scan(/z/)`, "[]\n"},
		{"named_groups", `p "a1b2".scan(/(?<l>[a-z])(?<n>\d)/)`,
			"[[\"a\", \"1\"], [\"b\", \"2\"]]\n"},
		{"optional_nil", `p "ab".scan(/(a)(b)?/)`, "[[\"a\", \"b\"]]\n"},
		{"optional_nonparticipating", `p "a".scan(/(a)(b)?/)`, "[[\"a\", nil]]\n"},
		{"empty_pattern", `p "abc".scan(//)`, "[\"\", \"\", \"\", \"\"]\n"},
		{"star_empties", `p "a.b.c".scan(/\w*/)`,
			"[\"a\", \"\", \"b\", \"\", \"c\", \"\"]\n"},
		{"nonspace", `p "x y  z".scan(/\S+/)`, "[\"x\", \"y\", \"z\"]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringScanStringPattern(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"literal_dot", `p "a.b.c".scan(".")`, "[\".\", \".\"]\n"},
		{"literal_digit", `p "a1b2".scan("1")`, "[\"1\"]\n"},
		{"meta_star", `p "a.b*c".scan("*")`, "[\"*\"]\n"},
		{"meta_plus", `p "a+b".scan("+")`, "[\"+\"]\n"},
		{"meta_paren", `p "a(b)c".scan("(")`, "[\"(\"]\n"},
		{"meta_bracket", `p "a[b]".scan("[")`, "[\"[\"]\n"},
		{"meta_caret", `p "a^b".scan("^")`, "[\"^\"]\n"},
		{"meta_dollar", `p "a$b".scan("$")`, "[\"$\"]\n"},
		{"meta_backslash", `p "a\\b".scan("\\")`, "[\"\\\\\"]\n"},
		{"tab", `p "a\tb\tc".scan("\t")`, "[\"\\t\", \"\\t\"]\n"},
		{"dash", `p "a-b-c".scan("-")`, "[\"-\", \"-\"]\n"},
		{"space", `p "a b".scan(" ")`, "[\" \"]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringScanBlock(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"no_groups_yield", `"a1b2".scan(/[a-z]\d/) { |m| print "[#{m}]" }`, "[a1][b2]"},
		{"returns_self", `r = "ab".scan(/x/) { |m| }; print r`, "ab"},
		{"returns_self_match", "p(\"a1b2\".scan(/[a-z]\\d/) { |m| })", "\"a1b2\"\n"},
		{"groups_destructure", `"a1b2".scan(/([a-z])(\d)/) { |a, b| print "#{a}=#{b} " }`,
			"a=1 b=2 "},
		{"single_group_array", `"a1b2".scan(/([a-z])/) { |x| print "#{x}." }`,
			"[\"a\"].[\"b\"]."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestStringScanMultibyte confirms scan is byte-correct on multibyte input when
// the pattern is itself a (multibyte) literal: matched substrings are
// representation-independent. (A bare `.` in the underlying byte-oriented
// engine matches one byte, not one character, so multibyte `.`-class patterns
// are out of scope here — that is an engine property, documented in regexp.go.)
func TestStringScanMultibyte(t *testing.T) {
	if got := eval(t, `p "héllo héllo".scan(/héllo/)`); got != "[\"héllo\", \"héllo\"]\n" {
		t.Errorf("multibyte literal scan: got %q", got)
	}
	if got := eval(t, `p "café".scan("é")`); got != "[\"é\"]\n" {
		t.Errorf("multibyte string-literal scan: got %q", got)
	}
}

func TestStringScanError(t *testing.T) {
	if err := runErr(t, `"x".scan(123)`); err == nil ||
		!strings.Contains(err.Error(), "TypeError") {
		t.Errorf("scan with non-regex: got %v", err)
	}
}
