// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	mustache "github.com/go-ruby-mustache/mustache"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestMustacheToBridge covers the Go-only arms of toMustache the Ruby tests do
// not reach directly: a plain Go nil and the object.Nil singleton (both map to a
// nil context), and confirms the common value shapes round-trip through the
// library value model.
func TestMustacheToBridge(t *testing.T) {
	vm := New(nil)
	if got := toMustache(vm, object.NilVal()); got != nil {
		t.Errorf("plain nil: got %#v want nil", got)
	}
	if got := toMustache(vm, object.NilVal()); got != nil {
		t.Errorf("object.Nil: got %#v want nil", got)
	}
	if got := toMustache(vm, object.BoolValue(bool(object.Bool(true)))); got != true {
		t.Errorf("bool: got %#v want true", got)
	}
	if got := toMustache(vm, object.IntValue(int64(object.Integer(7)))); got != int64(7) {
		t.Errorf("int: got %#v want 7", got)
	}
	if got := toMustache(vm, object.FloatValue(float64(object.Float(1.5)))); got != 1.5 {
		t.Errorf("float: got %#v want 1.5", got)
	}
	if got := toMustache(vm, object.SymVal(string(object.Symbol("s")))); got != mustache.Symbol("s") {
		t.Errorf("symbol: got %#v want Symbol(s)", got)
	}
}

// TestMustacheKeyDefault covers mustacheKey's fall-through (a non-Symbol,
// non-String key rendered via to_s).
func TestMustacheKeyDefault(t *testing.T) {
	if got := mustacheKey(object.IntValue(int64(object.Integer(3)))); got != "3" {
		t.Errorf("integer key: got %#v want \"3\"", got)
	}
	if got := mustacheKey(object.Wrap(object.NewString("k"))); got != "k" {
		t.Errorf("string key: got %#v want \"k\"", got)
	}
	if got := mustacheKey(object.SymVal(string(object.Symbol("y")))); got != mustache.Symbol("y") {
		t.Errorf("symbol key: got %#v want Symbol(y)", got)
	}
}
