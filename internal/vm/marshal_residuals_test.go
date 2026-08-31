package vm_test

import (
	"strings"
	"testing"
)

// TestMarshalResiduals covers the Marshal conformance residuals fixed on top of
// the earlier passes: Data (Ruby 3.2+ immutable value objects) dump/load via the
// 'S' container, and the Regexp encoding model (the option byte's
// FIXEDENCODING / NOENCODING bits and the recorded source encoding). Every
// byte-exact expectation was captured from MRI Ruby 4.0
// (`ruby -e 'p Marshal.dump(x).bytes'`).
func TestMarshalResiduals(t *testing.T) {
	cases := []struct{ src, want string }{
		// --- Data: 'S' container, member name/value pairs -----------------
		{`M = Data.define(:amount, :unit); p Marshal.dump(M.new(100, "km".b)).bytes`,
			"[4, 8, 83, 58, 6, 77, 7, 58, 11, 97, 109, 111, 117, 110, 116, 105, 105, 58, 9, 117, 110, 105, 116, 34, 7, 107, 109]\n"},
		// A Data round-trips to a frozen instance with its members restored.
		{`MB = Data.define(:a, :b); v = Marshal.load(Marshal.dump(MB.new(1, 2))); p [v.a, v.b]`, "[1, 2]\n"},
		{`MF = Data.define(:a); p Marshal.load(Marshal.dump(MF.new(1))).frozen?`, "true\n"},
		// A plain subclass of a Data.define class shares the parent's members.
		{`MP = Data.define(:a); class MPSub < MP; end; v = Marshal.load(Marshal.dump(MPSub.new(9))); p [v.class.name, v.a]`,
			"[\"MPSub\", 9]\n"},
		// The real class name is used, never a #name override.
		{`MN = Data.define(:a); def MN.name; "Nope"; end; d = Marshal.dump(MN.new(1)); p [d.include?("MN"), d.include?("Nope")]`, "[true, false]\n"},

		// --- Regexp: encoding + option byte -------------------------------
		// Binary (ASCII-8BIT) source: dumped bare, no encoding ivar, FIXED bit set.
		{`p Marshal.dump(Regexp.new("".b, Regexp::FIXEDENCODING)).bytes`, "[4, 8, 47, 0, 16]\n"},
		// US-ASCII source, FIXEDENCODING: E => false, FIXED bit set.
		{`p Marshal.dump(Regexp.new("a".encode("us-ascii"), Regexp::FIXEDENCODING)).bytes`,
			"[4, 8, 73, 47, 6, 97, 16, 6, 58, 6, 69, 70]\n"},
		// US-ASCII source, not fixed: E => false, no FIXED bit.
		{`p Marshal.dump(Regexp.new("a".encode("us-ascii"))).bytes`,
			"[4, 8, 73, 47, 6, 97, 0, 6, 58, 6, 69, 70]\n"},
		// Windows-1251 (ASCII-compatible) source, FIXEDENCODING: named :encoding ivar.
		{`p Marshal.dump(Regexp.new("a".encode("windows-1251"), Regexp::FIXEDENCODING)).bytes`,
			"[4, 8, 73, 47, 6, 97, 16, 6, 58, 13, 101, 110, 99, 111, 100, 105, 110, 103, 34, 17, 87, 105, 110, 100, 111, 119, 115, 45, 49, 50, 53, 49]\n"},
		// UTF-8 source, FIXEDENCODING: E => true.
		{`p Marshal.dump(Regexp.new("".dup.force_encoding("utf-8"), Regexp::FIXEDENCODING)).bytes`,
			"[4, 8, 73, 47, 0, 16, 6, 58, 6, 69, 84]\n"},
		// UTF-16LE (ASCII-incompatible) source: :encoding ivar, FIXED bit.
		{`p Marshal.dump(Regexp.new("a".encode("utf-16le"), Regexp::FIXEDENCODING)).bytes`,
			"[4, 8, 73, 47, 7, 97, 0, 16, 6, 58, 13, 101, 110, 99, 111, 100, 105, 110, 103, 34, 13, 85, 84, 70, 45, 49, 54, 76, 69]\n"},
		// UTF-32LE source, NOT fixed by the caller: an ASCII-incompatible source
		// is fixed anyway, so FIXED is set and the encoding is recorded.
		{`p Marshal.dump(Regexp.new("a".encode("utf-32le"))).bytes`,
			"[4, 8, 73, 47, 9, 97, 0, 0, 0, 16, 6, 58, 13, 101, 110, 99, 111, 100, 105, 110, 103, 34, 13, 85, 84, 70, 45, 51, 50, 76, 69]\n"},
		// A UTF-8 literal with a non-ASCII byte is implicitly fixed: FIXED bit, E => true.
		{`p Marshal.dump(Regexp.new("café".dup.force_encoding("utf-8"))).bytes`,
			"[4, 8, 73, 47, 10, 99, 97, 102, 195, 169, 16, 6, 58, 6, 69, 84]\n"},
		// An ASCII-only literal: US-ASCII, no FIXED bit.
		{`p Marshal.dump(/\A.\Z/).bytes`, "[4, 8, 73, 47, 10, 92, 65, 46, 92, 90, 0, 6, 58, 6, 69, 70]\n"},
		// Flag letters map to the low option bits (i|m = 5); still US-ASCII.
		{`p Marshal.dump(//im).bytes`, "[4, 8, 73, 47, 0, 5, 6, 58, 6, 69, 70]\n"},
		// NOENCODING over an ASCII-only source: encoding stays US-ASCII (E => false),
		// only the NONE option bit (32) is set — not FIXED.
		{`p Marshal.dump(Regexp.new("a", Regexp::NOENCODING)).bytes`,
			"[4, 8, 73, 47, 6, 97, 32, 6, 58, 6, 69, 70]\n"},

		// --- Regexp reader: options + encoding survive a round-trip -------
		// dump -> load -> dump is byte-stable for each encoding shape, which
		// exercises the reader restoring the FIXED / NONE option bits and the
		// source encoding from the E / :encoding ivar.
		{`re = Regexp.new("a".encode("us-ascii"), Regexp::FIXEDENCODING); p Marshal.dump(Marshal.load(Marshal.dump(re))).bytes == Marshal.dump(re).bytes`, "true\n"},
		{`re = Regexp.new("café".dup.force_encoding("utf-8"), Regexp::FIXEDENCODING); p Marshal.dump(Marshal.load(Marshal.dump(re))).bytes == Marshal.dump(re).bytes`, "true\n"},
		{`re = Regexp.new("a".encode("windows-1251"), Regexp::FIXEDENCODING); p Marshal.dump(Marshal.load(Marshal.dump(re))).bytes == Marshal.dump(re).bytes`, "true\n"},
		{`re = Regexp.new("a".encode("utf-16le"), Regexp::FIXEDENCODING); p Marshal.dump(Marshal.load(Marshal.dump(re))).bytes == Marshal.dump(re).bytes`, "true\n"},
		{`re = Regexp.new("a".encode("utf-32le")); p Marshal.dump(Marshal.load(Marshal.dump(re))).bytes == Marshal.dump(re).bytes`, "true\n"},
		{`re = Regexp.new("a", Regexp::NOENCODING); p Marshal.dump(Marshal.load(Marshal.dump(re))).bytes == Marshal.dump(re).bytes`, "true\n"},
		{`re = /\A.\Z/; p Marshal.dump(Marshal.load(Marshal.dump(re))).bytes == Marshal.dump(re).bytes`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// An 'S' container naming a class with no member layout is rejected, the
		// same as it is for a non-Struct (here a plain object class).
		{`class NotAData; end; Marshal.load("\x04\bS:\rNotAData\x00")`, "is not a Struct"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
