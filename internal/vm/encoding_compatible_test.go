package vm_test

import "testing"

// compatName evaluates Encoding.compatible?(a, b) for the given operand source
// expressions and returns the inspected result name (or nil). Every want below is
// verified byte-for-byte against MRI 4.0.6.
func compatName(t *testing.T, a, b string) string {
	t.Helper()
	src := "r = Encoding.compatible?(" + a + ", " + b + ")\np(r && r.name)\n"
	return eval(t, src)
}

func TestEncodingCompatibleEngine(t *testing.T) {
	// Byte-oriented content is built with String#b / #force_encoding so the test
	// does not depend on the source file's declared encoding.
	cases := []struct {
		name, a, b, want string
	}{
		// Identical encodings are always compatible (fast path).
		{"same_binary", `"abc".b`, `"def".b`, "\"ASCII-8BIT\"\n"},
		{"same_regexp_us_ascii", `/abc/`, `/def/`, "\"US-ASCII\"\n"},

		// An empty second String takes the first's encoding.
		{"empty_second", `"x".dup.force_encoding("UTF-8")`, `""`, "\"UTF-8\"\n"},

		// An empty first String: first's encoding when first is ASCII-compatible and
		// the second is 7-bit; otherwise the second's.
		{"empty_first_second_7bit", `"".dup.force_encoding("UTF-8")`, `"\x01\x01".b`, "\"UTF-8\"\n"},
		{"empty_first_second_not7bit", `"".dup.force_encoding("UTF-8")`, `"\x81".b`, "\"ASCII-8BIT\"\n"},
		{"empty_first_not_asciicompat", `"".dup.force_encoding("UTF-16BE")`, `"ab".dup.force_encoding("US-ASCII")`, "\"US-ASCII\"\n"},

		// A non-ASCII-compatible operand (that is not identical) is incompatible.
		{"non_asciicompat_string", `"a".dup.force_encoding("UTF-8")`, `"bb".dup.force_encoding("UTF-16BE")`, "nil\n"},

		// Two Strings: the 7-bit one adopts the other's encoding; two non-7-bit are
		// incompatible.
		{"two_strings_second_7bit", `"\x81".b`, `"abc".dup.force_encoding("UTF-8")`, "\"ASCII-8BIT\"\n"},
		{"two_strings_first_7bit", `"abc".dup.force_encoding("UTF-8")`, `"\x81".b`, "\"ASCII-8BIT\"\n"},
		{"two_strings_both_not7bit", `"\x81".b`, `"あ".dup.force_encoding("UTF-8")`, "nil\n"},

		// Encoding, Encoding operands: US-ASCII is the "same as contents" special.
		{"enc_enc_second_us_ascii", `Encoding::UTF_8`, `Encoding::US_ASCII`, "\"UTF-8\"\n"},
		{"enc_enc_first_us_ascii", `Encoding::US_ASCII`, `Encoding::EUC_JP`, "\"EUC-JP\"\n"},
		{"enc_enc_equal", `Encoding::UTF_8`, `Encoding::UTF_8`, "\"UTF-8\"\n"},
		{"enc_enc_differ", `Encoding::UTF_8`, `Encoding::EUC_JP`, "nil\n"},
		{"enc_enc_dummy", `Encoding::UTF_8`, `Encoding::UTF_7`, "nil\n"},

		// Regexp operands report their source encoding.
		{"string_regexp_eucjp", `"hello".dup.force_encoding("utf-8")`, `Regexp.new("\xa4\xa2".dup.force_encoding("euc-jp"))`, "\"EUC-JP\"\n"},
		{"regexp_string_same_eucjp", `Regexp.new("\xa4\xa2".dup.force_encoding("euc-jp"))`, `"hi".dup.force_encoding("euc-jp")`, "\"EUC-JP\"\n"},
		{"regexp_binary_source", `Regexp.new("\xff".b)`, `/abc/`, "\"ASCII-8BIT\"\n"},
		{"regexp_noencoding", `Regexp.new("\xff".b, Regexp::NOENCODING)`, `/abc/`, "\"ASCII-8BIT\"\n"},
		{"regexp_literal_nonascii", `/あ/`, `"x".dup.force_encoding("utf-8")`, "\"UTF-8\"\n"},

		// Symbol operands: US-ASCII when ASCII-only, else UTF-8.
		{"symbol_us_ascii_string_binary", `:abc`, `"\x81".b`, "\"ASCII-8BIT\"\n"},
		{"symbol_nonascii_utf8", `"あ".to_sym`, `"x".dup.force_encoding("utf-8")`, "\"UTF-8\"\n"},

		// String vs Encoding US-ASCII keeps the String's own encoding.
		{"string_binary_enc_us_ascii", `"\x81".b`, `Encoding::US_ASCII`, "\"ASCII-8BIT\"\n"},

		// Objects without an encoding make the pair incompatible.
		{"object_first", `Object.new`, `"x"`, "nil\n"},
		{"object_second", `"x"`, `Object.new`, "nil\n"},
		{"nil_nil", `nil`, `nil`, "nil\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compatName(t, tc.a, tc.b); got != tc.want {
				t.Errorf("Encoding.compatible?(%s, %s) = %q, want %q", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestEncodingToSIsAliasOfName checks that Encoding#to_s and #name are one method
// record (a true alias), matching MRI.
func TestEncodingToSIsAliasOfName(t *testing.T) {
	if got := eval(t, "p(Encoding.instance_method(:to_s) == Encoding.instance_method(:name))\n"); got != "true\n" {
		t.Errorf("to_s alias of name = %q, want %q", got, "true\n")
	}
	if got := eval(t, `p Encoding::UTF_8.to_s`); got != "\"UTF-8\"\n" {
		t.Errorf("Encoding::UTF_8.to_s = %q, want %q", got, "\"UTF-8\"\n")
	}
}

// TestRegexpEncodingFromSource covers Regexp#encoding reporting the source
// String's encoding for a runtime Regexp.new.
func TestRegexpEncodingFromSource(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p(/abc/.encoding.name)`, "\"US-ASCII\"\n"},
		{`p(Regexp.new("\xa4\xa2".dup.force_encoding("euc-jp")).encoding.name)`, "\"EUC-JP\"\n"},
		{`p(Regexp.new("\xff".b).encoding.name)`, "\"ASCII-8BIT\"\n"},
		{`p(Regexp.new("あ".dup.force_encoding("utf-8")).encoding.name)`, "\"UTF-8\"\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
		}
	}
}
