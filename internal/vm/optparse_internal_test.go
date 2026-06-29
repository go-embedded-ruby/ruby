// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
	optparse "github.com/go-ruby-optparse/optparse"
)

// TestOptValueToRubyEdges covers optValueToRuby / optListLookup branches that the
// library never produces through normal parsing: a Match value of an unexpected Go
// type (the defensive default arm → nil) and a list match whose value is not among
// the recorded candidate keys (optListLookup's no-match → the literal String).
func TestOptValueToRubyEdges(t *testing.T) {
	vm := New(&bytes.Buffer{})
	op := &OptionParser{
		listVals: map[int][]object.Value{0: {object.Symbol("big")}},
		listKeys: map[int][]string{0: {"big"}},
	}

	// An unknown Go value type falls through to nil.
	if v := vm.optValueToRuby(op, optparse.Match{SpecIndex: 0, Value: struct{}{}}); v.Truthy() {
		t.Errorf("unknown value type: got %v, want nil", v)
	}
	// A list spec whose matched string is not a recorded key reports the literal
	// String (optListLookup returns nil, so optValueToRuby keeps the raw value).
	if v := vm.optValueToRuby(op, optparse.Match{SpecIndex: 0, Value: "unlisted"}); v.ToS() != "unlisted" {
		t.Errorf("unlisted key: got %v, want \"unlisted\"", v.ToS())
	}
}

// TestOptRaiseDefensiveBranches covers the two otherwise-unreachable fallbacks of
// optRaise: a non-*ParseError error (the library only ever returns a *ParseError,
// so this is reached here by hand), and a *ParseError whose Class() does not map
// to a registered OptionParser:: subclass (likewise impossible from the seven-node
// taxonomy, forced here with an out-of-range kind). Both must panic with the base
// OptionParser::ParseError carrying the error message.
func TestOptRaiseDefensiveBranches(t *testing.T) {
	vm := New(&bytes.Buffer{})

	// A plain error → base ParseError with the error's message.
	func() {
		defer func() {
			r := recover()
			re, ok := r.(RubyError)
			if !ok || re.Class != "OptionParser::ParseError" || re.Message != "boom" {
				t.Fatalf("non-ParseError: got %#v", r)
			}
		}()
		vm.optRaise(errors.New("boom"))
	}()

	// A ParseError with a kind that has no rubyClass entry → its Class() is "", so
	// the subclass lookup misses and the base ParseError is raised with #Error().
	func() {
		defer func() {
			r := recover()
			re, ok := r.(RubyError)
			if !ok || re.Class != "OptionParser::ParseError" {
				t.Fatalf("unmapped class: got %#v", r)
			}
		}()
		vm.optRaise(&optparse.ParseError{Kind: optparse.ErrorKind(99), Args: []string{"x"}})
	}()
}

// TestOptStrPtr covers optStrPtr's nil and non-nil arms directly (the nil arm is
// otherwise only reached through version/release before they are set).
func TestOptStrPtr(t *testing.T) {
	if v := optStrPtr(nil); v.ToS() != "" || v.Truthy() {
		t.Errorf("optStrPtr(nil) = %v, want nil", v)
	}
	s := "x"
	if v := optStrPtr(&s); v.ToS() != "x" {
		t.Errorf("optStrPtr(&\"x\") = %v", v)
	}
}

// TestOptionParserValueSurface covers the native value type's ToS/Inspect/Truthy.
func TestOptionParserValueSurface(t *testing.T) {
	op := &OptionParser{}
	if op.ToS() != "#<OptionParser>" || op.Inspect() != "#<OptionParser>" || !op.Truthy() {
		t.Errorf("value surface: %q / %q / %v", op.ToS(), op.Inspect(), op.Truthy())
	}
}
