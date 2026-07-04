// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"os"

	libparquet "github.com/go-ruby-parquet/parquet"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-parquet/parquet — the pure-Go (CGO=0),
// red-parquet-faithful port of Apache Parquet's read/write surface on top of the
// official apache/arrow-go Parquet stack — into rbgo as the native `Parquet`
// module (require "parquet"). The library owns the on-disk Parquet format, the
// compression codecs and the Arrow bridge; this file is only the thin shell that
// maps Ruby values onto the library's calls (see parquet_bind.go) and exposes
// the class/method surface red-parquet provides:
//
//	Parquet                        — module: .write / .read / .load convenience
//	Parquet::ArrowFileReader       — random-access reader yielding Arrow::Table
//	Parquet::ArrowFileWriter       — streaming writer of Arrow::Table
//	Parquet::Error (< StandardError) / Parquet::Error::Io — the exception tree
//
// The tables read and written are github.com/go-ruby-arrow/arrow Tables — the
// exact type the `Arrow` module (internal/vm/arrow.go) is bound to — so the two
// modules compose: build an Arrow::Table, persist it here, load it back and hand
// it to any Arrow consumer unchanged. Parquet is a little-endian on-disk format;
// arrow-go performs the byte swap on the big-endian target (s390x), so a file
// round-trips identically across all six supported 64-bit arches.

// ParquetReader is the Ruby wrapper around a go-ruby-parquet ArrowFileReader.
type ParquetReader struct{ r *libparquet.ArrowFileReader }

func (r *ParquetReader) ToS() string     { return "#<Parquet::ArrowFileReader>" }
func (r *ParquetReader) Inspect() string { return r.ToS() }
func (r *ParquetReader) Truthy() bool    { return true }

// ParquetWriter is the Ruby wrapper around a go-ruby-parquet ArrowFileWriter. It
// always writes into an in-memory buffer; on #close it either flushes that buffer
// to the target path (path form) or returns the encoded bytes (in-memory form).
type ParquetWriter struct {
	w        *libparquet.ArrowFileWriter
	buf      *bytes.Buffer
	path     string
	havePath bool
	closed   bool
}

func (w *ParquetWriter) ToS() string     { return "#<Parquet::ArrowFileWriter>" }
func (w *ParquetWriter) Inspect() string { return w.ToS() }
func (w *ParquetWriter) Truthy() bool    { return true }

// registerParquet installs the Parquet module, its convenience module methods,
// the reader/writer classes and the error tree (require "parquet"). It runs
// eagerly at boot; the error tree needs StandardError in place, and it references
// the Arrow classes (installed by registerArrow) only at runtime.
func (vm *VM) registerParquet() {
	mod := newClass("Parquet", nil)
	mod.isModule = true
	vm.consts["Parquet"] = mod

	vm.registerParquetErrors(mod)
	vm.registerParquetModuleMethods(mod)

	mk := func(name string, super *RClass) *RClass {
		full := "Parquet::" + name
		cls := newClass(full, super)
		mod.consts[name] = cls
		vm.consts[full] = cls
		return cls
	}

	vm.registerParquetReader(mk("ArrowFileReader", vm.cObject))
	vm.registerParquetWriter(mk("ArrowFileWriter", vm.cObject))
}

// registerParquetErrors installs the Parquet::Error exception tree:
// Parquet::Error < StandardError and Parquet::Error::Io < Parquet::Error. The
// library tags each failure with a kind whose RubyClass() names the faithful Ruby
// class red-parquet raises, so a re-raised library IO error rescues as
// Parquet::Error::Io; its other kinds map to the pre-existing TypeError /
// IndexError / ArgumentError / NotImplementedError.
func (vm *VM) registerParquetErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)

	errCls := newClass("Parquet::Error", std)
	mod.consts["Error"] = errCls
	vm.consts["Parquet::Error"] = errCls

	ioCls := newClass("Parquet::Error::Io", errCls)
	errCls.consts["Io"] = ioCls
	vm.consts["Parquet::Error::Io"] = ioCls
}

// parquetSMethod installs a class ("singleton") method on a class.
func parquetSMethod(cls *RClass, name string, fn NativeFn) {
	cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
}

// registerParquetModuleMethods installs the Parquet module convenience methods:
// Parquet.write(table, path = nil, opts) writes an Arrow::Table to a Parquet file
// (path form → nil) or returns the encoded Parquet bytes (no path); Parquet.load
// reads a Parquet file at a path back into an Arrow::Table; Parquet.read decodes
// an in-memory Parquet byte String into an Arrow::Table.
func (vm *VM) registerParquetModuleMethods(mod *RClass) {
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	def("write", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		parquetArity(args, 1, "write")
		t := parquetTableArg(args[0])
		path, havePath, opts := parquetWriteArgs(args[1:])
		if havePath {
			raiseParquetErr(libparquet.WriteTable(t, path, opts...))
			return object.NilV
		}
		var buf bytes.Buffer
		raiseParquetErr(libparquet.WriteTableTo(&buf, t, opts...))
		return object.NewStringBytesEnc(buf.Bytes(), "ASCII-8BIT")
	})
	def("load", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		parquetArity(args, 1, "load")
		s := parquetStringArg(args[0], "Parquet.load")
		return parquetTableOK(libparquet.Load(s.Str()))
	})
	def("read", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		parquetArity(args, 1, "read")
		s := parquetStringArg(args[0], "Parquet.read")
		return parquetTableOK(libparquet.ReadTableBytes(s.Bytes()))
	})
}

// registerParquetReader installs Parquet::ArrowFileReader:
// ArrowFileReader.new(source) opens a reader over a filesystem path (when source
// names an existing file) or over in-memory Parquet bytes, and exposes
// read_table / read_row_group (→ Arrow::Table), n_rows / n_row_groups, schema
// (→ Arrow::Schema) and close.
func (vm *VM) registerParquetReader(cls *RClass) {
	parquetSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		parquetArity(args, 1, "new")
		s := parquetStringArg(args[0], "Parquet::ArrowFileReader.new")
		if parquetFileExists(s.Str()) {
			r, err := libparquet.OpenArrowFileReader(s.Str())
			raiseParquetErr(err)
			return &ParquetReader{r: r}
		}
		r, err := libparquet.NewArrowFileReader(bytes.NewReader(s.Bytes()))
		raiseParquetErr(err)
		return &ParquetReader{r: r}
	})

	self := func(v object.Value) *ParquetReader { return v.(*ParquetReader) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("read_table", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return parquetTableOK(self(v).r.ReadTable())
	})
	d("read_row_group", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		parquetArity(args, 1, "read_row_group")
		return parquetTableOK(self(v).r.ReadRowGroup(int(parquetInt(args[0]))))
	})
	nRows := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).r.NumRows())
	}
	d("n_rows", nRows)
	d("num_rows", nRows)
	nRG := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).r.NumRowGroups()))
	}
	d("n_row_groups", nRG)
	d("num_row_groups", nRG)
	d("schema", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s, err := self(v).r.Schema()
		raiseParquetErr(err)
		return &ArrowSchema{s: s}
	})
	d("close", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		raiseParquetErr(self(v).r.Close())
		return object.NilV
	})
	toS := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ToS())
	}
	d("to_s", toS)
	d("inspect", toS)
}

// registerParquetWriter installs Parquet::ArrowFileWriter:
// ArrowFileWriter.new(schema, path = nil, opts) opens a streaming writer bound to
// an Arrow::Schema; #write (aliased #write_table) appends an Arrow::Table; #close
// flushes the footer and either persists to the path (→ nil) or returns the
// encoded Parquet bytes (no path). The write options mirror red-parquet's
// per-file knobs: compression (:uncompressed/:snappy/:gzip/:zstd), row_group_size
// and dictionary.
func (vm *VM) registerParquetWriter(cls *RClass) {
	parquetSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		parquetArity(args, 1, "new")
		schema := parquetSchemaArg(args[0])
		path, havePath, opts := parquetWriteArgs(args[1:])
		buf := &bytes.Buffer{}
		w, err := libparquet.NewArrowFileWriter(buf, schema, opts...)
		raiseParquetErr(err)
		return &ParquetWriter{w: w, buf: buf, path: path, havePath: havePath}
	})

	self := func(v object.Value) *ParquetWriter { return v.(*ParquetWriter) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	write := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		parquetArity(args, 1, "write")
		w := self(v)
		if w.closed {
			raise("Parquet::Error", "cannot write to a closed ArrowFileWriter")
		}
		raiseParquetErr(w.w.Write(parquetTableArg(args[0])))
		return v
	}
	d("write", write)
	d("write_table", write)
	d("<<", write)
	d("close", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		w := self(v)
		if !w.closed {
			w.closed = true
			raiseParquetErr(w.w.Close())
			if w.havePath {
				if err := os.WriteFile(w.path, w.buf.Bytes(), 0o644); err != nil {
					raise("Parquet::Error::Io", "write %s: %s", w.path, err.Error())
				}
			}
		}
		if w.havePath {
			return object.NilV
		}
		return object.NewStringBytesEnc(w.buf.Bytes(), "ASCII-8BIT")
	})
	toS := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ToS())
	}
	d("to_s", toS)
	d("inspect", toS)
}
