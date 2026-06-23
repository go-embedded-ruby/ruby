package vm_test

import "testing"

// TestSecureRandom checks SecureRandom's output shapes and ranges (the values are
// cryptographically random, so we assert format/length/range, not exact bytes).
func TestSecureRandom(t *testing.T) {
	cases := []struct{ src, want string }{
		// hex: 2n lowercase hex chars; no-argument default is 16 bytes; nil -> default.
		{`require "securerandom"; p [(SecureRandom.hex(4) =~ /\A[0-9a-f]{8}\z/) == 0, SecureRandom.hex.length, SecureRandom.hex(nil).length]`, "[true, 32, 32]\n"},
		// random_bytes length (bytesize, since random bytes are not valid UTF-8 and
		// String#length counts characters here — we have no binary encoding).
		{`require "securerandom"; p SecureRandom.random_bytes(5).bytesize`, "5\n"},
		// uuid v4 format.
		{`require "securerandom"; p (SecureRandom.uuid =~ /\A\h{8}-\h{4}-4\h{3}-[89ab]\h{3}-\h{12}\z/) == 0`, "true\n"},
		// alphanumeric.
		{`require "securerandom"; p (SecureRandom.alphanumeric(12) =~ /\A[A-Za-z0-9]{12}\z/) == 0`, "true\n"},
		// base64 / urlsafe_base64 (padless default; padded when a truthy 2nd arg is given).
		{`require "securerandom"; p [(SecureRandom.base64(6) =~ /\A[A-Za-z0-9+\/=]+\z/) == 0, (SecureRandom.urlsafe_base64(8) =~ /\A[A-Za-z0-9_-]+\z/) == 0, SecureRandom.urlsafe_base64(5, true).include?("=")]`, "[true, true, true]\n"},
		// random_number: no arg / 0 / negative -> Float in [0,1); positive Integer -> Integer
		// in [0,n); positive Float -> Float in [0,n).
		{`require "securerandom"; r = SecureRandom.random_number(10); p [SecureRandom.random_number.is_a?(Float), SecureRandom.random_number(0).is_a?(Float), SecureRandom.random_number(-3).is_a?(Float), r.is_a?(Integer) && r >= 0 && r < 10]`, "[true, true, true, true]\n"},
		{`require "securerandom"; f = SecureRandom.random_number(2.5); p [f.is_a?(Float), f >= 0, f < 2.5, SecureRandom.random_number(0.0).is_a?(Float)]`, "[true, true, true, true]\n"},
		// Successive calls differ.
		{`require "securerandom"; p SecureRandom.hex != SecureRandom.hex`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
