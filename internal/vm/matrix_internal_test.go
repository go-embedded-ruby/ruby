// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"math"
	"math/big"
	"testing"

	libmatrix "github.com/go-ruby-matrix/matrix"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestMatrixNumToValue covers the Num -> Ruby-value mapping across every kind
// the library can produce: Integer, Bignum (a value outside int64), exact
// Rational, finite Float, and the three Float specials (+Inf / -Inf / NaN).
// These last reach the binding only through a Float entry, so they are pinned
// here directly against the library's renderings.
func TestMatrixNumToValue(t *testing.T) {
	big1 := new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil)
	for _, c := range []struct {
		n    libmatrix.Num
		want string // Ruby #inspect of the produced value
	}{
		{libmatrix.NewInt(-2), "-2"},
		{libmatrix.NewBigInt(big1), "100000000000000000000"},
		{libmatrix.NewRat(-3, 2), "(-3/2)"},
		{libmatrix.NewFloat(0.6), "0.6"},
		{libmatrix.NewFloat(5.0), "5.0"},
		{libmatrix.NewFloat(math.Inf(1)), "Infinity"},
		{libmatrix.NewFloat(math.Inf(-1)), "-Infinity"},
		{libmatrix.NewFloat(math.NaN()), "NaN"},
	} {
		got := numToValue(c.n).Inspect()
		if got != c.want {
			t.Errorf("numToValue(%s) = %q; want %q", c.n.String(), got, c.want)
		}
	}

	// A Bignum value round-trips to the *object.Bignum type (not Integer).
	if _, ok := object.KindOK[*object.Bignum](numToValue(libmatrix.NewBigInt(big1))); !ok {
		t.Errorf("a >int64 Num should map to *object.Bignum")
	}
}

// TestMatrixNumFromValue covers the Ruby-value -> Num mapping, including the
// TypeError raised for a non-numeric entry (the guard that keeps the library's
// own numFromAny error unreachable from the binding).
func TestMatrixNumFromValue(t *testing.T) {
	cases := []object.Value{
		object.IntValue(int64(object.Integer(3))),
		object.Wrap(&object.Bignum{I: big.NewInt(7)}),
		object.Wrap(&object.Rational{R: big.NewRat(1, 2)}),
		object.FloatValue(float64(object.Float(1.5))),
	}
	for _, v := range cases {
		// Each accepted kind round-trips through the library: feeding the Num into
		// a 1x1 matrix and reading it back reproduces the value.
		m, err := libmatrix.New([][]any{{numFromValue(v)}})
		if err != nil {
			t.Fatalf("New from %v: %v", v.Inspect(), err)
		}
		n, _ := m.At(0, 0)
		if got := numToValue(n).Inspect(); got != v.Inspect() {
			t.Errorf("round-trip %v -> %q", v.Inspect(), got)
		}
	}

	assertRaises(t, "TypeError", func() { numFromValue(object.Wrap(object.NewString("x"))) })
}

// TestMatrixRaiseErr covers raiseMatrixErr across its branches: nil is a no-op,
// each sentinel maps to its ExceptionForMatrix class, and an unknown error falls
// back to RuntimeError (the defensive branch the library's three sentinels never
// reach from valid Ruby).
func TestMatrixRaiseErr(t *testing.T) {
	raiseMatrixErr(nil) // no panic

	for _, c := range []struct {
		err   error
		class string
	}{
		{libmatrix.ErrDimensionMismatch, "ExceptionForMatrix::ErrDimensionMismatch"},
		{libmatrix.ErrNotRegular, "ExceptionForMatrix::ErrNotRegular"},
		{libmatrix.ErrOperationNotDefined, "ExceptionForMatrix::ErrOperationNotDefined"},
		{errors.New("boom"), "RuntimeError"},
	} {
		assertRaises(t, c.class, func() { raiseMatrixErr(c.err) })
	}
}

// TestMatrixWrapperTruthy covers the Truthy() of the Matrix/Vector wrappers
// (always true, as every object is truthy in Ruby) — reached by the VM's
// boolean-coercion path, pinned here directly.
func TestMatrixWrapperTruthy(t *testing.T) {
	m, _ := libmatrix.New([][]any{{1}})
	v, _ := libmatrix.NewVector([]any{1})
	if !(&Matrix{m: m}).Truthy() || !(&Vector{v: v}).Truthy() {
		t.Errorf("Matrix/Vector wrappers must be truthy")
	}
}

// assertRaises runs fn and asserts it panics with a RubyError of the given
// class.
func assertRaises(t *testing.T, class string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("expected a %s panic, got none", class)
			return
		}
		e, ok := r.(RubyError)
		if !ok || e.Class != class {
			t.Errorf("expected RubyError %s, got %#v", class, r)
		}
	}()
	fn()
}
