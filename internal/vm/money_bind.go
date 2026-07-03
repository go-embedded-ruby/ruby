// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"

	money "github.com/go-ruby-money/money"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's object graph and the
// github.com/go-ruby-money/money library. The value model, arithmetic and
// formatting live in that library; rbgo only translates its Money / Currency
// wrappers, integer / float / rational scalars and the format options hash.

// moneyOf returns the receiver's wrapped library Money.
func moneyOf(v object.Value) *money.Money { return object.Kind[*Money](v).m }

// moneyArg reads a required Money argument, raising TypeError otherwise.
func moneyArg(args []object.Value) *money.Money {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	m, ok := object.KindOK[*Money](args[0])
	if !ok {
		raise("TypeError", "expected a Money")
	}
	return m.m
}

// moneyCmp compares the receiver against a Money argument, raising
// Money::DifferentCurrencyError when the currencies cannot be compared (matching
// the gem's ordering, which requires a common currency).
func moneyCmp(self object.Value, args []object.Value) int {
	c, err := moneyOf(self).Cmp(moneyArg(args))
	if err != nil {
		raise("Money::DifferentCurrencyError", "%s", err.Error())
	}
	return c
}

// moneyResult wraps an (out, err) arithmetic result: a DifferentCurrencyError
// from the library becomes the Ruby Money::DifferentCurrencyError.
func moneyResult(out *money.Money, err error) object.Value {
	if err != nil {
		raise("Money::DifferentCurrencyError", "%s", err.Error())
	}
	return object.Wrap(&Money{m: out})
}

// currencyArg coerces a currency argument (a String / Symbol id, or a
// Money::Currency wrapper) to a *money.Currency, raising
// Money::Currency::UnknownCurrency for an unknown id.
func (vm *VM) currencyArg(v object.Value) *money.Currency {
	{
		__sw99 := v
		switch {
		case object.IsKind[*Currency](__sw99):
			c := object.Kind[*Currency](__sw99)
			_ = c
			return c.c
		case object.IsKind[object.Symbol](__sw99):
			c := object.Kind[object.Symbol](__sw99)
			_ = c
			return lookupCurrency(string(c))
		case object.IsKind[*object.String](__sw99):
			c := object.Kind[*object.String](__sw99)
			_ = c
			return lookupCurrency(c.Str())
		}
	}
	raise("TypeError", "no implicit conversion of %s into a currency", v.Inspect())
	return nil
}

// currencyArgOr coerces args[i] to a currency, or falls back to def when the
// argument is absent.
func (vm *VM) currencyArgOr(args []object.Value, i int, def string) *money.Currency {
	if i >= len(args) || object.IsNil(args[i]) {
		return lookupCurrency(def)
	}
	return vm.currencyArg(args[i])
}

// lookupCurrency resolves an id, raising Money::Currency::UnknownCurrency when it
// is not in the ISO table.
func lookupCurrency(id string) *money.Currency {
	c, err := money.NewCurrency(id)
	if err != nil {
		raise("Money::Currency::UnknownCurrency", "%s", err.Error())
	}
	return c
}

// moneyRat coerces a Ruby scalar (Integer / Float / Bignum) to a *big.Rat for
// Money's rational arithmetic, raising TypeError otherwise.
func moneyRat(v object.Value) *big.Rat {
	{
		__sw100 := v
		switch {
		case object.IsInt(__sw100):
			n := object.AsInteger(__sw100)
			_ = n
			return new(big.Rat).SetInt64(int64(n))
		case object.IsKind[*object.Bignum](__sw100):
			n := object.Kind[*object.Bignum](__sw100)
			_ = n
			return new(big.Rat).SetInt(n.I)
		case object.IsFloat(__sw100):
			n := object.AsFloatV(__sw100)
			_ = n
			return new(big.Rat).SetFloat64(float64(n))
		}
	}
	raise("TypeError", "no implicit conversion of %s into a number", v.Inspect())
	return nil
}

// moneyInt64Slice coerces a Ruby Array of weights to a []int64 for #allocate.
func moneyInt64Slice(v object.Value) []int64 {
	arr, ok := object.KindOK[*object.Array](v)
	if !ok {
		raise("TypeError", "expected an Array of weights")
	}
	out := make([]int64, len(arr.Elems))
	for i, e := range arr.Elems {
		out[i] = intArg(e)
	}
	return out
}

// moneySlice maps a []*money.Money to a Ruby Array of Money objects.
func moneySlice(ms []*money.Money) object.Value {
	arr := object.NewArrayFromSlice(make([]object.Value, len(ms)))
	for i, m := range ms {
		arr.Elems[i] = object.Wrap(&Money{m: m})
	}
	return object.Wrap(arr)
}

// ratToFloat renders a *big.Rat as the nearest float64 (Money#amount is a
// BigDecimal in the gem; rbgo surfaces it as a Float).
func ratToFloat(r *big.Rat) float64 {
	f, _ := r.Float64()
	return f
}

// moneyOptsArg returns the trailing options Hash of Money#format, or nil.
func moneyOptsArg(args []object.Value) *object.Hash {
	if len(args) == 0 {
		return nil
	}
	h, _ := object.KindOK[*object.Hash](args[len(args)-1])
	return h
}

// moneyFormatOpts maps the Ruby format options hash to money.Options. The keys
// mirror the gem: no_cents:, with_currency:, symbol: (false disables it, a String
// overrides it), sign_positive: and drop_trailing_zeros:.
func moneyFormatOpts(h *object.Hash) money.Options {
	o := money.Options{}
	if h == nil {
		return o
	}
	o.NoCents = boolOpt(h, "no_cents")
	o.NoCentsIfWhole = boolOpt(h, "no_cents_if_whole")
	o.WithCurrency = boolOpt(h, "with_currency")
	o.DropTrailingZeros = boolOpt(h, "drop_trailing_zeros")
	o.SignPositive = boolOpt(h, "sign_positive")
	if v, ok := h.Get(object.SymVal(string(object.Symbol("symbol")))); ok {
		{
			__sw101 := v
			switch {
			case object.IsBool(__sw101):
				s := object.AsBoolV(__sw101)
				_ = s
				if bool(s) {
					o.Symbol = money.SymbolOn()
				} else {
					o.Symbol = money.SymbolOff()
				}
			case object.IsKind[*object.String](__sw101):
				s := object.Kind[*object.String](__sw101)
				_ = s
				o.Symbol = money.SymbolString(s.Str())
			}
		}
	}
	return o
}

// boolOpt reads a boolean option keyed by a Symbol, defaulting to false.
func boolOpt(h *object.Hash, key string) bool {
	if v, ok := h.Get(object.SymVal(string(object.Symbol(key)))); ok {
		return v.Truthy()
	}
	return false
}
