// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestBigDecimalFloatToDecimal covers floatToDecimal across the float shapes a
// Ruby program reaches it with: a finite value and the three specials. Each one
// formats (via object.Float#to_s) to a string the go-ruby-bigdecimal library
// parses, so the conversion is total.
func TestBigDecimalFloatToDecimal(t *testing.T) {
	for _, c := range []struct {
		v    float64
		want string
	}{
		{2.5, "0.25e1"},
		{math.NaN(), "NaN"},
		{math.Inf(+1), "Infinity"},
		{math.Inf(-1), "-Infinity"},
	} {
		d := floatToDecimal(c.v)
		if d == nil || d.ToS("") != c.want {
			t.Errorf("floatToDecimal(%v) = %v; want %q", c.v, d, c.want)
		}
	}
}

// TestBigDecimalOpUnsupported covers bigDecimalOp's final NoMethodError arm: a
// bytecode op outside + - * / %. Only those five opcodes reach bigDecimalOp from
// binary(), so the arm is unreachable from a Ruby program and is exercised here
// directly with the comparison opcode OpLt.
func TestBigDecimalOpUnsupported(t *testing.T) {
	defer func() {
		re, ok := recover().(RubyError)
		if !ok || re.Class != "NoMethodError" {
			t.Fatalf("want NoMethodError, got %v", recover())
		}
	}()
	a := newDecimalString("1")
	b := newDecimalString("2")
	bigDecimalOp(bytecode.OpLt, a, object.Wrap(b))
}
