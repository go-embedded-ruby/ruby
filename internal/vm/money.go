// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	money "github.com/go-ruby-money/money"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Money wraps a *money.Money as a Ruby Money object. The integer-cents value
// model, arithmetic, allocate/split penny distribution, formatting and the
// exchange bank all live in the github.com/go-ruby-money/money library; this
// shell only reports the Ruby class and delegates each method (see money_bind.go).
type Money struct{ m *money.Money }

func (m *Money) ToS() string     { return m.m.String() }
func (m *Money) Inspect() string { return m.m.Inspect() }
func (m *Money) Truthy() bool    { return true }

// Currency wraps a *money.Currency as a Ruby Money::Currency object.
type Currency struct{ c *money.Currency }

func (c *Currency) ToS() string     { return c.c.String() }
func (c *Currency) Inspect() string { return "#<Money::Currency id: " + c.c.ID + ">" }
func (c *Currency) Truthy() bool    { return true }

// registerMoney installs the Money class (require "money"): Money.new(cents,
// currency), the arithmetic / comparison / allocate / split surface, #format, the
// Money::Currency class, and the process-wide default bank (Money.default_bank /
// Money.add_rate). The error tree lives under Money::Currency:: mirroring the gem.
func (vm *VM) registerMoney() {
	cls := newClass("Money", vm.cObject)
	vm.consts["Money"] = cls

	curCls := newClass("Money::Currency", vm.cObject)
	cls.consts["Currency"] = curCls
	vm.consts["Money::Currency"] = curCls
	vm.registerMoneyErrors(cls, curCls)

	// The process-wide default bank starts as a VariableExchange so Money.add_rate
	// works out of the box (matching the gem's default).
	vm.moneyBank = money.NewVariableExchange()

	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			cents := intArg(args[0])
			cur := vm.currencyArgOr(args, 1, "USD")
			return &Money{m: money.NewWithBank(cents, cur, vm.moneyBank)}
		}}
	cls.smethods["default_bank"] = &Method{name: "default_bank", owner: cls,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NilV // the bank is a host seam; the Ruby handle is opaque
		}}
	cls.smethods["add_rate"] = &Method{name: "add_rate", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 3 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 3)", len(args))
			}
			from := vm.currencyArg(args[0])
			to := vm.currencyArg(args[1])
			vm.moneyBank.AddRate(from, to, moneyRat(args[2]))
			return args[2]
		}}

	vm.registerMoneyInstance(cls)
	vm.registerMoneyCurrency(curCls)
}

// registerMoneyInstance installs the Money instance surface: readers, arithmetic,
// comparison, penny distribution and formatting.
func (vm *VM) registerMoneyInstance(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("cents", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(moneyOf(self).Cents())
	})
	d("fractional", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(moneyOf(self).Fractional())
	})
	d("amount", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(ratToFloat(moneyOf(self).Amount()))
	})
	d("to_f", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(moneyOf(self).ToF())
	})
	d("to_i", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(moneyOf(self).ToI())
	})
	d("currency", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &Currency{c: moneyOf(self).Currency()}
	})
	d("symbol", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(moneyOf(self).Symbol())
	})
	d("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(moneyOf(self).String())
	})
	d("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(moneyOf(self).Inspect())
	})
	d("zero?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(moneyOf(self).Zero())
	})
	d("positive?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(moneyOf(self).Positive())
	})
	d("negative?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(moneyOf(self).Negative())
	})

	// Arithmetic. +/- require a Money of a matching currency (a mismatch raises
	// Money::Currency::DifferentCurrencyError); * takes a scalar.
	d("+", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := moneyOf(self).Add(moneyArg(args))
		return moneyResult(out, err)
	})
	d("-", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := moneyOf(self).Sub(moneyArg(args))
		return moneyResult(out, err)
	})
	d("*", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return &Money{m: moneyOf(self).Mul(moneyRat(args[0]))}
	})
	d("abs", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &Money{m: moneyOf(self).Abs()}
	})
	d("<=>", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		c, err := moneyOf(self).Cmp(moneyArg(args))
		if err != nil {
			return object.NilV
		}
		return object.IntValue(int64(c))
	})
	d("==", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := object.KindOK[*Money](args[0])
		return object.Bool(ok && moneyOf(self).Eql(other.m))
	})
	d("<", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(moneyCmp(self, args) < 0)
	})
	d(">", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(moneyCmp(self, args) > 0)
	})
	d("<=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(moneyCmp(self, args) <= 0)
	})
	d(">=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(moneyCmp(self, args) >= 0)
	})

	// Penny distribution: allocate([w1, w2, ...]) and split(n).
	d("allocate", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return moneySlice(moneyOf(self).Allocate(moneyInt64Slice(args[0])))
	})
	d("split", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return moneySlice(moneyOf(self).Split(int(intArg(args[0]))))
	})

	// Exchange and formatting.
	d("exchange_to", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := moneyOf(self).ExchangeTo(vm.currencyArg(args[0]))
		if err != nil {
			raise("Money::Bank::UnknownRate", "%s", err.Error())
		}
		return &Money{m: out}
	})
	d("format", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(moneyOf(self).Format(moneyFormatOpts(moneyOptsArg(args))))
	})
}

// registerMoneyCurrency installs the Money::Currency class (new + readers).
func (vm *VM) registerMoneyCurrency(cls *RClass) {
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			return &Currency{c: vm.currencyArg(args[0])}
		}}
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	d("id", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(object.Kind[*Currency](self).c.ID)
	})
	d("iso_code", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(object.Kind[*Currency](self).c.ISOCode)
	})
	d("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(object.Kind[*Currency](self).c.Name)
	})
	d("symbol", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(object.Kind[*Currency](self).c.SymbolOrDefault())
	})
	d("subunit_to_unit", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(object.Kind[*Currency](self).c.SubunitToUnit)
	})
	d("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(object.Kind[*Currency](self).c.String())
	})
	d("==", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := object.KindOK[*Currency](args[0])
		return object.Bool(ok && object.Kind[*Currency](self).c.Equal(other.c))
	})
}

// registerMoneyErrors installs the Money error tree mirroring the gem
// (Money::Currency::UnknownCurrency and Money::Bank::UnknownRate /
// DifferentCurrencyError < StandardError). Each is registered scoped and flat.
func (vm *VM) registerMoneyErrors(cls, curCls *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	bank := newClass("Money::Bank", nil)
	bank.isModule = true
	cls.consts["Bank"] = bank
	vm.consts["Money::Bank"] = bank

	reg := func(host *RClass, simple, qualified string) {
		c := newClass(qualified, std)
		host.consts[simple] = c
		vm.consts[qualified] = c
	}
	reg(curCls, "UnknownCurrency", "Money::Currency::UnknownCurrency")
	reg(cls, "DifferentCurrencyError", "Money::DifferentCurrencyError")
	reg(bank, "UnknownRate", "Money::Bank::UnknownRate")
}
