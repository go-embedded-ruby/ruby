// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestSprintfFloatStringHex covers the format binding's parseFormatFloat: a
// String operand for a float conversion is parsed exactly as MRI's Kernel#Float
// (the "%f/%e/%g behaves as if calling Kernel#Float" rule), so a hexadecimal
// float literal is accepted and an out-of-range magnitude yields ±Inf / 0.0
// rather than an error. Every expectation is byte-for-byte against MRI 4.0.
func TestSprintfFloatStringHex(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		// Hexadecimal float strings (the previously-failing case): "0xA" == 10.0.
		{"hex_f", `p("%f" % "0xA")`, "\"10.000000\"\n"},
		{"hex_e", `p("%e" % "0xA")`, "\"1.000000e+01\"\n"},
		{"hex_E", `p("%E" % "0xA")`, "\"1.000000E+01\"\n"},
		{"hex_g", `p("%g" % "0xA")`, "\"10\"\n"},
		{"hex_G", `p("%G" % "0xA")`, "\"10\"\n"},
		{"hex_scaled", `p("%f" % "0x1.8p3")`, "\"12.000000\"\n"},
		{"hex_signed", `p("%f" % "-0xA")`, "\"-10.000000\"\n"},
		// Overflow -> +Infinity, underflow -> 0.0 (strconv's ErrRange path).
		{"overflow", `p("%f" % "1e400")`, "\"Inf\"\n"},
		{"underflow", `p("%f" % "1e-400")`, "\"0.000000\"\n"},
		// Decimal strings and underscores keep working (regression guard).
		{"decimal", `p("%f" % "1.5")`, "\"1.500000\"\n"},
		{"integerish", `p("%f" % "10")`, "\"10.000000\"\n"},
		{"underscore", `p("%f" % "1_000.5")`, "\"1000.500000\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestSprintfFloatStringErrors covers parseFormatFloat's error path: a malformed
// String operand raises ArgumentError with MRI's exact, un-doubled message
// "invalid value for Float(): <inspect>" (the engine wraps err.Error(), so the
// binding must return only the message, not a class-prefixed *format.Error).
func TestSprintfFloatStringErrors(t *testing.T) {
	tests := []struct{ name, src, wantMsg string }{
		{"garbage", `"%f" % "xyz"`, `invalid value for Float(): "xyz"`},
		{"binary_prefix", `"%f" % "0b1"`, `invalid value for Float(): "0b1"`},
		{"octal_prefix", `"%f" % "0o17"`, `invalid value for Float(): "0o17"`},
		{"bad_hex_underscore", `"%f" % "0x1_p0"`, `invalid value for Float(): "0x1_p0"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil {
				t.Fatalf("src=%q: expected ArgumentError, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "ArgumentError") {
				t.Errorf("src=%q: want ArgumentError, got %v", tc.src, err)
			}
			// The message must not carry a doubled "ArgumentError: " prefix.
			if !strings.Contains(err.Error(), tc.wantMsg) || strings.Contains(err.Error(), "ArgumentError: ArgumentError") {
				t.Errorf("src=%q: want message %q un-doubled, got %v", tc.src, tc.wantMsg, err)
			}
		})
	}
}

// TestSprintfIntegerStringErrorMessage covers parseFormatInteger's error path:
// like the float path, a malformed integer String operand raises ArgumentError
// with the un-doubled "invalid value for Integer(): <inspect>" message.
func TestSprintfIntegerStringErrorMessage(t *testing.T) {
	got := eval(t, `begin; "%d" % "abc"; rescue => e; p e.message; end`)
	want := "\"invalid value for Integer(): \\\"abc\\\"\"\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestSprintfIntegerStringOK guards the parseFormatInteger success path: a valid
// radix-prefixed String operand still formats (so the error-path change did not
// disturb the accepted case).
func TestSprintfIntegerStringOK(t *testing.T) {
	if got := eval(t, `p("%d" % "0x1f")`); got != "\"31\"\n" {
		t.Errorf("got=%q want %q", got, "\"31\"\n")
	}
}

// TestSprintfNamedKeyError covers raiseFormatError's KeyError path: an unmatched
// %{name} / %<name> reference raises a KeyError whose #key is the missing key as
// a Symbol and whose #receiver is the very hash operand (identity), matching
// MRI. Both bracket styles are exercised (namedKeyFromMessage handles each).
func TestSprintfNamedKeyError(t *testing.T) {
	// %{name}: message, class, and #key.
	got := eval(t, `begin; "%{foo}" % {a: 5}; rescue KeyError => e; p [e.message, e.key]; end`)
	if want := "[\"key{foo} not found\", :foo]\n"; got != want {
		t.Errorf("braced: got=%q want=%q", got, want)
	}
	// %<name>: #key.to_s and #receiver identity with the passed hash.
	got = eval(t, `h = {a: 5}
begin
  "%<foo>s" % h
rescue KeyError => e
  p [e.key.to_s, e.receiver.equal?(h)]
end`)
	if want := "[\"foo\", true]\n"; got != want {
		t.Errorf("angle: got=%q want=%q", got, want)
	}
}

// TestSprintfNonKeyErrorUnaffected guards raiseFormatError's non-KeyError branch:
// a ClassName-driven TypeError from an unconvertible operand and an ArgumentError
// from a bad literal still surface with their own class (the KeyError special-
// casing does not intercept them).
func TestSprintfNonKeyErrorUnaffected(t *testing.T) {
	if err := runErr(t, `"%d" % :sym`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("want TypeError, got %v", err)
	}
	if err := runErr(t, `"%d" % "abc"`); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
		t.Errorf("want ArgumentError, got %v", err)
	}
}
