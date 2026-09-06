package vm_test

import (
	"strings"
	"testing"
)

// TestEncodingResidualsWave13 covers the encoding-subsystem conformance fixes of
// wave 13: the dynamic Encoding.find names ("external"/"filesystem"/"locale" ->
// default_external, "internal" -> default_internal), Encoding.default_external=
// rejecting nil, and the macCyrillic (Macintosh Cyrillic) transcoding codec.
// Every expectation is asserted against MRI Ruby 4.0.6.
func TestEncodingResidualsWave13(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// Encoding.find resolves the dynamic external/filesystem/locale names to the
		// current default_external, tracking the setter.
		{"find_external_default", `p Encoding.find("external").name`, "\"UTF-8\"\n"},
		{"find_locale_default", `p Encoding.find("locale").name`, "\"UTF-8\"\n"},
		{"find_filesystem_default", `p Encoding.find("filesystem").name`, "\"UTF-8\"\n"},
		{"find_external_tracks_setter",
			`Encoding.default_external = Encoding::SHIFT_JIS
p [Encoding.find("external").name, Encoding.find("filesystem").name]`,
			"[\"Shift_JIS\", \"Shift_JIS\"]\n"},
		{"find_external_case_insensitive", `p Encoding.find("EXTERNAL").name`, "\"UTF-8\"\n"},
		// Encoding.find("internal") is default_internal: nil when unset, the encoding
		// once set.
		{"find_internal_nil", `p Encoding.find("internal")`, "nil\n"},
		{"find_internal_set",
			`Encoding.default_internal = Encoding::US_ASCII
p Encoding.find("internal").name`,
			"\"US-ASCII\"\n"},
		// macCyrillic (Macintosh Cyrillic) transcodes via golang.org/x/text: a Cyrillic
		// letter encodes to its single mac byte, an undefined character raises.
		{"maccyrillic_encode", `p "А".encode("macCyrillic").bytes`, "[128]\n"},
		{"maccyrillic_converter",
			`ec = Encoding::Converter.new("UTF-8", Encoding.find("macCyrillic"))
p ec.convert("А").bytes`,
			"[128]\n"},
		// Encoding::Converter#primitive_convert carries output that did not fit the
		// destination to the next call: successive calls with a growing byte cap flush
		// the held byte before converting the next character (MRI's output-buffer
		// carryover), so three one-byte conversions accumulate to "aabbb".
		{"primconv_carryover",
			`ec = Encoding::Converter.new("utf-8", "iso-8859-1")
dest = +"aa"
r1 = ec.primitive_convert(+"b", dest, nil, 0)
r2 = ec.primitive_convert(+"b", dest, nil, 1)
r3 = ec.primitive_convert(+"b", dest, nil, 2)
p [r1, r2, r3, dest]`,
			"[:destination_buffer_full, :destination_buffer_full, :finished, \"aabbb\"]\n"},
		// A character whose encoding is larger than the destination cap returns
		// :destination_buffer_full with the whole source consumed (the produced bytes
		// are held), matching MRI.
		{"primconv_buffer_full_clears_source",
			`ec = Encoding::Converter.new("utf-8", "iso-2022-jp")
s = +"\u{9999}"
r = ec.primitive_convert(s, +"", 0, 2)
p [r, s]`,
			"[:destination_buffer_full, \"\"]\n"},
		// UTF8-MAC (HFS+ NFD) is a valid Converter destination: a precomposed
		// character is emitted in canonical decomposition (é -> "e" + combining acute),
		// while an ASCII run is unchanged.
		{"utf8mac_encode_nfd",
			`ec = Encoding::Converter.new("UTF-8", "UTF8-MAC")
p ec.convert("é").bytes`,
			"[101, 204, 129]\n"},
		{"utf8mac_encode_ascii",
			`ec = Encoding::Converter.new("UTF-8", "UTF8-MAC")
p ec.convert("abc").bytes`,
			"[97, 98, 99]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}

	// Error paths: default_external = nil raises ArgumentError (not the TypeError the
	// generic coercion would give), and an undefined macCyrillic character raises
	// Encoding::UndefinedConversionError.
	errCases := []struct{ name, src, want string }{
		{"default_external_nil", `Encoding.default_external = nil`, "default external can not be nil"},
		{"maccyrillic_undef", `"\u{6543}".encode("macCyrillic")`, "from UTF-8 to macCyrillic"},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}
