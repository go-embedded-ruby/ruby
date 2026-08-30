package vm_test

import (
	"strings"
	"testing"
)

// TestStringCaseMapping locks in MRI 4.0.x String#upcase/#downcase/#swapcase/
// #capitalize behaviour, including full (multi-character) Unicode case mapping
// and the :ascii, :fold, :turkic and :lithuanian options. Every expected value
// below was verified byte-for-byte against MRI.
func TestStringCaseMapping(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		// Full Unicode case mapping producing multi-character sequences.
		{"upcase_sharp_s", `p "aßet".upcase`, "\"ASSET\"\n"},
		{"upcase_umlauts", `p "äöü".upcase`, "\"ÄÖÜ\"\n"},
		{"upcase_dotted_I_unchanged", `p "İ".upcase`, "\"İ\"\n"},
		{"downcase_sharp_s_stays", `p "ß".downcase`, "\"ß\"\n"},
		{"downcase_dotted_I_full", `p "İS".downcase`, "\"i̇s\"\n"},
		{"swapcase_sharp_s", `p "Aßet".swapcase`, "\"aSSET\"\n"},
		{"swapcase_mixed", `p "cYbEr_PuNk11".swapcase`, "\"CyBeR_pUnK11\"\n"},
		{"capitalize_sharp_s", `p "ß".capitalize`, "\"Ss\"\n"},
		{"capitalize_sharp_s_tail", `p "ßeT".capitalize`, "\"Sset\"\n"},
		{"capitalize_empty", `p "".capitalize`, "\"\"\n"},
		{"capitalize_digit_first", `p "1a".capitalize`, "\"1a\"\n"},

		// :ascii option — only ASCII letters are mapped.
		{"upcase_ascii", `p "aßet".upcase(:ascii)`, "\"AßET\"\n"},
		{"downcase_ascii", `p "CÅR".downcase(:ascii)`, "\"cÅr\"\n"},
		{"swapcase_ascii", `p "aßet".swapcase(:ascii)`, "\"AßET\"\n"},
		{"swapcase_ascii_upper", `p "AbC".swapcase(:ascii)`, "\"aBc\"\n"},
		{"capitalize_ascii_nonascii_first", `p "ßet".capitalize(:ascii)`, "\"ßet\"\n"},
		{"capitalize_ascii_substr", `p "garçon"[1...-1].capitalize(:ascii)`, "\"Arço\"\n"},

		// :turkic option — dotted/dotless I semantics.
		{"upcase_turkic_i", `p "i".upcase(:turkic)`, "\"İ\"\n"},
		{"upcase_turkic_dotless", `p "ı".upcase(:turkic)`, "\"I\"\n"},
		{"downcase_turkic_dotted", `p "İ".downcase(:turkic)`, "\"i\"\n"},
		{"downcase_turkic_I", `p "I".downcase(:turkic)`, "\"ı\"\n"},
		{"swapcase_turkic", `p "aiS".swapcase(:turkic)`, "\"Aİs\"\n"},
		{"capitalize_turkic", `p "iSa".capitalize(:turkic)`, "\"İsa\"\n"},
		{"capitalize_turkic_dotless", `p "ısa".capitalize(:turkic)`, "\"Isa\"\n"},

		// :lithuanian option — currently the same as full mapping, unless it is
		// combined with :turkic (in which case Turkic semantics apply).
		{"upcase_lithuanian", `p "iß".upcase(:lithuanian)`, "\"ISS\"\n"},
		{"upcase_lithuanian_turkic", `p "iß".upcase(:lithuanian, :turkic)`, "\"İSS\"\n"},
		{"downcase_lithuanian", `p "İS".downcase(:lithuanian)`, "\"i̇s\"\n"},
		{"downcase_lithuanian_turkic", `p "İS".downcase(:lithuanian, :turkic)`, "\"is\"\n"},
		{"swapcase_lithuanian", `p "Iß".swapcase(:lithuanian)`, "\"iSS\"\n"},
		{"swapcase_lithuanian_turkic", `p "iS".swapcase(:lithuanian, :turkic)`, "\"İs\"\n"},
		{"capitalize_lithuanian", `p "iß".capitalize(:lithuanian)`, "\"Iß\"\n"},
		{"capitalize_lithuanian_turkic", `p "iß".capitalize(:lithuanian, :turkic)`, "\"İß\"\n"},
		{"upcase_turkic_lithuanian", `p "i".upcase(:turkic, :lithuanian)`, "\"İ\"\n"},

		// :fold option — full case folding, downcase only.
		{"downcase_fold_sharp_s", `p "ß".downcase(:fold)`, "\"ss\"\n"},

		// Non-ASCII-compatible encodings are transcoded through UTF-8.
		{"upcase_utf16le", `p "äöü".encode("utf-16le").upcase.bytes`, "[196, 0, 214, 0, 220, 0]\n"},
		{"swapcase_utf16le", `p "äÖü".encode("utf-16le").swapcase.bytes`, "[196, 0, 246, 0, 220, 0]\n"},

		// Invalid bytes in a binary string pass through untouched.
		{"upcase_invalid_byte", `p "A\xFFb".b.upcase.bytes`, "[65, 255, 66]\n"},

		// Bang variants: in place, nil when unchanged, otherwise the receiver.
		{"upcase_bang_changed", `p "abc".dup.upcase!`, "\"ABC\"\n"},
		{"upcase_bang_unchanged", `p "HELLO".dup.upcase!`, "nil\n"},
		{"downcase_bang_fold", `s = "ß".dup; s.downcase!(:fold); p s`, "\"ss\"\n"},
		{"capitalize_bang_multi", `s = "ß".dup; s.capitalize!; p s`, "\"Ss\"\n"},
		{"swapcase_bang_turkic", `s = "aiS".dup; s.swapcase!(:turkic); p s`, "\"Aİs\"\n"},

		// Metadata after a multi-character expansion.
		{"upcase_metadata_size", `p "aßet".upcase.size`, "5\n"},
		{"upcase_metadata_ascii_only", `p "aßet".upcase.ascii_only?`, "true\n"},

		// Symbol case methods share the same full-mapping engine.
		{"symbol_upcase", `p :aßet.upcase`, ":ASSET\n"},
		{"symbol_capitalize", `p :ßet.capitalize`, ":Sset\n"},
		{"symbol_swapcase", `p :Aßet.swapcase`, ":aSSET\n"},
		{"symbol_downcase", `p :ÄÖÜ.downcase.to_s`, "\"äöü\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestStringCaseMappingErrors locks in the ArgumentError/FrozenError paths of
// the case-mapping option parser and the bang methods.
func TestStringCaseMappingErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		// :fold is only valid for downcasing.
		{"upcase_fold", `"abc".upcase(:fold)`, "ArgumentError"},
		{"swapcase_fold", `"abc".swapcase(:fold)`, "ArgumentError"},
		{"capitalize_fold", `"abc".capitalize(:fold)`, "ArgumentError"},
		{"upcase_bang_fold", `"abc".dup.upcase!(:fold)`, "ArgumentError"},

		// Unknown / invalid options.
		{"invalid_option", `"abc".upcase(:invalid_option)`, "ArgumentError"},
		{"non_symbol_option", `"abc".upcase(5)`, "ArgumentError"},

		// :ascii cannot be combined with another option.
		{"ascii_plus_turkic", `"abc".upcase(:ascii, :turkic)`, "ArgumentError"},

		// Only :turkic+:lithuanian may be combined; the second must match.
		{"turkic_bad_second", `"abc".upcase(:turkic, :ascii)`, "ArgumentError"},
		{"turkic_nonsym_second", `"abc".upcase(:turkic, 5)`, "ArgumentError"},
		{"lithuanian_bad_second", `"abc".upcase(:lithuanian, :ascii)`, "ArgumentError"},
		{"lithuanian_nonsym_second", `"abc".upcase(:lithuanian, 5)`, "ArgumentError"},

		// More than two options.
		{"too_many_options", `"abc".upcase(:turkic, :lithuanian, :ascii)`, "ArgumentError"},

		// Frozen receiver raises even when no change would be made.
		{"frozen_mixed", `"HeLlo".freeze.upcase!`, "FrozenError"},
		{"frozen_no_change", `"HELLO".freeze.upcase!`, "FrozenError"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}
