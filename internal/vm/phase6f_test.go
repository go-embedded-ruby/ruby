package vm_test

import "testing"

// TestRegexpMatchGlobals covers $~, $1..$N, $&, $`, $' fed by the last match
// (a VM-global last-match, not frame-local — a documented simplification).
func TestRegexpMatchGlobals(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"numbered", "\"2026-06-15\" =~ /(\\d+)-(\\d+)-(\\d+)/\np $1\np $2\np $3", "\"2026\"\n\"06\"\n\"15\"\n"},
		{"whole_match", "\"abc\" =~ /b/\np $&", "\"b\"\n"},
		{"matchdata", "\"xy\" =~ /(x)(y)/\np $~[1]\np $~[2]", "\"x\"\n\"y\"\n"},
		{"pre_post", "\"hello world\" =~ /o w/\np $`\np $'", "\"hell\"\n\"orld\"\n"},
		{"no_match_group", "\"abc\" =~ /z/\np $1", "nil\n"},
		{"no_match_tilde", "\"abc\" =~ /z/\np $~", "nil\n"},
		{"no_match_amp", "\"abc\" =~ /z/\np $&", "nil\n"},
		{"no_match_pre", "\"abc\" =~ /z/\np $`", "nil\n"},
		{"no_match_post", "\"abc\" =~ /z/\np $'", "nil\n"},
		{"group_overflow", "\"ab\" =~ /(a)(b)/\np $9", "nil\n"},
		{"via_match", "\"foo123\".match(/(\\d+)/)\np $1", "\"123\"\n"},
		{"undefined_global", "p $totally_undefined_xyz", "nil\n"},
		{"idiomatic_if", "if \"x=42\" =~ /=(\\d+)/\np $1.to_i\nend", "42\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
