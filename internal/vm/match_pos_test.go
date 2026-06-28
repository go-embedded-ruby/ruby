package vm_test

import "testing"

// Regexp#match(str, pos) and String#match(re, pos) start scanning at character
// offset pos, reporting positions against the full subject (the behaviour
// StringScanner#scan relies on at a non-zero position).
func TestMatchWithPosition(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"regexp_match_pos", `p(/[A-Z]\w*/.match("Variant[ScalarData", 8)[0])`, "\"ScalarData\"\n"},
		{"regexp_match_pos_begin", `p(/[A-Z]\w*/.match("Variant[ScalarData", 8).begin(0))`, "8\n"},
		{"regexp_match_pos_end", `p(/[A-Z]\w*/.match("Variant[ScalarData", 8).end(0))`, "18\n"},
		{"regexp_match_pre", `p(/[A-Z]\w*/.match("Variant[ScalarData", 8).pre_match)`, "\"Variant[\"\n"},
		{"regexp_match_post", `p(/[A-Z]\w*/.match("Variant[ScalarData", 8).post_match)`, "\"\"\n"},
		{"string_match_pos", `p "hello world".match(/\w+/, 6)[0]`, "\"world\"\n"},
		{"string_match_pos_begin", `p "hello world".match(/\w+/, 6).begin(0)`, "6\n"},
		{"match_pos_zero", `p(/\w+/.match("abc def", 0)[0])`, "\"abc\"\n"},
		{"match_pos_negative", `p(/\w+/.match("abc def", -3)[0])`, "\"def\"\n"},
		{"match_pos_no_match", `p(/x/.match("abc", 1))`, "nil\n"},
		{"match_pos_at_end", `p(/x/.match("abc", 3))`, "nil\n"},
		{"match_pos_oob", `p(/a/.match("abc", 5))`, "nil\n"},
		{"match_pos_neg_oob", `p(/a/.match("abc", -5))`, "nil\n"},
		{"match_pos_multibyte", `m = /\w+/.match("ab café xyz", 3); p [m[0], m.begin(0)]`, "[\"caf\", 3]\n"},
		// StringScanner over the path that needs position-aware scanning.
		{"scanner_at_pos", `require "strscan"; s = StringScanner.new("Variant[ScalarData"); s.pos = 8; p s.scan(/[A-Z]\w*/)`, "\"ScalarData\"\n"},
		{"scanner_pos_after", `require "strscan"; s = StringScanner.new("Variant[ScalarData"); s.pos = 8; s.scan(/[A-Z]\w*/); p s.pos`, "18\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("eval(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}
