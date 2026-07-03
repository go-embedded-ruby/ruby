// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io"
	"testing"

	libcsv "github.com/go-ruby-csv/csv"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestCSVRaiseErrNil covers raiseCSVErr's nil arm: a successful library call
// passes nil, which must return without raising. It is the path every successful
// parse / generate takes, exercised here directly so the arm is pinned.
func TestCSVRaiseErrNil(t *testing.T) {
	raiseCSVErr(nil) // must not panic
}

// TestCSVRaiseErrRuntime covers raiseCSVErr's non-malformed arm: any error that
// is not a *MalformedCSVError becomes a Ruby RuntimeError. The library's parse /
// generate only ever return a *MalformedCSVError (or nil), so this defensive arm
// is unreachable from a Ruby program and is exercised here with a plain error.
func TestCSVRaiseErrRuntime(t *testing.T) {
	defer func() {
		re, ok := recover().(RubyError)
		if !ok || re.Class != "RuntimeError" || re.Message != "boom" {
			t.Fatalf("want RuntimeError boom, got %v", recover())
		}
	}()
	raiseCSVErr(errors.New("boom"))
}

// TestCSVFieldToRubyDefault covers csvFieldToRuby's default arm: a Go value
// outside the enumerated field types (the library never surfaces one) is
// rendered through one-field generation. A bool is used as such a value; the
// generated text is "true".
func TestCSVFieldToRubyDefault(t *testing.T) {
	vm := New(io.Discard)
	got := vm.csvFieldToRuby(true)
	s, ok := object.KindOK[*object.String](got)
	if !ok || s.Str() != "true\n" {
		t.Fatalf("csvFieldToRuby(true) = %v, want String \"true\\n\"", got)
	}
}

// TestCSVFieldToRubyObjectValue covers the object.Value arm: a stored Ruby value
// (a :nil_value / :empty_value substitution) round-trips unchanged.
func TestCSVFieldToRubyObjectValue(t *testing.T) {
	vm := New(io.Discard)
	in := object.Integer(7)
	if got := vm.csvFieldToRuby(in); got != object.IntValue(int64(in)) {
		t.Fatalf("csvFieldToRuby(Integer 7) = %v, want it unchanged", got)
	}
}

// TestCSVParseLineRowNoTable covers csvParseLineRow's non-Table arm: when the
// options carry no headers, Parse returns [][]any rather than a *Table, so the
// type assertion fails and nil is returned. csvParseLineRow is only called with
// headers set in practice, so this arm is exercised here directly with empty
// options (no headers).
func TestCSVParseLineRowNoTable(t *testing.T) {
	vm := New(io.Discard)
	if got := vm.csvParseLineRow("a,b", libcsv.Options{}); !object.IsNil(got) {
		t.Fatalf("csvParseLineRow without headers = %v, want nil", got)
	}
}
