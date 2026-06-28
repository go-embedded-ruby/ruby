package vm_test

import (
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
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`Base64.encode64(123)`, "TypeError"},
		{`Base64.strict_decode64("aGVsbG8")`, "ArgumentError"}, // strict: padding required
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
		// A Digest instance reports its class and is truthy / inspectable.
		{`p Digest::SHA256.new.class`, "Digest::SHA256\n"},
		{`p !!Digest::SHA256.new`, "true\n"},
		{`p Digest::SHA256.new.inspect`, "\"#<Digest::SHA256>\"\n"},
		{`p Digest::SHA256.new.to_s`, "\"#<Digest::SHA256>\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if err := runErr(t, `require "digest"; Digest::MD5.hexdigest(123)`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("hexdigest(123): got %v", err)
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
