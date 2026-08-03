package vm_test

import (
	"strings"
	"testing"
)

// TestEncoding covers the String encoding tag (default UTF-8, ASCII-8BIT binary)
// and the Encoding class. Asserted against MRI Ruby 4.0.5.
func TestEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		// Default encoding and the Encoding objects (interned, == by identity).
		{`p ["abc".encoding.name, "café".encoding.to_s, "x".encoding == Encoding::UTF_8]`, "[\"UTF-8\", \"UTF-8\", true]\n"},
		{`p [Encoding::UTF_8, Encoding::ASCII_8BIT, Encoding::BINARY == Encoding::ASCII_8BIT]`, "[#<Encoding:UTF-8>, #<Encoding:BINARY (ASCII-8BIT)>, true]\n"},
		// length counts characters for UTF-8 but bytes for a binary string.
		{`p ["café".length, "café".b.length, "café".bytesize]`, "[4, 5, 5]\n"},
		{`s = "café".dup; s.force_encoding("ASCII-8BIT"); p [s.encoding.name, s.length]`, "[\"ASCII-8BIT\", 5]\n"},
		// force_encoding accepts a name (case-insensitively / aliases) or an Encoding.
		{`p ["x".dup.force_encoding("utf-8").encoding.name, "x".dup.force_encoding(Encoding::BINARY).encoding.name, "x".dup.force_encoding("us-ascii").encoding.name]`, "[\"UTF-8\", \"ASCII-8BIT\", \"US-ASCII\"]\n"},
		// force_encoding resolves aliases case-insensitively and rejects unknown names.
		{`p ["x".dup.force_encoding("BINARY").encoding.name, "x".dup.force_encoding("eucJP").encoding.name]`, "[\"ASCII-8BIT\", \"EUC-JP\"]\n"},
		// b returns a binary copy; the original is untouched.
		{`s = "café"; b = s.b; p [b.encoding.name, b.length, s.encoding.name, s.length]`, "[\"ASCII-8BIT\", 5, \"UTF-8\", 4]\n"},
		// ascii_only? and valid_encoding?.
		{`p ["abc".ascii_only?, "café".ascii_only?, "café".valid_encoding?, "café".b.valid_encoding?]`, "[true, false, true, true]\n"},
		// Encoding#name / to_s (via puts) / inspect method / == with a non-encoding /
		// a pass-through name.
		{`puts Encoding::UTF_8`, "UTF-8\n"},
		{`p [Encoding::UTF_8.inspect, Encoding::UTF_8 == :foo]`, "[\"#<Encoding:UTF-8>\", false]\n"},
		{`p ["x".dup.force_encoding("ISO-8859-1").encoding.name, (Encoding::UTF_8 ? :t : :f)]`, "[\"ISO-8859-1\", :t]\n"},
		// random_bytes is binary, so length == the requested count.
		{`require "securerandom"; b = SecureRandom.random_bytes(5); p [b.length, b.encoding.name]`, "[5, \"ASCII-8BIT\"]\n"},
		// String#[] / #slice on a binary (ASCII-8BIT) string indexes by BYTES and
		// keeps the result binary — matching MRI, where index/length are byte
		// offsets on an ASCII-8BIT string. "café".b is 5 bytes; [3, 2] takes the
		// two bytes of "é", and each slice stays ASCII-8BIT.
		{`s = "café".b; p [s[3, 2].bytesize, s[3, 2].encoding.name]`, "[2, \"ASCII-8BIT\"]\n"},
		{`s = "café".b; p [s.slice(0, 5).bytesize, s.slice(0, 5).encoding.name]`, "[5, \"ASCII-8BIT\"]\n"},
		{`s = "é".b; p [s[0].bytesize, s[1].bytesize, s[1].encoding.name]`, "[1, 1, \"ASCII-8BIT\"]\n"},
		{`s = "café".b; p [s[0..2].bytesize, s[0..2].encoding.name]`, "[3, \"ASCII-8BIT\"]\n"},
		// The UTF-8 default is unchanged: [] indexes by character.
		{`p ["café"[3, 1], "café"[0, 3], "café"[3].bytesize]`, "[\"é\", \"caf\", 2]\n"},
		// A binary index past the byte length is nil (byte-range, not char-range).
		{`s = "é".b; p s[2]`, "nil\n"},
		// The substring form s[sub] is byte-wise Contains either way: present →
		// the substring, absent → nil.
		{`s = "café".b; p [s["af"], s["zz"]]`, "[\"af\", nil]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// force_encoding with a non-String/Encoding argument raises TypeError.
	if err := runErr(t, `"x".dup.force_encoding(123)`); err == nil || !strings.Contains(err.Error(), "into String") {
		t.Errorf("force_encoding(123) err=%v, want a TypeError", err)
	}
	// force_encoding with an unknown name raises ArgumentError.
	if err := runErr(t, `"x".dup.force_encoding("no-such-enc")`); err == nil || !strings.Contains(err.Error(), "unknown encoding name - no-such-enc") {
		t.Errorf("force_encoding(bogus) err=%v, want an ArgumentError", err)
	}
}

// TestEncodingRegistry covers the Encoding registry surface (list / name_list /
// aliases / find / named constants / names / dummy? / ascii_compatible?).
// Asserted against MRI Ruby 3.4/4.0.
func TestEncodingRegistry(t *testing.T) {
	cases := []struct{ src, want string }{
		// The registry has the full standard set; every list entry is an Encoding,
		// listed once, and name_list = names + aliases.
		{`p [Encoding.list.size, Encoding.list.all? { |e| e.is_a?(Encoding) }, Encoding.list.map(&:name) == Encoding.list.map(&:name).uniq]`, "[103, true, true]\n"},
		{`p [Encoding.name_list.size, Encoding.aliases.size]`, "[174, 71]\n"},
		{`p Encoding.list.include?(Encoding.default_external)`, "true\n"},
		{`p Encoding.list.any? { |e| e.dummy? }`, "true\n"},
		// name_list includes every alias and every encoding name.
		{`p Encoding.aliases.keys.all? { |a| Encoding.name_list.include?(a) }`, "true\n"},
		{`p Encoding.list.all? { |e| Encoding.name_list.include?(e.name) }`, "true\n"},
		// aliases map alias => canonical, and resolve to the same object as find.
		{`p [Encoding.aliases["BINARY"], Encoding.aliases["ASCII"]]`, "[\"ASCII-8BIT\", \"US-ASCII\"]\n"},
		{`p Encoding.aliases.all? { |a, o| Encoding.find(a) == Encoding.find(o) }`, "true\n"},
		{`p Encoding.aliases["external"] == Encoding.default_external.name`, "true\n"},
		// Named constants and their flags.
		{`p [Encoding::Shift_JIS.name, Encoding::UTF_16LE.name, Encoding::ISO_8859_1.name]`, "[\"Shift_JIS\", \"UTF-16LE\", \"ISO-8859-1\"]\n"},
		{`p [Encoding::UTF_8.dummy?, Encoding::UTF_7.dummy?, Encoding::UTF_16.dummy?, Encoding::UTF_32.dummy?]`, "[false, true, true, true]\n"},
		{`p [Encoding::UTF_8.ascii_compatible?, Encoding::UTF_16LE.ascii_compatible?, Encoding::US_ASCII.ascii_compatible?]`, "[true, false, true]\n"},
		{`p Encoding.list.select(&:dummy?).all? { |e| !e.ascii_compatible? }`, "true\n"},
		// names: first is the canonical name; all are Strings.
		{`p Encoding::UTF_8.names`, "[\"UTF-8\", \"CP65001\", \"locale\", \"external\", \"filesystem\"]\n"},
		{`p Encoding.name_list.all? { |n| e = Encoding.find(n); e.names.first == e.name && e.names.all? { |x| x.is_a?(String) } }`, "true\n"},
		// find: case-insensitive, alias-aware, pass-through Encoding, to_str duck typing.
		{`p [Encoding.find("eucjp").name, Encoding.find("UTF-8").equal?(Encoding.find("utf-8"))]`, "[\"EUC-JP\", true]\n"},
		{`p Encoding.find(Encoding::UTF_8) == Encoding::UTF_8`, "true\n"},
		{`o = Object.new; def o.to_str; "Shift_JIS"; end; p Encoding.find(o).name`, "\"Shift_JIS\"\n"},
		{`p Encoding.find("locale").name`, "\"UTF-8\"\n"},
		{`p Encoding.locale_charmap`, "\"UTF-8\"\n"},
		// inspect: dummy encodings show "(dummy)"; ASCII-8BIT shows BINARY.
		{`p [Encoding::UTF_16.inspect, Encoding::ASCII_8BIT.inspect, Encoding::UTF_8.inspect]`, "[\"#<Encoding:UTF-16 (dummy)>\", \"#<Encoding:BINARY (ASCII-8BIT)>\", \"#<Encoding:UTF-8>\"]\n"},
		// default_external / default_internal and their setters.
		{`p [Encoding.default_external.name, Encoding.default_internal]`, "[\"UTF-8\", nil]\n"},
		{`Encoding.default_external = Encoding::US_ASCII; p Encoding.default_external.name`, "\"US-ASCII\"\n"},
		{`p (Encoding.default_internal = Encoding::UTF_8).name`, "\"UTF-8\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// find with a Symbol raises TypeError (Symbol has no #to_str); find with an
	// unknown name raises ArgumentError.
	if err := runErr(t, `Encoding.find(:"utf-8")`); err == nil || !strings.Contains(err.Error(), "into String") {
		t.Errorf("find(:sym) err=%v, want a TypeError", err)
	}
	if err := runErr(t, `Encoding.find("dh2dh278d")`); err == nil || !strings.Contains(err.Error(), "unknown encoding name - dh2dh278d") {
		t.Errorf("find(bogus) err=%v, want an ArgumentError", err)
	}
}
