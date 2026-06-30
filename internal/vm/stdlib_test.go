package vm_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestBase64 covers the Base64 module (require "base64"), asserted against
// MRI Ruby 4.0.5.
func TestBase64(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "base64"; p Base64.strict_encode64("hello")`, "\"aGVsbG8=\"\n"},
		{`p Base64.strict_encode64("")`, "\"\"\n"},
		{`p Base64.urlsafe_encode64("hello")`, "\"aGVsbG8=\"\n"},
		{`p Base64.decode64("aGVsbG8=")`, "\"hello\"\n"},
		{`p Base64.decode64("aGVs\nbG8=")`, "\"hello\"\n"},       // newlines ignored
		{`p Base64.decode64("@@@")`, "\"\"\n"},                   // invalid chars ignored
		{`p Base64.decode64("aGVsbG8")`, "\"hello\"\n"},          // missing padding ok
		{`p Base64.decode64("abcde").bytes`, "[105, 183, 29]\n"}, // orphan sextet dropped
		{`p Base64.strict_decode64("aGVsbG8=")`, "\"hello\"\n"},
		{`p Base64.urlsafe_decode64("aGVsbG8=")`, "\"hello\"\n"},
		// encode64 wraps at 60 columns; verify via a round-trip of a long input.
		{`p Base64.decode64(Base64.encode64("a" * 50)) == "a" * 50`, "true\n"},
		// Fidelity gaps the go-ruby-base64 dedup fixed vs the old inline shim:
		// decode64 stops at a mid-stream "=" terminator (the "m" quad machine).
		{`p Base64.decode64("aGVsbG8=world")`, "\"hello\"\n"},
		// urlsafe_encode64 emits padding and the -_ alphabet (RFC 4648).
		{`p Base64.urlsafe_encode64("\xfb\xff")`, "\"-_8=\"\n"},
		// 60-column wrap of encode64 with a trailing newline.
		{"p Base64.encode64(\"a\" * 50)", "\"YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFh\\nYWFhYWE=\\n\"\n"},
		// encode64 whose output is an EXACT multiple of 60 columns: MRI puts a '\n'
		// only between lines plus one trailing '\n' — so a single full line ends with
		// exactly one newline, two full lines with two (not an extra blank line).
		{"p Base64.encode64(\"a\" * 45)", "\"YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFh\\n\"\n"},
		{"p Base64.encode64(\"a\" * 90)", "\"YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFh\\nYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFh\\n\"\n"},
		// encode tails: a 2-byte (len%3==2) input pads with a single '='.
		{`p Base64.strict_encode64("ab")`, "\"YWI=\"\n"},
		{"p Base64.encode64(\"xy\")", "\"eHk=\\n\"\n"},
		// Large inputs cross the SIMD threshold (strict_encode64 routes to go-simd);
		// the round-trip stays bit-exact through encode64 (always scalar) too.
		{`p Base64.strict_decode64(Base64.strict_encode64("M" * 3000)) == "M" * 3000`, "true\n"},
		{`p Base64.decode64(Base64.encode64("Z" * 3000)) == "Z" * 3000`, "true\n"},
		// urlsafe_decode64 accepts unpadded input (re-pads to a quad) and, like MRI's
		// tr-based impl, also decodes the standard +/ alphabet.
		{`p Base64.urlsafe_decode64("MDEyMzQ1Njc")`, "\"01234567\"\n"},
		{`p Base64.urlsafe_decode64("++//").bytes`, "[251, 239, 255]\n"},
		// urlsafe_decode64 translates the -_ alphabet to +/ before strict-decoding.
		{`p Base64.urlsafe_decode64("-_8=").bytes`, "[251, 255]\n"},
		// strict_decode64 of a "==" (two-pad) final quad -> a single byte.
		{`p Base64.strict_decode64("YQ==")`, "\"a\"\n"},
		// encode tail of a single byte pads with "==".
		{`p Base64.strict_encode64("a")`, "\"YQ==\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`Base64.encode64(123)`, "TypeError"},
		{`Base64.strict_decode64("aGVsbG8")`, "ArgumentError"},    // strict: padding required
		{`Base64.strict_decode64("aGVs\nbG8=")`, "ArgumentError"}, // strict: embedded newline rejected
		{`Base64.strict_decode64("ab===")`, "ArgumentError"},      // strict: >2 padding chars (non-quad length)
		{`Base64.strict_decode64("a===")`, "ArgumentError"},       // strict: 3 padding chars in a quad
		{`Base64.strict_decode64("abcde")`, "ArgumentError"},      // strict: length not a multiple of 4
		{`Base64.strict_decode64("ab@=")`, "ArgumentError"},       // strict: stray byte in padded quad
		{`Base64.strict_decode64("a@==")`, "ArgumentError"},       // strict: stray byte before "=="
		{`Base64.urlsafe_decode64("@@@@")`, "ArgumentError"},
	} {
		if err := runErr(t, `require "base64"; `+c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestDigest covers the Digest module (require "digest"), asserted against MRI.
func TestDigest(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "digest"; p Digest::MD5.hexdigest("abc")`, "\"900150983cd24fb0d6963f7d28e17f72\"\n"},
		{`p Digest::SHA1.hexdigest("abc")`, "\"a9993e364706816aba3e25717850c26c9cd0d89d\"\n"},
		{`p Digest::SHA256.hexdigest("abc")`, "\"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\"\n"},
		{`p Digest::SHA512.hexdigest("")[0, 16]`, "\"cf83e1357eefb8bd\"\n"},
		{`p Digest::SHA256.base64digest("abc")`, "\"ungWv48Bz+pBQUDeXa4iI7ADYaOWF3qctBD/YfIAFa0=\"\n"},
		{`p Digest::SHA256.digest("abc").bytesize`, "32\n"},
		// Incremental instance protocol: new, update / <<, hexdigest / digest,
		// base64digest, reset — byte-identical to the one-shot class methods.
		{`d = Digest::SHA256.new; d << "a"; d.update("bc"); p d.hexdigest`,
			"\"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\"\n"},
		{`d = Digest::SHA256.new; p d.hexdigest("abc")`,
			"\"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\"\n"},
		{`d = Digest::SHA256.new; p d.digest("abc").bytesize`, "32\n"},
		{`d = Digest::SHA256.new; d << "abc"; p d.base64digest`,
			"\"ungWv48Bz+pBQUDeXa4iI7ADYaOWF3qctBD/YfIAFa0=\"\n"},
		// update / << return the digest so they chain.
		{`d = Digest::MD5.new; p (d << "x").equal?(d)`, "true\n"},
		// reset returns a fresh state.
		{`d = Digest::SHA256.new; d << "junk"; d.reset; p d.hexdigest("abc")`,
			"\"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\"\n"},
		// The remaining algorithms (SHA384, RMD160) round out the library set.
		{`p Digest::SHA384.hexdigest("abc")[0, 16]`, "\"cb00753f45a35e8b\"\n"},
		{`p Digest::RMD160.hexdigest("abc")`, "\"8eb208f7e05d987a9b044a8e98c6b087f15a0bfc\"\n"},
		// A Digest instance reports its class and is truthy; to_s / inspect render
		// the running hex digest, matching MRI's Digest::Instance form.
		{`p Digest::SHA256.new.class`, "Digest::SHA256\n"},
		{`p !!Digest::SHA256.new`, "true\n"},
		{`p Digest::SHA256.new.to_s`, "\"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855\"\n"},
		{`p Digest::SHA256.new.inspect`, "\"#<Digest::SHA256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855>\"\n"},
		// The ! finalizers emit then reset, so a follow-up read is the empty digest.
		{`d = Digest::SHA256.new; d << "abc"; h = d.hexdigest!; p [h, d.hexdigest == Digest::SHA256.hexdigest("")]`,
			"[\"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\", true]\n"},
		{`d = Digest::SHA256.new; d << "abc"; p [d.digest!.bytesize, d.hexdigest == Digest::SHA256.hexdigest("")]`,
			"[32, true]\n"},
		{`d = Digest::MD5.new; d << "abc"; b = d.base64digest!; p [b, d.base64digest == Digest::MD5.base64digest("")]`,
			"[\"kAFQmDzST7DWlj99KOF/cg==\", true]\n"},
		{`p Digest::MD5.base64digest("")`, "\"1B2M2Y8AsgTpgAmY7PhCfg==\"\n"},
		// == compares hex digests against another instance or a hex string.
		{`a = Digest::MD5.new; a << "abc"; b = Digest::MD5.new; b << "abc"; p(a == b)`, "true\n"},
		{`a = Digest::MD5.new; a << "abc"; p(a == Digest::MD5.hexdigest("abc"))`, "true\n"},
		{`a = Digest::MD5.new; a << "abc"; p(a == "wrong")`, "false\n"},
		{`a = Digest::MD5.new; a << "abc"; p(a == 42)`, "false\n"},
		// Size accessors: length / size / digest_length report the digest size,
		// block_length the algorithm's internal block size.
		{`d = Digest::SHA256.new; p [d.length, d.size, d.digest_length, d.block_length]`, "[32, 32, 32, 64]\n"},
		// Bubblebabble: of a raw string (Digest.bubblebabble) and of a digest.
		{`p Digest.bubblebabble("abc")`, "\"ximek-domex\"\n"},
		{`p Digest::MD5.bubblebabble("abc")[0, 11]`, "\"xogab-cegen\"\n"},
		// Digest(name) factory returns the algorithm class (case/dash-insensitive,
		// RIPEMD160 aliasing RMD160), so .hexdigest works straight off it.
		{`p Digest("SHA256")`, "Digest::SHA256\n"},
		{`p Digest("sha-256").hexdigest("abc")`, "\"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\"\n"},
		{`p Digest("RIPEMD160")`, "Digest::RMD160\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// A non-String argument to a class one-shot raises TypeError (base64Arg).
	if err := runErr(t, `require "digest"; Digest::MD5.hexdigest(123)`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("hexdigest(123): got %v", err)
	}
	// The Digest(name) factory raises LoadError for an unknown algorithm, matching
	// MRI's autoload-based const_missing.
	if err := runErr(t, `require "digest"; Digest("BOGUS")`); err == nil || !strings.Contains(err.Error(), "LoadError") {
		t.Errorf("Digest(BOGUS): got %v", err)
	}
}

// TestDigestFile covers Digest::ALGO.file(path): the digest of a file's contents,
// returned as a resumable instance, plus the missing-file (Errno::ENOENT) branch.
// The path comes from t.TempDir so the test is identical on every OS (Windows CI
// runs the same gate); filepath.ToSlash keeps the Ruby string literal portable.
func TestDigestFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.ToSlash(filepath.Join(dir, "data.txt"))
	if err := os.WriteFile(filepath.FromSlash(path), []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `require "digest"; p Digest::SHA256.file(` + strconv.Quote(path) + `).hexdigest`
	want := "\"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("file digest: got %q want %q", got, want)
	}
	missing := filepath.ToSlash(filepath.Join(dir, "nope.txt"))
	bad := `require "digest"; Digest::SHA256.file(` + strconv.Quote(missing) + `)`
	if err := runErr(t, bad); err == nil || !strings.Contains(err.Error(), "ENOENT") {
		t.Errorf("file(missing): got %v", err)
	}
}

// TestRequireReturn covers require's return value for built-in features: a
// preloaded feature is false, a provided one is true the first time then false.
func TestRequireReturn(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require("set")`, "false\n"},                                 // preloaded
		{`p require("base64")`, "true\n"},                               // provided, first load
		{`p [require("digest"), require("digest")]`, "[true, false]\n"}, // then already loaded
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
