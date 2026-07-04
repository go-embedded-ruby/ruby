// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"os"

	libarrow "github.com/go-ruby-arrow/arrow"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-arrow/arrow — the pure-Go (CGO=0),
// red-arrow-faithful port of Apache Arrow's columnar in-memory format and its
// IPC (Feather / stream) serialization — into rbgo as the native `Arrow` module
// (require "arrow"). The library owns the columnar format (on top of the official
// apache/arrow-go), the typed builders, the schema/record-batch/table machinery
// and the wire-compatible IPC round-trip; this file is only the thin shell that
// maps Ruby values onto the library's element model (see arrow_bind.go) and
// exposes the class/method surface red-arrow provides:
//
//	Arrow::Array / Arrow::ArrayBuilder   — typed, nullable columns
//	Arrow::DataType / Arrow::Field / Arrow::Schema — the type/shape surface
//	Arrow::RecordBatch / Arrow::Table    — equal-length column sets + IPC
//	Arrow::Error (< StandardError) / Arrow::Error::Io — the exception tree
//
// Arrow IPC buffers are little-endian on the wire on every architecture; on the
// big-endian target (s390x) apache/arrow-go performs the byte swap, so the same
// bytes round-trip identically across all six supported 64-bit arches.

// ArrowDataType is the Ruby wrapper around a go-ruby-arrow DataType.
type ArrowDataType struct{ dt *libarrow.DataType }

func (d *ArrowDataType) ToS() string     { return d.dt.String() }
func (d *ArrowDataType) Inspect() string { return d.dt.String() }
func (d *ArrowDataType) Truthy() bool    { return true }

// ArrowField is the Ruby wrapper around a go-ruby-arrow Field.
type ArrowField struct{ f *libarrow.Field }

func (f *ArrowField) ToS() string     { return f.f.String() }
func (f *ArrowField) Inspect() string { return f.f.String() }
func (f *ArrowField) Truthy() bool    { return true }

// ArrowSchema is the Ruby wrapper around a go-ruby-arrow Schema.
type ArrowSchema struct{ s *libarrow.Schema }

func (s *ArrowSchema) ToS() string     { return s.s.String() }
func (s *ArrowSchema) Inspect() string { return s.s.String() }
func (s *ArrowSchema) Truthy() bool    { return true }

// ArrowArray is the Ruby wrapper around a go-ruby-arrow Array (a typed column).
type ArrowArray struct{ a *libarrow.Array }

func (a *ArrowArray) ToS() string     { return a.a.String() }
func (a *ArrowArray) Inspect() string { return a.a.String() }
func (a *ArrowArray) Truthy() bool    { return true }

// ArrowArrayBuilder is the Ruby wrapper around a go-ruby-arrow ArrayBuilder.
type ArrowArrayBuilder struct{ b *libarrow.ArrayBuilder }

func (b *ArrowArrayBuilder) ToS() string     { return "#<Arrow::ArrayBuilder>" }
func (b *ArrowArrayBuilder) Inspect() string { return "#<Arrow::ArrayBuilder>" }
func (b *ArrowArrayBuilder) Truthy() bool    { return true }

// ArrowRecordBatch is the Ruby wrapper around a go-ruby-arrow RecordBatch.
type ArrowRecordBatch struct{ r *libarrow.RecordBatch }

func (r *ArrowRecordBatch) ToS() string     { return r.r.String() }
func (r *ArrowRecordBatch) Inspect() string { return r.r.String() }
func (r *ArrowRecordBatch) Truthy() bool    { return true }

// ArrowTable is the Ruby wrapper around a go-ruby-arrow Table.
type ArrowTable struct{ t *libarrow.Table }

func (t *ArrowTable) ToS() string     { return t.t.String() }
func (t *ArrowTable) Inspect() string { return t.t.String() }
func (t *ArrowTable) Truthy() bool    { return true }

// registerArrow installs the Arrow module and its classes (require "arrow"). It
// runs eagerly at boot; the error tree needs StandardError in place.
func (vm *VM) registerArrow() {
	mod := newClass("Arrow", nil)
	mod.isModule = true
	vm.consts["Arrow"] = mod

	vm.registerArrowErrors(mod)

	mk := func(name string, super *RClass) *RClass {
		full := "Arrow::" + name
		cls := newClass(full, super)
		mod.consts[name] = cls
		vm.consts[full] = cls
		return cls
	}

	vm.registerArrowDataType(mk("DataType", vm.cObject))
	vm.registerArrowField(mk("Field", vm.cObject))
	vm.registerArrowSchema(mk("Schema", vm.cObject))
	vm.registerArrowArray(mk("Array", vm.cObject))
	vm.registerArrowArrayBuilder(mk("ArrayBuilder", vm.cObject))
	vm.registerArrowRecordBatch(mk("RecordBatch", vm.cObject))
	vm.registerArrowTable(mk("Table", vm.cObject))
}

// registerArrowErrors installs the Arrow::Error exception tree: Arrow::Error <
// StandardError and Arrow::Error::Io < Arrow::Error. The library tags its IO
// failures with a kind whose RubyClass() is "Arrow::Error::Io", so a re-raised
// library error rescues as the matching class; its other kinds map to the
// pre-existing TypeError / IndexError / ArgumentError / NotImplementedError.
func (vm *VM) registerArrowErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)

	errCls := newClass("Arrow::Error", std)
	mod.consts["Error"] = errCls
	vm.consts["Arrow::Error"] = errCls

	ioCls := newClass("Arrow::Error::Io", errCls)
	errCls.consts["Io"] = ioCls
	vm.consts["Arrow::Error::Io"] = ioCls
}

// smethod installs a class ("singleton") method on a class.
func arrowSMethod(cls *RClass, name string, fn NativeFn) {
	cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
}

// registerArrowDataType installs Arrow::DataType: a class method per primitive
// type (Arrow::DataType.int64, .string, .boolean, …), the parameterised
// constructors (.decimal128 / .list / .struct), .resolve, and the instance
// name/to_s/== surface.
func (vm *VM) registerArrowDataType(cls *RClass) {
	for name, ctor := range arrowTypeSpecs {
		ctor := ctor
		arrowSMethod(cls, name, func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &ArrowDataType{dt: ctor()}
		})
	}
	arrowSMethod(cls, "resolve", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "resolve")
		return &ArrowDataType{dt: arrowTypeFromSpec(args[0])}
	})
	arrowSMethod(cls, "decimal128", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 2, "decimal128")
		p := int32(arrowInt(args[0]))
		s := int32(arrowInt(args[1]))
		return &ArrowDataType{dt: libarrow.Decimal128(p, s)}
	})
	arrowSMethod(cls, "list", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "list")
		return &ArrowDataType{dt: libarrow.ListOf(arrowTypeFromSpec(args[0]))}
	})
	arrowSMethod(cls, "struct", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		fields := make([]*libarrow.Field, len(args))
		for i, a := range args {
			fields[i] = arrowFieldArg(a).f
		}
		return &ArrowDataType{dt: libarrow.StructOf(fields...)}
	})

	self := func(v object.Value) *ArrowDataType { return v.(*ArrowDataType) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).dt.Name())
	})
	toS := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).dt.String())
	}
	d("to_s", toS)
	d("inspect", toS)
	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(*ArrowDataType)
		if !ok {
			return object.False
		}
		return object.Bool(self(v).dt.EqualQ(o.dt))
	})
}

// registerArrowField installs Arrow::Field: Field.new(name, type, nullable=true)
// and the name/data_type/nullable? instance surface.
func (vm *VM) registerArrowField(cls *RClass) {
	arrowSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
		}
		name := arrowKeyName(args[0])
		dt := arrowTypeFromSpec(args[1])
		nullable := true
		if len(args) >= 3 {
			nullable = args[2].Truthy()
		}
		if nullable {
			return &ArrowField{f: libarrow.NewField(name, dt)}
		}
		return &ArrowField{f: libarrow.NewFieldNonNull(name, dt)}
	})

	self := func(v object.Value) *ArrowField { return v.(*ArrowField) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).f.Name())
	})
	d("data_type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ArrowDataType{dt: self(v).f.DataType()}
	})
	d("nullable?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).f.NullableQ())
	})
	toS := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).f.String())
	}
	d("to_s", toS)
	d("inspect", toS)
}

// registerArrowSchema installs Arrow::Schema: Schema.new accepts either an Array
// of Arrow::Field or a Hash {name => type}, plus the n_fields/fields/[] surface.
func (vm *VM) registerArrowSchema(cls *RClass) {
	arrowSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "new")
		return &ArrowSchema{s: libarrow.NewSchema(arrowFieldsFromValue(args[0])...)}
	})

	self := func(v object.Value) *ArrowSchema { return v.(*ArrowSchema) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	nFields := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).s.NumFields()))
	}
	d("n_fields", nFields)
	d("num_fields", nFields)
	d("fields", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		fs := self(v).s.Fields()
		out := make([]object.Value, len(fs))
		for i, f := range fs {
			out[i] = &ArrowField{f: f}
		}
		return object.NewArrayFromSlice(out)
	})
	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "[]")
		s := self(v).s
		switch k := args[0].(type) {
		case object.Integer:
			i := int(k)
			if i < 0 || i >= s.NumFields() {
				raise("IndexError", "field index %d out of range (%d fields)", i, s.NumFields())
			}
			return &ArrowField{f: s.Field(i)}
		default:
			f, ok := s.FieldByName(arrowKeyName(args[0]))
			if !ok {
				return object.NilV
			}
			return &ArrowField{f: f}
		}
	})
	toS := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).s.String())
	}
	d("to_s", toS)
	d("inspect", toS)
}

// registerArrowArray installs Arrow::Array: Array.new(values, type = nil) (the
// type is inferred when omitted) plus the element-access/enumeration surface.
func (vm *VM) registerArrowArray(cls *RClass) {
	arrowSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		vals := arrowValuesFromArray(args[0])
		if len(args) >= 2 {
			return arrowArrayOK(libarrow.NewArrayOf(arrowTypeFromSpec(args[1]), vals))
		}
		return arrowArrayOK(libarrow.NewArray(vals))
	})

	self := func(v object.Value) *ArrowArray { return v.(*ArrowArray) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "[]")
		val, err := self(v).a.Get(int(arrowInt(args[0])))
		raiseArrowErr(err)
		return arrowScalarToRuby(val)
	})
	d("to_a", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return arrowArrayToA(self(v).a)
	})
	length := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).a.Length()))
	}
	d("length", length)
	d("size", length)
	d("n_rows", length)
	d("n_nulls", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).a.NNulls()))
	})
	d("null?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "null?")
		return object.Bool(self(v).a.NullQ(int(arrowInt(args[0]))))
	})
	d("valid?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "valid?")
		return object.Bool(self(v).a.ValidQ(int(arrowInt(args[0]))))
	})
	d("value_data_type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ArrowDataType{dt: self(v).a.DataType()}
	})
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		_ = self(v).a.Each(func(_ int, val any) error {
			vm.callBlock(blk, []object.Value{arrowScalarToRuby(val)})
			return nil
		})
		return v
	})
	toS := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).a.String())
	}
	d("to_s", toS)
	d("inspect", toS)
}

// registerArrowArrayBuilder installs Arrow::ArrayBuilder: ArrayBuilder.new(type)
// and the append/finish surface that freezes a typed Arrow::Array.
func (vm *VM) registerArrowArrayBuilder(cls *RClass) {
	arrowSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "new")
		return &ArrowArrayBuilder{b: libarrow.NewArrayBuilder(arrowTypeFromSpec(args[0]))}
	})

	self := func(v object.Value) *ArrowArrayBuilder { return v.(*ArrowArrayBuilder) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	appendVal := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "append")
		raiseArrowErr(self(v).b.Append(arrowValueToScalar(args[0])))
		return v
	}
	d("append", appendVal)
	d("<<", appendVal)
	d("append_null", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).b.AppendNull()
		return v
	})
	d("append_values", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "append_values")
		raiseArrowErr(self(v).b.AppendValues(arrowValuesFromArray(args[0])))
		return v
	})
	length := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).b.Length()))
	}
	d("length", length)
	d("size", length)
	d("value_data_type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ArrowDataType{dt: self(v).b.DataType()}
	})
	d("finish", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ArrowArray{a: self(v).b.Finish()}
	})
}

// registerArrowRecordBatch installs Arrow::RecordBatch: RecordBatch.new accepts
// either a Hash of columns {name => values} (types inferred) or an explicit
// (schema, [Array…]) pair, plus the row/column-access and each_record surface.
func (vm *VM) registerArrowRecordBatch(cls *RClass) {
	arrowSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		schema, cols := arrowBuildColumns(args)
		rb, err := libarrow.NewRecordBatch(schema, cols)
		raiseArrowErr(err)
		return &ArrowRecordBatch{r: rb}
	})

	self := func(v object.Value) *ArrowRecordBatch { return v.(*ArrowRecordBatch) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("schema", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ArrowSchema{s: self(v).r.Schema()}
	})
	nRows := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).r.NumRows())
	}
	d("n_rows", nRows)
	d("num_rows", nRows)
	nCols := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).r.NumColumns())
	}
	d("n_columns", nCols)
	d("num_columns", nCols)
	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "[]")
		a, err := self(v).r.Get(arrowColumnKey(args[0]))
		raiseArrowErr(err)
		return &ArrowArray{a: a}
	})
	d("column", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "column")
		a, err := self(v).r.Column(int(arrowInt(args[0])))
		raiseArrowErr(err)
		return &ArrowArray{a: a}
	})
	d("to_h", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		r := self(v).r
		return arrowColumnsToHash(r.Schema(), func(i int) *libarrow.Array {
			a, _ := r.Column(i)
			return a
		})
	})
	d("slice", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 2, "slice")
		sl, err := self(v).r.Slice(arrowInt(args[0]), arrowInt(args[1]))
		raiseArrowErr(err)
		return &ArrowRecordBatch{r: sl}
	})
	d("each_record", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each_record)")
		}
		_ = self(v).r.EachRecord(func(_ int, values map[string]any) error {
			vm.callBlock(blk, []object.Value{arrowScalarToRuby(values)})
			return nil
		})
		return v
	})
	toS := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).r.String())
	}
	d("to_s", toS)
	d("inspect", toS)
}

// registerArrowTable installs Arrow::Table: Table.new (Hash of columns or a
// (schema, [Array…]) pair), Table.load (a file path or in-memory IPC bytes), the
// row/column-access and each_record surface, and Table#save (to a path, or — with
// no path — the encoded IPC bytes) so a table round-trips through Arrow IPC.
func (vm *VM) registerArrowTable(cls *RClass) {
	arrowSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		schema, cols := arrowBuildColumns(args)
		return arrowTableOK(libarrow.NewTable(schema, cols))
	})
	arrowSMethod(cls, "load", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "load")
		s, ok := args[0].(*object.String)
		if !ok {
			raise("TypeError", "Arrow::Table.load expects a String path or IPC bytes")
		}
		if arrowFileExists(s.Str()) {
			return arrowTableOK(libarrow.LoadTable(s.Str()))
		}
		return arrowTableOK(libarrow.DecodeTable(s.Bytes()))
	})

	self := func(v object.Value) *ArrowTable { return v.(*ArrowTable) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("schema", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ArrowSchema{s: self(v).t.Schema()}
	})
	nRows := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).t.NumRows())
	}
	d("n_rows", nRows)
	d("num_rows", nRows)
	nCols := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).t.NumColumns())
	}
	d("n_columns", nCols)
	d("num_columns", nCols)
	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 1, "[]")
		a, err := self(v).t.Column(arrowColumnKey(args[0]))
		raiseArrowErr(err)
		return &ArrowArray{a: a}
	})
	d("to_h", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		t := self(v).t
		return arrowColumnsToHash(t.Schema(), func(i int) *libarrow.Array {
			a, _ := t.Column(i)
			return a
		})
	})
	d("slice", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		arrowArity(args, 2, "slice")
		sl, err := self(v).t.Slice(arrowInt(args[0]), arrowInt(args[1]))
		raiseArrowErr(err)
		return &ArrowTable{t: sl}
	})
	d("record_batch", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ArrowRecordBatch{r: self(v).t.RecordBatch()}
	})
	d("each_record", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each_record)")
		}
		_ = self(v).t.EachRecord(func(_ int, values map[string]any) error {
			vm.callBlock(blk, []object.Value{arrowScalarToRuby(values)})
			return nil
		})
		return v
	})
	d("save", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return arrowSaveTable(self(v).t, args)
	})
	toS := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).t.String())
	}
	d("to_s", toS)
	d("inspect", toS)
}

// --- shared helpers --------------------------------------------------------

// arrowArity raises a Ruby ArgumentError when fewer than want arguments were
// given, matching MRI's arity error for the native method.
func arrowArity(args []object.Value, want int, name string) {
	if len(args) < want {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d) for %s",
			len(args), want, name)
	}
}

// arrowInt coerces a Ruby Integer argument to an int64, raising TypeError for a
// non-Integer (an Arrow index/offset must be an Integer).
func arrowInt(v object.Value) int64 {
	n, ok := object.BigOf(v)
	if !ok {
		raise("TypeError", "expected an Integer, got %s", v.Inspect())
	}
	return n.Int64()
}

// arrowColumnKey coerces a Ruby column key (Integer, String or Symbol) into the
// library's `any` key model.
func arrowColumnKey(v object.Value) any {
	switch k := v.(type) {
	case object.Integer:
		return int(k)
	case *object.String:
		return k.Str()
	case object.Symbol:
		return string(k)
	}
	raise("TypeError", "column key must be an Integer or String, got %s", v.Inspect())
	panic("unreachable")
}

// arrowFieldArg asserts an argument is an Arrow::Field, raising TypeError otherwise.
func arrowFieldArg(v object.Value) *ArrowField {
	f, ok := v.(*ArrowField)
	if !ok {
		raise("TypeError", "expected an Arrow::Field, got %s", v.Inspect())
	}
	return f
}

// arrowFieldsFromValue maps a Schema.new argument into a []*Field. It accepts an
// Array of Arrow::Field, or a Hash {name => type-spec}.
func arrowFieldsFromValue(v object.Value) []*libarrow.Field {
	switch spec := v.(type) {
	case *object.Array:
		out := make([]*libarrow.Field, len(spec.Elems))
		for i, e := range spec.Elems {
			out[i] = arrowFieldArg(e).f
		}
		return out
	case *object.Hash:
		out := make([]*libarrow.Field, 0, spec.Len())
		for _, k := range spec.Keys {
			val, _ := spec.Get(k)
			out = append(out, libarrow.NewField(arrowKeyName(k), arrowTypeFromSpec(val)))
		}
		return out
	}
	raise("TypeError", "expected an Array of Arrow::Field or a Hash of name => type")
	panic("unreachable")
}

// arrowArrayToA materialises a library Array into a Ruby Array of Ruby scalars.
func arrowArrayToA(a *libarrow.Array) object.Value {
	slice := a.ToSlice()
	out := make([]object.Value, len(slice))
	for i, el := range slice {
		out[i] = arrowScalarToRuby(el)
	}
	return object.NewArrayFromSlice(out)
}

// arrowColumnsToHash renders a schema-ordered column set as a Ruby Hash mapping
// each column name (a String) to a Ruby Array of its values, so #to_h keeps the
// schema's field order regardless of Go map iteration order.
func arrowColumnsToHash(schema *libarrow.Schema, col func(i int) *libarrow.Array) object.Value {
	h := object.NewHash()
	for i := 0; i < schema.NumFields(); i++ {
		h.Set(object.NewString(schema.Field(i).Name()), arrowArrayToA(col(i)))
	}
	return h
}

// arrowBuildColumns resolves the shared RecordBatch.new / Table.new argument
// forms into a (schema, columns) pair: a single Hash {name => values} (types
// inferred per column) or an explicit (Arrow::Schema, [Arrow::Array…]) pair.
func arrowBuildColumns(args []object.Value) (*libarrow.Schema, []*libarrow.Array) {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	switch first := args[0].(type) {
	case *object.Hash:
		fields := make([]*libarrow.Field, 0, first.Len())
		cols := make([]*libarrow.Array, 0, first.Len())
		for _, k := range first.Keys {
			val, _ := first.Get(k)
			arr, err := libarrow.NewArray(arrowValuesFromArray(val))
			raiseArrowErr(err)
			fields = append(fields, libarrow.NewField(arrowKeyName(k), arr.DataType()))
			cols = append(cols, arr)
		}
		return libarrow.NewSchema(fields...), cols
	case *ArrowSchema:
		arrowArity(args, 2, "new")
		return first.s, arrowArraysFromValue(args[1])
	}
	raise("TypeError", "expected a Hash of columns or an (Arrow::Schema, Array) pair")
	panic("unreachable")
}

// arrowArraysFromValue maps a Ruby Array of Arrow::Array into a []*Array.
func arrowArraysFromValue(v object.Value) []*libarrow.Array {
	arr, ok := v.(*object.Array)
	if !ok {
		raise("TypeError", "expected an Array of Arrow::Array columns")
	}
	out := make([]*libarrow.Array, len(arr.Elems))
	for i, e := range arr.Elems {
		aw, ok := e.(*ArrowArray)
		if !ok {
			raise("TypeError", "expected an Arrow::Array column, got %s", e.Inspect())
		}
		out[i] = aw.a
	}
	return out
}

// arrowSaveTable implements Table#save. With a String path it writes the table to
// disk in the chosen IPC format and returns nil; with no path it returns the
// encoded IPC bytes as a binary (ASCII-8BIT) String. The format defaults to the
// Arrow streaming format and is selected by a trailing Symbol argument or a
// trailing Hash {format: sym}.
func arrowSaveTable(t *libarrow.Table, args []object.Value) object.Value {
	path := ""
	havePath := false
	format := libarrow.FormatStream
	for _, a := range args {
		switch x := a.(type) {
		case object.Nil:
			// an explicit nil path selects the bytes-returning form
		case *object.String:
			path, havePath = x.Str(), true
		case object.Symbol:
			format = arrowFormat(string(x))
		case *object.Hash:
			if fv, ok := x.Get(object.Symbol("format")); ok {
				format = arrowFormat(arrowKeyName(fv))
			}
		default:
			raise("ArgumentError", "unexpected save argument %s", a.Inspect())
		}
	}
	if havePath {
		raiseArrowErr(t.Save(path, format))
		return object.NilV
	}
	var buf bytes.Buffer
	switch format {
	case libarrow.FormatFile:
		_ = libarrow.WriteTableFile(&buf, t)
	default:
		_ = libarrow.WriteTableStream(&buf, t)
	}
	return object.NewStringBytesEnc(buf.Bytes(), "ASCII-8BIT")
}

// arrowFormat maps a Ruby format Symbol/name onto the library's IPC [Format].
// The streaming aliases select the Arrow stream format; the file aliases select
// the Arrow file (Feather v2) format. An unknown name raises ArgumentError.
func arrowFormat(name string) libarrow.Format {
	switch name {
	case "arrow_streaming", "stream", "streaming":
		return libarrow.FormatStream
	case "arrow", "file", "feather", "arrow_file":
		return libarrow.FormatFile
	}
	raise("ArgumentError", "unknown Arrow IPC format %q", name)
	panic("unreachable")
}

// arrowFileExists reports whether path names an existing regular file, so
// Table.load can distinguish a filesystem path from in-memory IPC bytes.
func arrowFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
