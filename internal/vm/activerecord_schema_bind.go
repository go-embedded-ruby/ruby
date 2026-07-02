// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	activerecord "github.com/go-ruby-activerecord/activerecord"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the schema-authoring half of ActiveRecord over
// go-ruby-activerecord: ActiveRecord::Schema.define runs a DDL block whose
// create_table renders CREATE TABLE (+ CREATE INDEX) through the same connected
// sqlite3 adapter the model queries use, and a `class … < ActiveRecord::Base`
// subclass becomes a working model (table name inferred via
// activerecord.Tableize, or set with self.table_name =) whose class methods
// return the same chainable relations. Together they make the idiomatic
// Rails-style data route — Schema.define -> Model.create! -> where(...).to_a —
// actually run.

// ActiveRecordSchemaDSL is the block self of ActiveRecord::Schema.define: a bare
// create_table / add_index / execute call inside the block dispatches to it.
type ActiveRecordSchemaDSL struct{}

func (d *ActiveRecordSchemaDSL) ToS() string     { return "#<ActiveRecord::Schema::Definition>" }
func (d *ActiveRecordSchemaDSL) Inspect() string { return d.ToS() }
func (d *ActiveRecordSchemaDSL) Truthy() bool    { return true }

// arPendingIndex is one create_table `t.index` declaration, emitted as CREATE
// INDEX after the table exists.
type arPendingIndex struct {
	cols   []string
	unique bool
	name   string
}

// ActiveRecordTableDSL is the object yielded to a create_table block (`|t|`):
// its column-type methods accumulate a TableDef and its #index queues indexes.
type ActiveRecordTableDSL struct {
	td      *activerecord.TableDef
	indexes []arPendingIndex
}

func (t *ActiveRecordTableDSL) ToS() string     { return "#<ActiveRecord::Schema::TableDefinition>" }
func (t *ActiveRecordTableDSL) Inspect() string { return t.ToS() }
func (t *ActiveRecordTableDSL) Truthy() bool    { return true }

// registerActiveRecordSchemaDSL installs the Schema.define block receiver and the
// create_table TableDefinition receiver classes, and adds Schema.define itself.
func (vm *VM) registerActiveRecordSchemaDSL(schema *RClass) {
	// ActiveRecord::Schema.define { create_table … } runs the DDL block with a
	// definition self, so a bare create_table resolves to it.
	schema.smethods["define"] = &Method{name: "define", owner: schema, native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "ActiveRecord::Schema.define requires a block")
		}
		vm.callBlockSelf(blk, &ActiveRecordSchemaDSL{}, nil)
		return object.NilV
	}}

	def := newClass("ActiveRecord::Schema::Definition", vm.cObject)
	schema.consts["Definition"] = def
	vm.consts["ActiveRecord::Schema::Definition"] = def
	vm.registerActiveRecordSchemaDefinition(def)

	tbl := newClass("ActiveRecord::Schema::TableDefinition", vm.cObject)
	schema.consts["TableDefinition"] = tbl
	vm.consts["ActiveRecord::Schema::TableDefinition"] = tbl
	vm.registerActiveRecordTableDefinition(tbl)
}

// registerActiveRecordSchemaDefinition installs the Schema.define block methods:
// create_table (the DDL DSL), and the raw add_index / add_column / execute
// pass-throughs, all executed against the connected adapter.
func (vm *VM) registerActiveRecordSchemaDefinition(def *RClass) {
	def.define("create_table", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		name := arStr(args[0])
		td := activerecord.CreateTable(activerecord.SQLite, name)
		if len(args) > 1 {
			arApplyCreateTableOpts(td, args[1])
		}
		tdsl := &ActiveRecordTableDSL{td: td}
		if blk != nil {
			vm.callBlock(blk, []object.Value{tdsl})
		}
		a := vm.arRequireAdapter()
		if _, _, err := a.ExecuteDML(td.ToSQL()); err != nil {
			raise("ActiveRecord::StatementInvalid", "%s", err.Error())
		}
		for _, idx := range tdsl.indexes {
			sql := activerecord.AddIndexSQL(activerecord.SQLite, name, idx.cols, idx.unique, idx.name)
			if _, _, err := a.ExecuteDML(sql); err != nil {
				raise("ActiveRecord::StatementInvalid", "%s", err.Error())
			}
		}
		return object.NilV
	})
	def.define("add_index", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..)", len(args))
		}
		cols, unique, name := arIndexArgs(args[1], indexOpts(args, 2))
		sql := activerecord.AddIndexSQL(activerecord.SQLite, arStr(args[0]), cols, unique, name)
		if _, _, err := vm.arRequireAdapter().ExecuteDML(sql); err != nil {
			raise("ActiveRecord::StatementInvalid", "%s", err.Error())
		}
		return object.NilV
	})
	def.define("add_column", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 3..)", len(args))
		}
		sql := activerecord.AddColumnSQL(activerecord.SQLite, arStr(args[0]), arStr(args[1]), arStr(args[2]), arColOpts(indexOpts(args, 3))...)
		if _, _, err := vm.arRequireAdapter().ExecuteDML(sql); err != nil {
			raise("ActiveRecord::StatementInvalid", "%s", err.Error())
		}
		return object.NilV
	})
	def.define("execute", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if _, _, err := vm.arRequireAdapter().ExecuteDML(arStr(args[0])); err != nil {
			raise("ActiveRecord::StatementInvalid", "%s", err.Error())
		}
		return object.NilV
	})
}

// registerActiveRecordTableDefinition installs the create_table block column DSL
// on the yielded `t`: the type shortcuts (string/integer/float/boolean/text/
// datetime/timestamp/date/time/binary/decimal/bigint), timestamps, references /
// belongs_to, the generic column, index, and primary_key.
func (vm *VM) registerActiveRecordTableDefinition(tbl *RClass) {
	self := func(v object.Value) *ActiveRecordTableDSL { return v.(*ActiveRecordTableDSL) }

	// Each shortcut adds a column of its ActiveRecord type.
	for _, typ := range []string{
		"string", "integer", "float", "boolean", "text", "datetime",
		"timestamp", "date", "time", "binary", "decimal", "bigint",
	} {
		t := typ
		tbl.define(t, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
			}
			d := self(v)
			for _, a := range arColumnNames(args) {
				d.td.Column(a, t, arColOpts(colOptsHash(args))...)
			}
			return object.NilV
		})
	}
	tbl.define("column", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..)", len(args))
		}
		self(v).td.Column(arStr(args[0]), arStr(args[1]), arColOpts(colOptsHash(args))...)
		return object.NilV
	})
	tbl.define("timestamps", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).td.Timestamps()
		return object.NilV
	})
	references := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		d := self(v)
		for _, a := range arColumnNames(args) {
			d.td.References(a, arColOpts(colOptsHash(args))...)
		}
		return object.NilV
	}
	tbl.define("references", references)
	tbl.define("belongs_to", references)
	tbl.define("index", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		cols, unique, name := arIndexArgs(args[0], indexOpts(args, 1))
		d := self(v)
		d.indexes = append(d.indexes, arPendingIndex{cols: cols, unique: unique, name: name})
		return object.NilV
	})
	tbl.define("primary_key", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		self(v).td.PrimaryKey(arStr(args[0]))
		return object.NilV
	})
}

// arApplyCreateTableOpts reads the create_table options Hash: id: false disables
// the implicit primary key, and id:/primary_key: <name> renames it.
func arApplyCreateTableOpts(td *activerecord.TableDef, v object.Value) {
	h, ok := v.(*object.Hash)
	if !ok {
		return
	}
	for _, key := range []string{"id", "primary_key"} {
		val, ok := arHashGet(h, key)
		if !ok {
			continue
		}
		if b, isBool := val.(object.Bool); isBool {
			if !bool(b) {
				td.NoPrimaryKey()
			}
			continue
		}
		td.PrimaryKey(arStr(val))
	}
}

// arColumnNames reads the leading column-name arguments of a create_table column
// shortcut (t.string :a, :b), stopping at a trailing options Hash.
func arColumnNames(args []object.Value) []string {
	var out []string
	for _, a := range args {
		if _, isHash := a.(*object.Hash); isHash {
			break
		}
		out = append(out, arStr(a))
	}
	return out
}

// colOptsHash returns the trailing options Hash of a column declaration, or nil.
// Callers guarantee at least one argument (the column name), so the last element
// is always addressable.
func colOptsHash(args []object.Value) *object.Hash {
	if h, ok := args[len(args)-1].(*object.Hash); ok {
		return h
	}
	return nil
}

// arColOpts maps a column options Hash (null:/default:/limit:) to the
// activerecord ColOpt list the schema layer consumes.
func arColOpts(h *object.Hash) []activerecord.ColOpt {
	if h == nil {
		return nil
	}
	var opts []activerecord.ColOpt
	if val, ok := arHashGet(h, "null"); ok {
		if b, isBool := val.(object.Bool); isBool && !bool(b) {
			opts = append(opts, activerecord.NotNull())
		}
	}
	if val, ok := arHashGet(h, "default"); ok {
		opts = append(opts, activerecord.Default(arToGo(val)))
	}
	if val, ok := arHashGet(h, "limit"); ok {
		if n, isInt := val.(object.Integer); isInt {
			opts = append(opts, activerecord.Limit(int(n)))
		}
	}
	return opts
}

// indexOpts returns the options Hash at args[from] (an index/add_column trailing
// Hash), or nil.
func indexOpts(args []object.Value, from int) *object.Hash {
	if from >= len(args) {
		return nil
	}
	if h, ok := args[from].(*object.Hash); ok {
		return h
	}
	return nil
}

// arIndexArgs reads an index declaration: the column spec (a name, or an Array of
// names) plus the unique:/name: options.
func arIndexArgs(colSpec object.Value, h *object.Hash) (cols []string, unique bool, name string) {
	cols = arStrList(colSpec)
	if h == nil {
		return cols, false, ""
	}
	if val, ok := arHashGet(h, "unique"); ok {
		unique = val.Truthy()
	}
	if val, ok := arHashGet(h, "name"); ok {
		name = arStr(val)
	}
	return cols, unique, name
}

// arHashGet reads key from a Ruby options Hash accepting either a Symbol or a
// String key (Rails options are symbol-keyed; a string key is tolerated).
func arHashGet(h *object.Hash, key string) (object.Value, bool) {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}
