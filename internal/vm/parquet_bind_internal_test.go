// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"testing"

	libparquet "github.com/go-ruby-parquet/parquet"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestParquetRaiseError covers raiseParquetErr's kind→class mapping and its
// RuntimeError fallback for a non-*parquet.Error, reached directly because every
// error the API returns through the Ruby surface is already a *parquet.Error.
func TestParquetRaiseError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{libparquet.ErrType, "TypeError"},
		{libparquet.ErrIndex, "IndexError"},
		{libparquet.ErrArgument, "ArgumentError"},
		{libparquet.ErrIO, "Parquet::Error::Io"},
		{libparquet.ErrNotImplemented, "NotImplementedError"},
		{&libparquet.Error{Kind: libparquet.KindError}, "Parquet::Error"},
		{errors.New("boom"), "RuntimeError"},
	}
	for _, c := range cases {
		func() {
			defer func() {
				re, ok := recover().(RubyError)
				if !ok {
					t.Fatalf("raiseParquetErr(%v): did not raise a RubyError", c.err)
				}
				if re.Class != c.want {
					t.Errorf("raiseParquetErr(%v): class %q want %q", c.err, re.Class, c.want)
				}
			}()
			raiseParquetErr(c.err)
		}()
	}
	// A nil error must not raise.
	raiseParquetErr(nil)
}

// TestParquetValueMethods covers the Go-level object.Value surface of the two
// wrapper types (Truthy, ToS, Inspect) reached directly rather than through
// dispatch.
func TestParquetValueMethods(t *testing.T) {
	r := &ParquetReader{}
	w := &ParquetWriter{}
	if !r.Truthy() || !w.Truthy() {
		t.Error("Parquet wrappers should be truthy")
	}
	if r.ToS() != "#<Parquet::ArrowFileReader>" || r.Inspect() != r.ToS() {
		t.Errorf("reader render = %q / %q", r.ToS(), r.Inspect())
	}
	if w.ToS() != "#<Parquet::ArrowFileWriter>" || w.Inspect() != w.ToS() {
		t.Errorf("writer render = %q / %q", w.ToS(), w.Inspect())
	}
}

// TestParquetClassOf proves the two wrapper types report their bound class
// through the interpreter's classOf dispatch basis.
func TestParquetClassOf(t *testing.T) {
	vm := New(&bytes.Buffer{})
	pairs := []struct {
		v    object.Value
		want *RClass
	}{
		{&ParquetReader{}, vm.consts["Parquet::ArrowFileReader"].(*RClass)},
		{&ParquetWriter{}, vm.consts["Parquet::ArrowFileWriter"].(*RClass)},
	}
	for _, p := range pairs {
		if got := vm.classOf(p.v); got != p.want {
			t.Errorf("classOf(%T) = %v want %v", p.v, got, p.want)
		}
	}
}
