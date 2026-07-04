// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"os"

	libarrow "github.com/go-ruby-arrow/arrow"
	libparquet "github.com/go-ruby-parquet/parquet"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin value bridge between rbgo's Ruby object graph
// (object.Value and the Arrow module's table/schema wrappers) and
// github.com/go-ruby-parquet/parquet. The Parquet format, the compression codecs
// and the Arrow bridge all live in that library; this file only coerces Ruby
// arguments (Arrow::Table / Arrow::Schema wrappers, path/bytes Strings, the
// writer-option Hash) into the library's calls, wraps the go-ruby-arrow tables it
// yields back into the Arrow module's Ruby wrappers, and re-raises library errors
// as the faithful Ruby exception. See parquet.go for the class surface.
//
// The Arrow tables move through unchanged: a go-ruby-parquet read yields a
// *github.com/go-ruby-arrow/arrow.Table, the very type ArrowTable wraps, so the
// Parquet and Arrow modules interoperate at the Ruby level with no re-encoding.

// raiseParquetErr re-raises a library error as the faithful Ruby exception. The
// library tags each error with an ErrorKind whose RubyClass() names the Ruby
// class red-parquet raises (TypeError / IndexError / ArgumentError /
// NotImplementedError / Parquet::Error::Io / Parquet::Error). A non-parquet error
// falls back to RuntimeError. It never returns when err is non-nil.
func raiseParquetErr(err error) {
	if err == nil {
		return
	}
	var pe *libparquet.Error
	if errors.As(err, &pe) {
		raise(pe.RubyClass(), "%s", err.Error())
	}
	raise("RuntimeError", "%s", err.Error())
}

// parquetTableOK wraps a (*Table, error) library call: it re-raises any error as
// the matching Ruby exception, then returns the go-ruby-arrow table as an
// Arrow::Table so the read result interoperates with the Arrow module.
func parquetTableOK(t *libarrow.Table, err error) object.Value {
	raiseParquetErr(err)
	return &ArrowTable{t: t}
}

// parquetArity raises a Ruby ArgumentError when fewer than want arguments were
// given, matching MRI's arity error for the native method.
func parquetArity(args []object.Value, want int, name string) {
	if len(args) < want {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d) for %s",
			len(args), want, name)
	}
}

// parquetInt coerces a Ruby Integer argument to an int64, raising TypeError for a
// non-Integer (a row-group index / size must be an Integer).
func parquetInt(v object.Value) int64 {
	n, ok := object.BigOf(v)
	if !ok {
		raise("TypeError", "expected an Integer, got %s", v.Inspect())
	}
	return n.Int64()
}

// parquetName renders a Ruby String or Symbol as a plain name (used for the
// compression option), raising TypeError otherwise.
func parquetName(v object.Value) string {
	switch k := v.(type) {
	case *object.String:
		return k.Str()
	case object.Symbol:
		return string(k)
	}
	raise("TypeError", "expected a String or Symbol, got %s", v.Inspect())
	panic("unreachable")
}

// parquetStringArg asserts an argument is a Ruby String, raising TypeError with
// the given call context otherwise.
func parquetStringArg(v object.Value, ctx string) *object.String {
	s, ok := v.(*object.String)
	if !ok {
		raise("TypeError", "%s expects a String, got %s", ctx, v.Inspect())
	}
	return s
}

// parquetTableArg asserts an argument is an Arrow::Table and unwraps its
// go-ruby-arrow table, raising TypeError otherwise.
func parquetTableArg(v object.Value) *libarrow.Table {
	t, ok := v.(*ArrowTable)
	if !ok {
		raise("TypeError", "expected an Arrow::Table, got %s", v.Inspect())
	}
	return t.t
}

// parquetSchemaArg asserts an argument is an Arrow::Schema and unwraps its
// go-ruby-arrow schema, raising TypeError otherwise.
func parquetSchemaArg(v object.Value) *libarrow.Schema {
	s, ok := v.(*ArrowSchema)
	if !ok {
		raise("TypeError", "expected an Arrow::Schema, got %s", v.Inspect())
	}
	return s.s
}

// parquetFileExists reports whether path names an existing regular file, so
// ArrowFileReader.new can distinguish a filesystem path from in-memory bytes.
func parquetFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// parquetWriteArgs resolves the trailing write arguments shared by Parquet.write
// and ArrowFileWriter.new: an optional String path (an explicit nil selects the
// bytes form) and an optional Hash of writer options. An unexpected argument
// raises ArgumentError.
func parquetWriteArgs(args []object.Value) (path string, havePath bool, opts []libparquet.WriteOption) {
	for _, a := range args {
		switch x := a.(type) {
		case object.Nil:
			// an explicit nil path selects the bytes-returning form
		case *object.String:
			path, havePath = x.Str(), true
		case *object.Hash:
			opts = parquetOptsFromHash(x)
		default:
			raise("ArgumentError", "unexpected write argument %s", a.Inspect())
		}
	}
	return
}

// parquetOptsFromHash maps a writer-option Hash onto the library's WriteOptions,
// mirroring red-parquet's Parquet::WriterProperties knobs: :compression (a
// Symbol/String codec name), :row_group_size (an Integer) and :dictionary (a
// boolean). An unknown compression name raises ArgumentError.
func parquetOptsFromHash(h *object.Hash) []libparquet.WriteOption {
	var opts []libparquet.WriteOption
	if v, ok := h.Get(object.Symbol("compression")); ok {
		c, err := libparquet.ParseCompression(parquetName(v))
		raiseParquetErr(err)
		opts = append(opts, libparquet.WithCompression(c))
	}
	if v, ok := h.Get(object.Symbol("row_group_size")); ok {
		opts = append(opts, libparquet.WithRowGroupSize(parquetInt(v)))
	}
	if v, ok := h.Get(object.Symbol("dictionary")); ok {
		opts = append(opts, libparquet.WithDictionary(v.Truthy()))
	}
	return opts
}
