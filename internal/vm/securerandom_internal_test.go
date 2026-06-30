package vm

import (
	"errors"
	"testing"
)

// fixSecureRandRead replaces the crypto/rand seam with a fixed generator for the
// duration of the test, so the SecureRandom binding produces deterministic,
// MRI-comparable output. The fill writes b[i] = byte(i) so the bytes are
// 00 01 02 ... — a stable pattern shared across every draw.
func fixSecureRandRead(t *testing.T) {
	t.Helper()
	orig := secureRandRead
	secureRandRead = func(b []byte) (int, error) {
		for i := range b {
			b[i] = byte(i)
		}
		return len(b), nil
	}
	t.Cleanup(func() { secureRandRead = orig })
}

// TestSecureRandomDeterministic drives the SecureRandom binding with a fixed
// entropy seam and asserts the exact formatted output, covering the hex/base64
// lengths, the uuid v4 and v7 bit-layout, and the random_number Integer/Float
// dispatch. The expected strings were checked against MRI 4.0.5 by feeding it the
// same 00 01 02 ... byte stream.
func TestSecureRandomDeterministic(t *testing.T) {
	fixSecureRandRead(t)
	cases := []struct{ src, want string }{
		// hex of 00..0f (16 bytes) -> 32 lowercase hex chars.
		{`require "securerandom"; puts SecureRandom.hex`, "000102030405060708090a0b0c0d0e0f\n"},
		{`require "securerandom"; puts SecureRandom.hex(4)`, "00010203\n"},
		// base64 of 00..0f and urlsafe (padless default + padded when truthy 2nd arg).
		{`require "securerandom"; puts SecureRandom.base64`, "AAECAwQFBgcICQoLDA0ODw==\n"},
		{`require "securerandom"; puts SecureRandom.urlsafe_base64(8)`, "AAECAwQFBgc\n"},
		{`require "securerandom"; puts SecureRandom.urlsafe_base64(8, true)`, "AAECAwQFBgc=\n"},
		// random_bytes bytesize.
		{`require "securerandom"; p SecureRandom.random_bytes(5).bytesize`, "5\n"},
		// uuid v4: version nibble 4 at byte 6, variant 10x at byte 8 over 00..0f.
		{`require "securerandom"; puts SecureRandom.uuid`, "00010203-0405-4607-8809-0a0b0c0d0e0f\n"},
		// uuid v7: 48-bit ms timestamp prefix, version nibble 7, 10x variant.
		{`require "securerandom"; p (SecureRandom.uuid_v7 =~ /\A\h{8}-\h{4}-7\h{3}-[89ab]\h{3}-\h{12}\z/) == 0`, "true\n"},
		// alphanumeric draws from the default A-Za-z0-9 alphabet.
		{`require "securerandom"; p (SecureRandom.alphanumeric(12) =~ /\A[A-Za-z0-9]{12}\z/) == 0`, "true\n"},
		// random_number Integer dispatch: positive Integer -> Integer in [0, n).
		{`require "securerandom"; r = SecureRandom.random_number(10); p [r.is_a?(Integer), r >= 0, r < 10]`, "[true, true, true]\n"},
		// random_number Float dispatch: positive Float -> Float in [0, n).
		{`require "securerandom"; f = SecureRandom.random_number(2.5); p [f.is_a?(Float), f >= 0, f < 2.5]`, "[true, true, true]\n"},
		// random_number non-positive / no-arg dispatch -> Float in [0, 1).
		{`require "securerandom"; p [SecureRandom.random_number.is_a?(Float), SecureRandom.random_number(0).is_a?(Float), SecureRandom.random_number(-3).is_a?(Float), SecureRandom.random_number(0.0).is_a?(Float)]`, "[true, true, true, true]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSecureRandomFailures covers the otherwise-unreachable crypto/rand failure
// path through the shared low-level seam (used by tmpdir/openssl as well as the
// SecureRandom binding's entropy source).
func TestSecureRandomFailures(t *testing.T) {
	defer func() {
		secureRandRead = realSecureRandRead
		if r := recover(); r == nil {
			t.Fatal("secureBytes did not panic on a read failure")
		}
	}()
	secureRandRead = func([]byte) (int, error) { return 0, errors.New("boom") }
	secureBytes(4)
}

var realSecureRandRead = secureRandRead
