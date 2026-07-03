// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"math/big"
	"testing"

	format "github.com/go-ruby-format/format"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestFormatValueKind covers every arm of formatValue.Kind, mapping each rbgo
// dynamic type to the library's Kind enumeration (including the composite and
// Nil/Other arms the Ruby-level %s/%p tests do not all reach directly).
func TestFormatValueKind(t *testing.T) {
	cases := []struct {
		v    object.Value
		want format.Kind
	}{
		{object.IntValue(int64(object.Integer(1))), format.KindInteger},
		{object.Wrap(&object.Bignum{I: big.NewInt(1)}), format.KindInteger},
		{object.FloatValue(float64(object.Float(1.5))), format.KindFloat},
		{object.Wrap(object.NewString("x")), format.KindString},
		{object.SymVal(string(object.Symbol("s"))), format.KindSymbol},
		{object.Wrap(&object.Array{Elems: []object.Value{object.IntValue(int64(object.Integer(1)))}}), format.KindArray},
		{object.Wrap(object.NewHash()), format.KindHash},
		{object.NilVal(), format.KindNil},
		{object.BoolValue(bool(object.Bool(true))), format.KindOther},
	}
	for _, c := range cases {
		if got := (formatValue{c.v}).Kind(); got != c.want {
			t.Errorf("Kind(%T) = %v, want %v", c.v, got, c.want)
		}
	}
}

// TestFormatValueInt64Fast covers every arm of formatValue.Int64Fast, the
// allocation-free fast path the library's integer conversions take: a native
// Integer and an int64-range Bignum report their value; an out-of-range Bignum
// and any non-integer decline so the formatter uses the precise Int() path.
func TestFormatValueInt64Fast(t *testing.T) {
	big64 := new(big.Int).Lsh(big.NewInt(1), 100) // 2^100, far beyond int64
	cases := []struct {
		v      object.Value
		wantN  int64
		wantOK bool
	}{
		{object.IntValue(int64(object.Integer(-42))), -42, true},
		{object.Wrap(&object.Bignum{I: big.NewInt(7)}), 7, true},  // Bignum that fits int64
		{object.Wrap(&object.Bignum{I: big64}), 0, false},         // Bignum exceeding int64
		{object.FloatValue(float64(object.Float(1.5))), 0, false}, // non-integer declines
		{object.Wrap(object.NewString("9")), 0, false},            // String declines (Int() parses it)
	}
	for _, c := range cases {
		n, ok := (formatValue{c.v}).Int64Fast()
		if n != c.wantN || ok != c.wantOK {
			t.Errorf("Int64Fast(%T=%v) = (%d, %v), want (%d, %v)", c.v, c.v, n, ok, c.wantN, c.wantOK)
		}
	}
}

// TestFormatValueInspect covers formatValue.Inspect (the %p backing), which the
// library calls when rendering an inspected value.
func TestFormatValueInspect(t *testing.T) {
	if got := (formatValue{object.Wrap(object.NewString("a"))}).Inspect(); got != `"a"` {
		t.Fatalf("Inspect = %q, want %q", got, `"a"`)
	}
	// Drive it through the formatter too, so the %p path is exercised end to end.
	if got := formatString("%p", []object.Value{object.Wrap(object.NewString("a"))}); got != `"a"` {
		t.Fatalf("%%p = %q, want %q", got, `"a"`)
	}
}

// TestFormatNamedArgsNonSymbolKey covers formatNamedArgs's skip of a non-Symbol
// hash key: only %<name>/%{name}-addressable symbol keys are carried, so a
// String key is dropped and referencing it raises the MRI KeyError.
func TestFormatNamedArgsNonSymbolKey(t *testing.T) {
	h := object.NewHash()
	h.Set(object.Wrap(object.NewString("a")), object.IntValue(int64(object.Integer(1))))        // non-symbol key: skipped
	h.Set(object.SymVal(string(object.Symbol("b"))), object.IntValue(int64(object.Integer(2)))) // symbol key: carried
	na := formatNamedArgs([]object.Value{object.Wrap(h)})
	// The symbol key resolves...
	if got := formatString("%<b>d", []object.Value{object.Wrap(h)}); got != "2" {
		t.Fatalf("%%<b>d = %q, want %q", got, "2")
	}
	// ...while the dropped string key raises a KeyError.
	defer func() {
		re, ok := recover().(RubyError)
		if !ok || re.Class != "KeyError" {
			t.Fatalf("want KeyError for the skipped key, got %v", re)
		}
	}()
	_ = na
	formatString("%<a>d", []object.Value{object.Wrap(h)})
}

// TestRaiseFormatErrorFallback covers raiseFormatError's defensive non-
// *format.Error arm, which the library never reaches but which re-raises any
// other error as an ArgumentError.
func TestRaiseFormatErrorFallback(t *testing.T) {
	defer func() {
		re, ok := recover().(RubyError)
		if !ok || re.Class != "ArgumentError" || re.Message != "boom" {
			t.Fatalf("want ArgumentError boom, got %v", re)
		}
	}()
	raiseFormatError(errors.New("boom"))
}
