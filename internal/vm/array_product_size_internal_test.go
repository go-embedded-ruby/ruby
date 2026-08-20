// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestProductRefusesWhatItCannotCount covers Array#product's size check.
//
// The product of eleven hundred-element arrays has 10^22 entries, which
// overflows the length of an array long before it exhausts memory. MRI works
// the length out first and raises RangeError; this interpreter used to start
// building, and allocated 61.8 GB before a CI runner was killed under it,
// taking the ruby/spec ratchet lane with it for three days.
//
// core/array/product_spec.rb has an example named "does not attempt to produce
// an unreasonable number of products" for exactly this. Verified against
// ruby 4.0.6.
func TestProductRefusesWhatItCannotCount(t *testing.T) {
	cases := []struct{ src, want string }{
		// The spec's own case: 101^11 entries.
		{`a = (0..100).to_a
begin
  a.product(a, a, a, a, a, a, a, a, a, a)
rescue RangeError => e
  p e.message
end`, `"too big to product"`},
		// A product that fits is still built, in MRI's order — last list
		// varying fastest.
		{`p [1, 2].product([3, 4])`, `[[1, 3], [1, 4], [2, 3], [2, 4]]`},
		{`p [1].product([2, 3], [4])`, `[[1, 2, 4], [1, 3, 4]]`},
		// An empty list anywhere makes an empty product, and must not be
		// mistaken for something too big.
		{`p [1, 2].product([])`, `[]`},
		{`p [].product([1, 2])`, `[]`},
		{`a = (0..100).to_a; p a.product(a, a, [], a, a, a, a, a, a, a)`, `[]`},
		// No arguments: each element on its own.
		{`p [1, 2].product`, `[[1], [2]]`},
		// The size is what decides, not the number of lists: many small lists
		// are fine.
		{`p [1, 2].product([1], [1], [1], [1], [1], [1], [1], [1], [1]).size`, `2`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
