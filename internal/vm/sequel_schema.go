// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	sequel "github.com/go-ruby-sequel/sequel"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds the sequel schema DSL (the create_table block builder) and the
// small set of query helpers the dataset methods need. The library owns the
// column-type mapping and DDL generation; this shell maps the Ruby DSL calls
// (primary_key/String/Integer/column/…) onto the library's TableBuilder.

// SequelSchemaObj is the Ruby wrapper yielded to a create_table block — the Go
// form of Sequel's Schema::CreateTableGenerator. Its DSL methods build columns on
// the underlying *sequel.TableBuilder.
type SequelSchemaObj struct {
	cls *RClass
	tb  *sequel.TableBuilder
}

func (s *SequelSchemaObj) ToS() string     { return "#<Sequel::Schema::Generator>" }
func (s *SequelSchemaObj) Inspect() string { return s.ToS() }
func (s *SequelSchemaObj) Truthy() bool    { return true }

// sequelSchemaClass returns (memoised) the Sequel::Schema::Generator class,
// defining its column DSL on first use.
func (vm *VM) sequelSchemaClass() *RClass {
	if c, ok := vm.consts["Sequel::Schema::Generator"].(*RClass); ok {
		return c
	}
	cls := newClass("Sequel::Schema::Generator", vm.cObject)
	vm.consts["Sequel::Schema::Generator"] = cls
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *sequel.TableBuilder { return v.(*SequelSchemaObj).tb }

	// primary_key :id defines an auto-increment primary key column.
	d("primary_key", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).PrimaryKey(sequelName(pgArg0(args)))
		return object.NilV
	})

	// The typed column builders (String/Integer/Float/Bignum/Numeric/Bool/Date/
	// DateTime/Time) each add a column, honouring null:/default:/unique: options
	// from a trailing keyword Hash.
	typed := func(add func(*sequel.TableBuilder, string, ...sequel.ColOpt) *sequel.TableBuilder) NativeFn {
		return func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			name, opts := sequelColArgs(args)
			add(self(v), name, opts...)
			return object.NilV
		}
	}
	d("String", typed((*sequel.TableBuilder).String))
	d("Integer", typed((*sequel.TableBuilder).Integer))
	d("Bignum", typed((*sequel.TableBuilder).Bignum))
	d("Float", typed((*sequel.TableBuilder).Float))
	d("Numeric", typed((*sequel.TableBuilder).Numeric))
	d("TrueClass", typed((*sequel.TableBuilder).Bool)) // Sequel spells Bool as TrueClass
	d("Bool", typed((*sequel.TableBuilder).Bool))
	d("Date", typed((*sequel.TableBuilder).Date))
	d("DateTime", typed((*sequel.TableBuilder).DateTime))
	d("Time", typed((*sequel.TableBuilder).Time))

	// column(:name, :type, opts) is the generic form; the type maps to the typed
	// builder above, defaulting to String for an unknown type name.
	d("column", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..)", len(args))
		}
		name := sequelName(args[0])
		typeName := sequelName(args[1])
		_, opts := sequelColArgs(args[2:])
		tb := self(v)
		switch typeName {
		case "Integer", "integer", "int":
			tb.Integer(name, opts...)
		case "Float", "float":
			tb.Float(name, opts...)
		case "Bignum", "bignum":
			tb.Bignum(name, opts...)
		case "Numeric", "numeric":
			tb.Numeric(name, opts...)
		case "TrueClass", "Bool", "boolean", "bool":
			tb.Bool(name, opts...)
		case "Date", "date":
			tb.Date(name, opts...)
		case "DateTime", "datetime":
			tb.DateTime(name, opts...)
		case "Time", "time":
			tb.Time(name, opts...)
		default:
			tb.String(name, opts...)
		}
		return object.NilV
	})

	// foreign_key(:name, :table, opts) adds a foreign-key column.
	d("foreign_key", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..)", len(args))
		}
		_, opts := sequelColArgs(args[2:])
		self(v).ForeignKey(sequelName(args[0]), sequelName(args[1]), opts...)
		return object.NilV
	})

	// index([:cols], unique: true) adds an index.
	d("index", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		cols := sequelIndexCols(args[0])
		var opts []sequel.IdxOpt
		if len(args) > 1 {
			if h, ok := args[len(args)-1].(*object.Hash); ok {
				if v, ok := sequelKw(h, "unique"); ok && v.Truthy() {
					opts = append(opts, sequel.UniqueIndex())
				}
			}
		}
		self(v).Index(cols, opts...)
		return object.NilV
	})

	return cls
}

// sequelName renders a Symbol/String argument to its bare name.
func sequelName(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return v.ToS()
}

// sequelColArgs splits a column DSL argument list into the column name and its
// ColOpts read from a trailing keyword Hash (null:/default:/unique:/size:).
func sequelColArgs(args []object.Value) (string, []sequel.ColOpt) {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
	}
	name := sequelName(args[0])
	var opts []sequel.ColOpt
	if len(args) > 1 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			opts = sequelColOpts(h)
		}
	}
	return name, opts
}

// sequelColOpts reads a column option Hash into the library's ColOpts.
func sequelColOpts(h *object.Hash) []sequel.ColOpt {
	var opts []sequel.ColOpt
	if v, ok := sequelKw(h, "null"); ok && !v.Truthy() {
		opts = append(opts, sequel.NotNull())
	}
	if v, ok := sequelKw(h, "unique"); ok && v.Truthy() {
		opts = append(opts, sequel.Unique())
	}
	if v, ok := sequelKw(h, "size"); ok {
		opts = append(opts, sequel.Size(int(pgIntArg(v))))
	}
	if v, ok := sequelKw(h, "text"); ok && v.Truthy() {
		opts = append(opts, sequel.Text())
	}
	if v, ok := sequelKw(h, "default"); ok {
		opts = append(opts, sequel.DefaultVal(sequelValue(v)))
	}
	return opts
}

// sequelIndexCols reads the index column list: a single Symbol/String, or an
// Array of them.
func sequelIndexCols(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		cols := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			cols[i] = sequelName(e)
		}
		return cols
	}
	return []string{sequelName(v)}
}

// --- dataset query helpers -------------------------------------------------

// sequelCond reads a where/having condition argument: a single Hash becomes an
// ordered hash condition, a single String a literal SQL fragment, a single
// value its literal; multiple args are not a standard Sequel form and take the
// first.
func sequelCond(args []object.Value) sequel.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	switch n := args[0].(type) {
	case *object.Hash:
		return sequelHashCond(n)
	case *object.String:
		return sequel.Lit(n.Str())
	}
	return sequelValue(args[0])
}

// sequelJoinArgs reads a join argument list: the table (Symbol/String) and its
// ON condition (a Hash mapping columns, or a literal).
func sequelJoinArgs(args []object.Value) (sequel.Value, sequel.Value) {
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
	}
	return sequelColumn(args[0]), sequelCond(args[1:])
}

// sequelKVArgs reads insert/update value arguments: a single Hash is flattened
// into the library's alternating key/value list (InsertSQL/UpdateSQL take
// column, value, column, value, …); an empty list inserts defaults.
func sequelKVArgs(args []object.Value) []sequel.Value {
	if len(args) == 0 {
		return nil
	}
	if h, ok := args[0].(*object.Hash); ok {
		kv := make([]sequel.Value, 0, h.Len()*2)
		for i := 0; i < h.Len(); i++ {
			k := h.Keys[i]
			val, _ := h.Get(k)
			// InsertSQL/UpdateSQL expect the column position to be a bare string
			// name (they quote it), not an Identifier.
			kv = append(kv, sequelName(k), sequelValue(val))
		}
		return kv
	}
	return sequelValues(args)
}
