package vm_test

import (
	"strings"
	"testing"
)

func TestStringSplitError(t *testing.T) {
	if err := runErr(t, `"a1b".split(123)`); err == nil ||
		!strings.Contains(err.Error(), "TypeError") {
		t.Errorf("split with non-regex pattern: got %v", err)
	}
}

// Phase 6d: String#split extended to accept a Regexp (or a literal String)
// pattern, with captured groups interpolated, an optional field limit, and the
// awk-style whitespace mode (no arg, nil, or " "). Pinned against MRI (Ruby 4.0).

func TestStringSplitRegexp(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"basic", `p "a1b2c3".split(/\d/)`, "[\"a\", \"b\", \"c\"]\n"},
		{"empty_field", `p "a,b,,c".split(/,/)`, "[\"a\", \"b\", \"\", \"c\"]\n"},
		{"trailing_stripped", `p "a,b,c,".split(/,/)`, "[\"a\", \"b\", \"c\"]\n"},
		{"trailing_multi", `p "a,b,,,".split(/,/)`, "[\"a\", \"b\"]\n"},
		{"empty_subject", `p "".split(/,/)`, "[]\n"},
		{"each_char", `p "abc".split(//)`, "[\"a\", \"b\", \"c\"]\n"},
		{"captures", `p "a1b2".split(/(\d)/)`, "[\"a\", \"1\", \"b\", \"2\"]\n"},
		{"capture_delims", `p "axbxc".split(/(x)/)`, "[\"a\", \"x\", \"b\", \"x\", \"c\"]\n"},
		{"nonparticipating", `p "a1b".split(/(\d)(x)?/)`, "[\"a\", \"1\", \"b\"]\n"},
		{"leading_empty", `p "xax".split(/x/)`, "[\"\", \"a\"]\n"},
		{"star_pattern", `p "axxb".split(/x*/)`, "[\"a\", \"b\"]\n"},
		{"empty_subject_empty_pat", `p "".split(//)`, "[]\n"},
		{"single_char_empty_pat", `p "a".split(//)`, "[\"a\"]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringSplitLimit(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"positive", `p "a1b2c3".split(/\d/, 2)`, "[\"a\", \"b2c3\"]\n"},
		{"positive_three", `p "a1b2c3d".split(/\d/, 3)`, "[\"a\", \"b\", \"c3d\"]\n"},
		{"limit_one", `p "a,b".split(/,/, 1)`, "[\"a,b\"]\n"},
		{"limit_with_captures", `p "a1b2".split(/(\d)/, 2)`, "[\"a\", \"1\", \"b2\"]\n"},
		{"zero_strips", `p "hello".split(/l/, 0)`, "[\"he\", \"\", \"o\"]\n"},
		{"negative_keeps", `p "a1b2c3".split(/\d/, -1)`, "[\"a\", \"b\", \"c\", \"\"]\n"},
		{"negative_trailing", `p "a,b,c,".split(/,/, -1)`, "[\"a\", \"b\", \"c\", \"\"]\n"},
		{"empty_pat_limit", `p "abc".split(//, 2)`, "[\"a\", \"bc\"]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringSplitWhitespace(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"no_arg", `p "a b  c".split`, "[\"a\", \"b\", \"c\"]\n"},
		{"space_string", `p " a b ".split(" ")`, "[\"a\", \"b\"]\n"},
		{"nil_pattern", `p "a b c".split(nil)`, "[\"a\", \"b\", \"c\"]\n"},
		{"regex_ws", `p "hello world".split(/\s+/)`, "[\"hello\", \"world\"]\n"},
		{"regex_ws_leading", `p "  a  b  ".split(/\s+/)`, "[\"\", \"a\", \"b\"]\n"},
		{"ws_limit", `p "a b c d".split(" ", 2)`, "[\"a\", \"b c d\"]\n"},
		{"ws_limit_leading", `p "  a b c".split(" ", 2)`, "[\"a\", \"b c\"]\n"},
		{"nil_limit", `p "a b c".split(nil, 2)`, "[\"a\", \"b c\"]\n"},
		{"all_whitespace", `p "  ".split(" ")`, "[]\n"},
		{"empty_no_arg", `p "".split`, "[]\n"},
		{"tabs_newlines", "p \"a\\tb\\nc\".split", "[\"a\", \"b\", \"c\"]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringSplitStringPattern(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"literal_comma", `p "a,b,c".split(",")`, "[\"a\", \"b\", \"c\"]\n"},
		{"literal_meta", `p "a.b.c".split(".")`, "[\"a\", \"b\", \"c\"]\n"},
		{"empty_string_chars", `p "test".split("")`, "[\"t\", \"e\", \"s\", \"t\"]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestStringSplitMultibyte confirms split is byte-correct on multibyte input
// with literal multibyte separators (substring comparison).
func TestStringSplitMultibyte(t *testing.T) {
	if got := eval(t, `p "café au lait".split(" ")`); got != "[\"café\", \"au\", \"lait\"]\n" {
		t.Errorf("multibyte whitespace split: got %q", got)
	}
	if got := eval(t, `p "a→b→c".split("→")`); got != "[\"a\", \"b\", \"c\"]\n" {
		t.Errorf("multibyte separator split: got %q", got)
	}
}
