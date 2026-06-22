package vm_test

import (
	"strings"
	"testing"
)

// TestRandom covers Random and Kernel#rand/#srand. Because the generator is a
// faithful reimplementation of MRI's seeded MT19937, the exact values are
// asserted against MRI Ruby 4.0.5.
func TestRandom(t *testing.T) {
	cases := []struct{ src, want string }{
		// Seeded float and integer sequences match MRI bit for bit.
		{`r = Random.new(42); p [r.rand, r.rand, r.rand]`,
			"[0.3745401188473625, 0.9507143064099162, 0.7319939418114051]\n"},
		{`r = Random.new(42); p [r.rand(100), r.rand(100), r.rand(100), r.rand(100), r.rand(100)]`,
			"[51, 92, 14, 71, 60]\n"},
		{`r = Random.new(1234); p (1..8).map { r.rand(1000) }`,
			"[815, 723, 294, 53, 204, 372, 664, 655]\n"},
		// Ranges (integer inclusive/exclusive and float).
		{`r = Random.new(7); p [r.rand(10..20), r.rand(5)]`, "[14, 1]\n"},
		{`r = Random.new(7); p r.rand(0...5)`, "4\n"},
		{`r = Random.new(5); p r.rand(1.0..2.0).class`, "Float\n"},
		// rand(0.0) and rand(Float) for a Random.
		{`r = Random.new(5); p [r.rand(0.0), r.rand(2.0).class]`, "[0.22199317108973948, Float]\n"},
		// Large (64-bit) bounds.
		{`p Random.new(1).rand(5_000_000_000)`, "4005303368\n"},
		{`p Random.new(123).rand(10_000_000_000)`, "2967327842\n"},
		// Multi-word and negative seeds.
		{`p Random.new(10_000_000_000).rand(100)`, "47\n"},
		{`p Random.new(-5).rand(100)`, "99\n"},
		// bytes (length not a multiple of 4 exercises the tail).
		{`p Random.new(99).bytes(4).bytes`, "[129, 114, 26, 172]\n"},
		{`p Random.new(5).bytes(7).length`, "7\n"},
		{`p Random.new(0).rand(100)`, "44\n"},     // zero seed (seedKey single zero word)
		{`p Random.new(5).rand(1)`, "0\n"},        // rand(1) -> limit 0
		{`p Random.new(1)`, "#<Random>\n"},        // Inspect (MRI shows an address; we don't)
		// seed accessor + determinism of Kernel#rand under srand.
		{`p Random.new(42).seed`, "42\n"},
		{`srand(123); p [rand(100), rand(100)]`, "[66, 92]\n"},
		{`srand(7); a = rand(10); srand(7); p a == rand(10)`, "true\n"},
		{`p srand(99).class`, "Integer\n"}, // srand returns the previous seed
		// Kernel#rand: float truncates to int, negatives use magnitude, 0 -> float.
		{`srand(5); p rand(2.5).class`, "Integer\n"},
		{`srand(5); p rand(-3).class`, "Integer\n"},
		{`srand(5); p rand(0).class`, "Float\n"},
		{`srand(5); p rand.class`, "Float\n"},
		{`srand(7); p rand(10..20)`, "14\n"}, // Kernel#rand with a range
		// Random.new with no seed is non-deterministic but still a Random.
		{`p Random.new.class`, "Random\n"},
		{`p(Random.new(1) ? "y" : "n")`, "\"y\"\n"}, // Truthy
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Random#rand rejects a non-positive integer or negative float, and a
	// non-numeric / empty range.
	errs := []struct{ src, want string }{
		{`Random.new(5).rand(0)`, "invalid argument - 0"},
		{`Random.new(5).rand(-3)`, "invalid argument - -3"},
		{`Random.new(5).rand(-1.5)`, "invalid argument"},
		{`Random.new(5).rand("x")`, "invalid argument"},
		{`Random.new(5).rand(5..1)`, "invalid argument"},
		{`Random.new(5).rand(2.0..1.0)`, "invalid argument"},
		{`rand("x")`, "invalid argument"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got %v want error %q", c.src, err, c.want)
		}
	}
}
