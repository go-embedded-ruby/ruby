// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestScanfValue covers scanfValue's mapping of each library value type onto its
// Ruby counterpart, including the defensive default arm. The go-ruby-scanf
// library only ever emits int / *big.Int / float64 / string (see its package
// doc), so the default arm is unreachable from a Ruby program; this internal
// test exercises it directly with a type the library never produces, asserting
// the fallback renders it via Go's %v rather than panicking or dropping it.
func TestScanfValue(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{int(42), "42"},
		{big.NewInt(99), "99"},
		{float64(3.5), "3.5"},
		{"foo", `"foo"`},
		{true, `"true"`}, // the defensive default arm: %v-rendered into a String
	}
	for _, tc := range cases {
		got := scanfValue(tc.in).Inspect()
		if got != tc.want {
			t.Errorf("scanfValue(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestScanfValuesEmpty covers scanfValues over an empty group (the no-match
// result), which must yield an empty Ruby Array.
func TestScanfValuesEmpty(t *testing.T) {
	a, ok := scanfValues(nil).(*object.Array)
	if !ok || len(a.Elems) != 0 {
		t.Errorf("scanfValues(nil) = %v, want empty Array", scanfValues(nil))
	}
}
