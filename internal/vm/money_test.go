// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestMoneyConstants covers the Money class, Money::Currency and the error tree
// (require "money").
func TestMoneyConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "money"; p Money.is_a?(Class)`, "true\n"},
		{`p require "money"`, "true\n"},
		{`require "money"; p require "money"`, "false\n"},
		{`require "money"; p Money::Currency.is_a?(Class)`, "true\n"},
		{`require "money"; p Money::Currency::UnknownCurrency < StandardError`, "true\n"},
		{`require "money"; p Money::DifferentCurrencyError < StandardError`, "true\n"},
		{`require "money"; p Money::Bank::UnknownRate < StandardError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMoneyBasics covers Money.new and the reader surface.
func TestMoneyBasics(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "money"; p Money.new(1000, "USD").cents`, "1000\n"},
		{`require "money"; p Money.new(1000, "USD").fractional`, "1000\n"},
		{`require "money"; p Money.new(1000, "USD").amount`, "10.0\n"},
		{`require "money"; p Money.new(1000, "USD").to_i`, "10\n"},
		{`require "money"; p Money.new(1000, "USD").to_f`, "10.0\n"},
		{`require "money"; p Money.new(1000, "USD").currency.iso_code`, "\"USD\"\n"},
		{`require "money"; p Money.new(1000, "USD").symbol`, "\"$\"\n"},
		{`require "money"; p Money.new(0, "USD").zero?`, "true\n"},
		{`require "money"; p Money.new(1, "USD").positive?`, "true\n"},
		{`require "money"; p Money.new(-1, "USD").negative?`, "true\n"},
		{`require "money"; p Money.new(1000, "USD").class.name`, "\"Money\"\n"},
		// A default currency (no second arg) is USD.
		{`require "money"; p Money.new(1000).currency.iso_code`, "\"USD\"\n"},
		// A Symbol currency id works too.
		{`require "money"; p Money.new(1000, :eur).currency.iso_code`, "\"EUR\"\n"},
		// A Money::Currency object as the currency arg.
		{`require "money"; c = Money::Currency.new("GBP"); p Money.new(500, c).currency.iso_code`, "\"GBP\"\n"},
		{`require "money"; p Money.new(1000, "USD").to_s`, "\"10.00\"\n"},
		{`require "money"; p Money.new(1000, "USD").inspect.start_with?("#<Money")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMoneyArithmetic covers +, -, *, unary -, abs and the comparisons dispatched
// through binaryOp.
func TestMoneyArithmetic(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "money"; p (Money.new(1000, "USD") + Money.new(500, "USD")).cents`, "1500\n"},
		{`require "money"; p (Money.new(1000, "USD") - Money.new(500, "USD")).cents`, "500\n"},
		{`require "money"; p (Money.new(1000, "USD") * 3).cents`, "3000\n"},
		{`require "money"; p (-Money.new(1000, "USD")).cents`, "-1000\n"},
		{`require "money"; p Money.new(-1000, "USD").abs.cents`, "1000\n"},
		{`require "money"; p (Money.new(1000, "USD") == Money.new(1000, "USD"))`, "true\n"},
		{`require "money"; p (Money.new(1000, "USD") == Money.new(500, "USD"))`, "false\n"},
		// == against a non-Money is false, not an error.
		{`require "money"; p (Money.new(1000, "USD") == 1000)`, "false\n"},
		{`require "money"; p (Money.new(1000, "USD") > Money.new(500, "USD"))`, "true\n"},
		{`require "money"; p (Money.new(500, "USD") < Money.new(1000, "USD"))`, "true\n"},
		{`require "money"; p (Money.new(1000, "USD") >= Money.new(1000, "USD"))`, "true\n"},
		{`require "money"; p (Money.new(1000, "USD") <= Money.new(1000, "USD"))`, "true\n"},
		{`require "money"; p (Money.new(1000, "USD") <=> Money.new(500, "USD"))`, "1\n"},
		// * by a Float scalar.
		{`require "money"; p (Money.new(1000, "USD") * 1.5).cents`, "1500\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMoneyDistribution covers allocate and split penny distribution.
func TestMoneyDistribution(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "money"; p Money.new(100, "USD").split(3).map(&:cents)`, "[34, 33, 33]\n"},
		{`require "money"; p Money.new(100, "USD").allocate([1, 1, 1]).map(&:cents)`, "[34, 33, 33]\n"},
		{`require "money"; p Money.new(100, "USD").allocate([1, 1, 2]).map(&:cents)`, "[25, 25, 50]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMoneyFormat covers Money#format and its options.
func TestMoneyFormat(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "money"; p Money.new(1000, "USD").format`, "\"$10.00\"\n"},
		{`require "money"; p Money.new(1000, "USD").format(no_cents: true)`, "\"$10\"\n"},
		{`require "money"; p Money.new(1000, "USD").format(symbol: false)`, "\"10.00\"\n"},
		{`require "money"; p Money.new(1000, "USD").format(symbol: "USD ")`, "\"USD 10.00\"\n"},
		{`require "money"; p Money.new(1000, "USD").format(with_currency: true)`, "\"$10.00 USD\"\n"},
		{`require "money"; p Money.new(1000, "USD").format(sign_positive: true)`, "\"$+10.00\"\n"},
		// symbol: true keeps the currency symbol.
		{`require "money"; p Money.new(1000, "USD").format(symbol: true)`, "\"$10.00\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMoneyExchange covers Money.add_rate and Money#exchange_to.
func TestMoneyExchange(t *testing.T) {
	got := eval(t, `require "money"
Money.add_rate("USD", "EUR", 0.9)
p Money.new(1000, "USD").exchange_to("EUR").cents`)
	if got != "900\n" {
		t.Errorf("exchange: got %q", got)
	}
	// exchange_to a currency with no known rate raises Money::Bank::UnknownRate.
	got = eval(t, `require "money"
begin
  Money.new(1000, "USD").exchange_to("JPY")
rescue Money::Bank::UnknownRate
  puts "norate"
end`)
	if !strings.Contains(got, "norate") {
		t.Errorf("no rate: got %q", got)
	}
}

// TestMoneyCurrency covers the Money::Currency class surface.
func TestMoneyCurrency(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "money"; p Money::Currency.new("USD").iso_code`, "\"USD\"\n"},
		{`require "money"; p Money::Currency.new("USD").id`, ":usd\n"},
		{`require "money"; p Money::Currency.new("USD").name`, "\"United States Dollar\"\n"},
		{`require "money"; p Money::Currency.new("USD").symbol`, "\"$\"\n"},
		{`require "money"; p Money::Currency.new("USD").subunit_to_unit`, "100\n"},
		{`require "money"; p Money::Currency.new("USD").to_s`, "\"USD\"\n"},
		{`require "money"; p (Money::Currency.new("USD") == Money::Currency.new("USD"))`, "true\n"},
		{`require "money"; p (Money::Currency.new("USD") == Money::Currency.new("EUR"))`, "false\n"},
		{`require "money"; p (Money::Currency.new("USD") == 5)`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMoneyErrors covers the error and arity paths.
func TestMoneyErrors(t *testing.T) {
	// A currency-mismatched + raises Money::DifferentCurrencyError (there is no
	// USD->EUR rate at the start of a fresh VM).
	got := eval(t, `require "money"
begin
  Money.new(1000, "USD") + Money.new(500, "EUR")
rescue Money::DifferentCurrencyError
  puts "mismatch"
end`)
	if !strings.Contains(got, "mismatch") {
		t.Errorf("currency mismatch: got %q", got)
	}
	// A currency-mismatched comparison raises Money::DifferentCurrencyError.
	got = eval(t, `require "money"
begin
  Money.new(1000, "USD") < Money.new(500, "EUR")
rescue Money::DifferentCurrencyError
  puts "cmpmismatch"
end`)
	if !strings.Contains(got, "cmpmismatch") {
		t.Errorf("cmp mismatch: got %q", got)
	}
	// <=> of different currencies returns nil (Comparable's contract).
	if got := eval(t, `require "money"; p (Money.new(1000, "USD") <=> Money.new(500, "EUR"))`); got != "nil\n" {
		t.Errorf("cmp spaceship mismatch: got %q", got)
	}
	// An unknown currency id raises Money::Currency::UnknownCurrency.
	got = eval(t, `require "money"
begin
  Money.new(100, "ZZZ")
rescue Money::Currency::UnknownCurrency
  puts "badcur"
end`)
	if !strings.Contains(got, "badcur") {
		t.Errorf("bad currency: got %q", got)
	}
	// No-argument constructors raise ArgumentError.
	for _, call := range []string{`Money.new`, `Money::Currency.new`} {
		src := `require "money"
begin
  ` + call + `
rescue ArgumentError
  puts "arity"
end`
		if got := eval(t, src); !strings.Contains(got, "arity") {
			t.Errorf("%s no-arg: got %q", call, got)
		}
	}
	// Money.add_rate with too few arguments raises ArgumentError.
	got = eval(t, `require "money"
begin
  Money.add_rate("USD")
rescue ArgumentError
  puts "raterity"
end`)
	if !strings.Contains(got, "raterity") {
		t.Errorf("add_rate arity: got %q", got)
	}
	// default_bank is an opaque handle (nil today) but the method exists.
	if got := eval(t, `require "money"; p Money.default_bank`); got != "nil\n" {
		t.Errorf("default_bank: got %q", got)
	}
	// add_rate returns the rate it was given.
	if got := eval(t, `require "money"; p Money.add_rate("USD", "EUR", 0.9)`); got != "0.9\n" {
		t.Errorf("add_rate return: got %q", got)
	}
}
