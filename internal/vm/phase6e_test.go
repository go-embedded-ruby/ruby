package vm_test

import "testing"

// TestRegexpDotCharAdvance covers the engine's UTF-8 char-advancing dot (since
// the go-onigmo encoding-aware cursor): a bare `.` consumes a whole character.
func TestRegexpDotCharAdvance(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"dot_matches_char", `p("é" =~ /./)`, "0\n"},
		{"scan_dot", `p "café".scan(/./)`, "[\"c\", \"a\", \"f\", \"é\"]\n"},
		{"gsub_dot_count", `p "résumé".gsub(/./, "x")`, "\"xxxxxx\"\n"},
		{"scan_arrow", `p "a→b".scan(/./)`, "[\"a\", \"→\", \"b\"]\n"},
		{"ascii_range_skips_multibyte", `p "naïve".scan(/[a-z]/)`, "[\"n\", \"a\", \"v\", \"e\"]\n"},
		{"match_after_multibyte", `p("héllo" =~ /llo/)`, "2\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRegexpMultibyteClassMembers covers literal non-ASCII character-class
// members (since the go-onigmo multibyte-class release): [é]/[à-ï] match code
// points, and mixed ASCII/non-ASCII classes combine.
func TestRegexpMultibyteClassMembers(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"mixed_class", `p "café".scan(/[a-zé]/)`, "[\"c\", \"a\", \"f\", \"é\"]\n"},
		{"mb_range", `p "résumé".scan(/[à-ï]/)`, "[\"é\", \"é\"]\n"},
		{"greek_set", `p "αβγδ".scan(/[αβγ]/)`, "[\"α\", \"β\", \"γ\"]\n"},
		{"negated_mb", `p "héllo".scan(/[^l]/)`, "[\"h\", \"é\", \"o\"]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
