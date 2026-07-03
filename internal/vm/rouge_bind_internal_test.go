// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestRougeLexerHandleValue covers the object.Value methods of the opaque lexer
// handle: they exist only so the handle can live in an ivar and are never reached
// from Ruby.
func TestRougeLexerHandleValue(t *testing.T) {
	h := &rougeLexer{}
	if h.ToS() != "#<Rouge::Lexer>" {
		t.Errorf("ToS = %q", h.ToS())
	}
	if h.Inspect() != h.ToS() {
		t.Errorf("Inspect = %q", h.Inspect())
	}
	if !h.Truthy() {
		t.Error("handle should be truthy")
	}
}

// TestRougeLexerHandleMissing covers rougeLexerHandle's raise arm: a Rouge::Lexer
// instance with no stored handle raises Rouge::Error. A Ruby program cannot build
// such an instance (only Lexer.find installs the handle), so the arm is exercised
// here directly.
func TestRougeLexerHandleMissing(t *testing.T) {
	vm := New(io.Discard)
	cls := object.Kind[*RClass](vm.consts["Rouge::Lexer"])
	inst := &RObject{class: cls, ivars: map[string]object.Value{}}
	defer func() {
		re, ok := recover().(RubyError)
		if !ok || re.Class != "Rouge::Error" {
			t.Fatalf("want Rouge::Error, got %v", recover())
		}
	}()
	rougeLexerHandle(inst)
}

// TestRougeNameArgToS covers rougeNameArg's #to_s fall-through arm, which a String
// or Symbol argument never reaches. An Integer stringifies to its decimal text.
func TestRougeNameArgToS(t *testing.T) {
	if got := rougeNameArg(object.Integer(7)); got != "7" {
		t.Errorf("rougeNameArg(int) = %q", got)
	}
}

// TestRougeStringArgToS covers rougeStringArg's #to_s fall-through arm for a
// non-String source value.
func TestRougeStringArgToS(t *testing.T) {
	if got := rougeStringArg(object.Integer(9)); got != "9" {
		t.Errorf("rougeStringArg(int) = %q", got)
	}
}
