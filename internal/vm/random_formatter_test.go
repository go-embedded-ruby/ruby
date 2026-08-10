package vm_test

import (
	"strings"
	"testing"
)

// TestRandomFormatter covers the Random::Formatter methods mixed into Random
// (require "random/formatter"). Because they draw from the same seeded MT19937
// stream MRI uses, the deterministic outputs are asserted exactly against MRI
// Ruby 4.0.5.
func TestRandomFormatter(t *testing.T) {
	pre := `require "random/formatter"; `
	cases := []struct{ src, want string }{
		// hex: 2n lowercase hex chars from random_bytes(n); default 16 bytes.
		{`p Random.new(42).hex`, "\"66dce15fb33deacb5c0362f30e95f52e\"\n"},
		{`p Random.new(42).hex(4)`, "\"66dce15f\"\n"},
		{`p Random.new(42).hex(nil).length`, "32\n"}, // nil -> default 16 bytes
		// base64 / urlsafe_base64 (padless default, padded on truthy 2nd arg).
		{`p Random.new(42).base64`, "\"ZtzhX7M96stcA2LzDpX1Lg==\"\n"},
		{`p Random.new(42).base64(6)`, "\"ZtzhX7M9\"\n"},
		{`p Random.new(42).urlsafe_base64`, "\"ZtzhX7M96stcA2LzDpX1Lg\"\n"},
		{`p Random.new(42).urlsafe_base64(8)`, "\"ZtzhX7M96ss\"\n"},
		{`p Random.new(42).urlsafe_base64(8, true)`, "\"ZtzhX7M96ss=\"\n"},
		// random_bytes: binary string; default 16 bytes; exact bytes for seed 42.
		{`p Random.new(42).random_bytes(5).bytes`, "[102, 220, 225, 95, 179]\n"},
		{`p Random.new(42).random_bytes.bytesize`, "16\n"},
		{`p Random.new(42).random_bytes(5).encoding.to_s`, "\"ASCII-8BIT\"\n"},
		// uuid v4 and its uuid_v4 alias are deterministic under a seed.
		{`p Random.new(42).uuid`, "\"66dce15f-b33d-4acb-9c03-62f30e95f52e\"\n"},
		{`p Random.new(42).uuid_v4`, "\"66dce15f-b33d-4acb-9c03-62f30e95f52e\"\n"},
		// alphanumeric: default 16, explicit length, and a custom chars: alphabet.
		{`p Random.new(42).alphanumeric`, "\"SyWMkJRvgNMiHV6O\"\n"},
		{`p Random.new(42).alphanumeric(12)`, "\"SyWMkJRvgNMi\"\n"},
		{`p Random.new(42).alphanumeric(8, chars: ["a", "b", "c"])`, "\"acacbcac\"\n"},
		{`p Random.new(7).alphanumeric(10, chars: [*"0".."9"])`, "\"5161477232\"\n"},
		// A single-character source yields n copies; an empty source or n<=0 yields "".
		{`p Random.new(42).alphanumeric(4, chars: ["z"])`, "\"zzzz\"\n"},
		{`p Random.new(42).alphanumeric(4, chars: [])`, "\"\"\n"},
		{`p Random.new(42).alphanumeric(0)`, "\"\"\n"},
		// random_number dispatch: Integer>0, no-arg/0/negative Float, positive Float, Range.
		{`p Random.new(42).random_number(100)`, "51\n"},
		{`p Random.new(42).random_number`, "0.3745401188473625\n"},
		{`p Random.new(42).random_number(2.5)`, "0.9363502971184062\n"},
		{`p Random.new(42).random_number(0)`, "0.3745401188473625\n"},
		{`p Random.new(42).random_number(-5)`, "0.3745401188473625\n"},
		{`p Random.new(42).random_number(0.0).class`, "Float\n"},
		{`p Random.new(7).random_number(10..20)`, "14\n"},
	}
	for _, c := range cases {
		if got := eval(t, pre+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}

	// uuid_v7 embeds the wall clock, so only its RFC 9562 shape is asserted.
	if got := eval(t, pre+`p((Random.new(1).uuid_v7 =~ /\A\h{8}-\h{4}-7\h{3}-[89ab]\h{3}-\h{12}\z/) == 0)`); got != "true\n" {
		t.Errorf("uuid_v7 shape: got %q", got)
	}
	// require returns true the first time and false afterwards.
	if got := eval(t, `p [require("random/formatter"), require("random/formatter")]`); got != "[true, false]\n" {
		t.Errorf("require random/formatter: got %q", got)
	}

	// Error branches: a non-numeric random_number bound is an ArgumentError; a
	// non-Array chars: is a TypeError.
	errs := []struct{ src, want string }{
		{pre + `Random.new(42).random_number("x")`, "invalid argument"},
		{pre + `Random.new(42).alphanumeric(8, chars: 5)`, "no implicit conversion"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got %v want error %q", c.src, err, c.want)
		}
	}
}

// TestRandomClassMethods covers Random's class-side singleton methods, which act
// on the process-wide default generator that Kernel#srand reseeds (so Random.rand
// and Kernel#rand share one stream). Values are pinned against MRI Ruby 4.0.5.
func TestRandomClassMethods(t *testing.T) {
	cases := []struct{ src, want string }{
		// Random.rand keeps Random#rand semantics (a Float bound gives a Float, unlike
		// Kernel#rand which truncates), on the srand-seeded default generator.
		{`srand(123); p Random.rand(100)`, "66\n"},
		{`srand(123); p Random.rand(2.5).class`, "Float\n"},
		// Random.bytes and Random.random_number draw from the same default stream.
		{`srand(123); p Random.bytes(4).bytes`, "[254, 205, 75, 178]\n"},
		{`srand(123); p Random.bytes(4).encoding.to_s`, "\"ASCII-8BIT\"\n"},
		{`srand(123); p Random.random_number(100)`, "66\n"},
		// Random.srand returns the previous seed and reseeds the default generator.
		{`srand(42); p Random.srand(999)`, "42\n"},
		{`srand(7); a = Random.rand(1000); srand(7); p a == Random.rand(1000)`, "true\n"},
		// Random.new_seed is a fresh Integer seed usable to construct a Random.
		{`p Random.new_seed.is_a?(Integer)`, "true\n"},
		{`p Random.new(Random.new_seed).class`, "Random\n"},
		// Random.srand with no argument reseeds from entropy and still returns the
		// previous seed (an Integer).
		{`srand(5); p Random.srand.class`, "Integer\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRandomEquality covers Random#== (both the direct method and the VM's OpEq
// fast path): two generators are equal iff they are in the same state — same
// seed and same number of draws — matching MRI Ruby 4.0.5.
func TestRandomEquality(t *testing.T) {
	cases := []struct{ src, want string }{
		// Fresh, identically seeded generators are equal; a draw on one diverges them.
		{`a = Random.new(42); b = Random.new(42); p a == b`, "true\n"},
		{`a = Random.new(42); b = Random.new(42); a.rand; p a == b`, "false\n"},
		// Differently seeded generators are unequal.
		{`p Random.new(42) == Random.new(7)`, "false\n"},
		// After the same draws from the same seed the states are equal again.
		{`a = Random.new(42); a.rand; b = Random.new(42); b.rand; p a == b`, "true\n"},
		// A non-Random operand is never equal (exercises the OpEq fast path too).
		{`p Random.new(42) == 5`, "false\n"},
		{`p Random.new(42) != Random.new(7)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
