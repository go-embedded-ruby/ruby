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
		{object.Integer(1), format.KindInteger},
		{&object.Bignum{I: big.NewInt(1)}, format.KindInteger},
		{object.Float(1.5), format.KindFloat},
		{object.NewString("x"), format.KindString},
		{object.Symbol("s"), format.KindSymbol},
		{&object.Array{Elems: []object.Value{object.Integer(1)}}, format.KindArray},
		{object.NewHash(), format.KindHash},
		{object.NilV, format.KindNil},
		{object.Bool(true), format.KindOther},
	}
	for _, c := range cases {
		if got := (formatValue{c.v}).Kind(); got != c.want {
			t.Errorf("Kind(%T) = %v, want %v", c.v, got, c.want)
		}
	}
}

// TestFormatValueInspect covers formatValue.Inspect (the %p backing), which the
// library calls when rendering an inspected value.
func TestFormatValueInspect(t *testing.T) {
	if got := (formatValue{object.NewString("a")}).Inspect(); got != `"a"` {
		t.Fatalf("Inspect = %q, want %q", got, `"a"`)
	}
	// Drive it through the formatter too, so the %p path is exercised end to end.
	if got := formatString("%p", []object.Value{object.NewString("a")}); got != `"a"` {
		t.Fatalf("%%p = %q, want %q", got, `"a"`)
	}
}

// TestFormatNamedArgsNonSymbolKey covers formatNamedArgs's skip of a non-Symbol
// hash key: only %<name>/%{name}-addressable symbol keys are carried, so a
// String key is dropped and referencing it raises the MRI KeyError.
func TestFormatNamedArgsNonSymbolKey(t *testing.T) {
	h := object.NewHash()
	h.Set(object.NewString("a"), object.Integer(1)) // non-symbol key: skipped
	h.Set(object.Symbol("b"), object.Integer(2))    // symbol key: carried
	na := formatNamedArgs([]object.Value{h})
	// The symbol key resolves...
	if got := formatString("%<b>d", []object.Value{h}); got != "2" {
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
	formatString("%<a>d", []object.Value{h})
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
