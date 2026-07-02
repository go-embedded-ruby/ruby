// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"testing"

	money "github.com/go-ruby-money/money"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// mustRaise runs fn and asserts it raised a Ruby exception (a rubyError panic).
func mustRaise(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s: expected a raise", what)
		}
	}()
	fn()
}

// TestMoneyWrapperMethods covers the ToS / Inspect / Truthy arms of the Money and
// Currency wrappers (the Ruby surface routes through explicit to_s/inspect
// methods, so the value-protocol arms need a direct call).
func TestMoneyWrapperMethods(t *testing.T) {
	m := &Money{m: money.New(1000, money.MustCurrency("USD"))}
	if m.ToS() == "" || m.Inspect() == "" || !m.Truthy() {
		t.Errorf("Money methods: %q %q %v", m.ToS(), m.Inspect(), m.Truthy())
	}
	c := &Currency{c: money.MustCurrency("USD")}
	if c.ToS() == "" || c.Inspect() == "" || !c.Truthy() {
		t.Errorf("Currency methods: %q %q %v", c.ToS(), c.Inspect(), c.Truthy())
	}
}

// TestMoneyRatBridge covers moneyRat across Integer, Bignum, Float and the
// TypeError default.
func TestMoneyRatBridge(t *testing.T) {
	if moneyRat(object.Integer(3)).Cmp(big.NewRat(3, 1)) != 0 {
		t.Error("integer -> rat")
	}
	if moneyRat(&object.Bignum{I: big.NewInt(5)}).Cmp(big.NewRat(5, 1)) != 0 {
		t.Error("bignum -> rat")
	}
	if f, _ := moneyRat(object.Float(1.5)).Float64(); f != 1.5 {
		t.Error("float -> rat")
	}
	mustRaise(t, "moneyRat proc", func() { moneyRat(&Proc{}) })
}

// TestMoneyArgBridge covers moneyArg's TypeError arm (a non-Money argument).
func TestMoneyArgBridge(t *testing.T) {
	mustRaise(t, "moneyArg non-money", func() { moneyArg([]object.Value{object.Integer(1)}) })
	mustRaise(t, "moneyArg no-arg", func() { moneyArg(nil) })
}

// TestCurrencyArgBridge covers currencyArg's default TypeError arm and
// currencyArgOr's absent-argument fallback.
func TestCurrencyArgBridge(t *testing.T) {
	vm := New(nil)
	mustRaise(t, "currencyArg int", func() { vm.currencyArg(object.Integer(1)) })
	// currencyArgOr with the index past the args uses the default.
	if c := vm.currencyArgOr([]object.Value{object.Integer(1)}, 1, "USD"); c.ISOCode != "USD" {
		t.Errorf("currencyArgOr default -> %s", c.ISOCode)
	}
	// currencyArgOr with an explicit nil uses the default too.
	if c := vm.currencyArgOr([]object.Value{object.Integer(1), object.NilV}, 1, "EUR"); c.ISOCode != "EUR" {
		t.Errorf("currencyArgOr nil -> %s", c.ISOCode)
	}
}

// TestMoneyInt64SliceBridge covers moneyInt64Slice's non-Array TypeError arm.
func TestMoneyInt64SliceBridge(t *testing.T) {
	mustRaise(t, "moneyInt64Slice non-array", func() { moneyInt64Slice(object.Integer(1)) })
}
