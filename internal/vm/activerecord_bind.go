// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	activerecord "github.com/go-ruby-activerecord/activerecord"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the ActiveRecord::Model DSL (columns / validations /
// associations), the chainable Relation surface + #to_sql and the adapter-backed
// execution methods, the Record attribute set, and the Errors shape, over the
// go-ruby-activerecord library. SQL rendering + validation are deterministic Go;
// execution runs through the go-ruby-sqlite3 adapter (activerecord_adapter.go).

// registerActiveRecordModel installs ActiveRecord::Model.new(name, table) { … }
// and the query methods that return relations plus #to_sql and the execution
// methods.
func (vm *VM) registerActiveRecordModel(mod *RClass) {
	cls := newClass("ActiveRecord::Model", vm.cObject)
	mod.consts["Model"] = cls
	vm.consts["ActiveRecord::Model"] = cls

	dsl := newClass("ActiveRecord::Model::DSL", vm.cObject)
	vm.consts["ActiveRecord::Model::DSL"] = dsl
	vm.registerActiveRecordModelDSL(dsl)

	// ActiveRecord::Model.new(name, table) { column …; validates … }.
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		m := activerecord.NewModel(arStr(args[0]), arStr(args[1]))
		wrapper := &ActiveRecordModel{m: m, cls: cls}
		if blk != nil {
			vm.callBlockSelf(blk, &ActiveRecordModelBuilder{m: m}, nil)
		}
		return wrapper
	}}

	self := func(v object.Value) *ActiveRecordModel { return v.(*ActiveRecordModel) }

	// Chainable entry points delegate to the model and wrap the relation.
	rel := func(m *ActiveRecordModel, r *activerecord.Relation) object.Value {
		return &ActiveRecordRelation{r: r, model: m}
	}
	cls.define("all", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		m := self(v)
		return rel(m, m.m.All())
	})
	cls.define("where", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		return rel(m, m.m.Where(arCondArgs(args)...))
	})
	cls.define("select", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		return rel(m, m.m.Select(arAnyArgs(args)...))
	})
	cls.define("order", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		return rel(m, m.m.Order(arAnyArgs(args)...))
	})
	cls.define("joins", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		return rel(m, m.m.Joins(arAnyArgs(args)...))
	})
	cls.define("table_name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).m.TableName)
	})
	// #build(attrs) makes a new validating Record without touching the database.
	cls.define("build", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		return &ActiveRecordRecord{rec: m.m.Build(arAttrs(args)), model: m}
	})
	cls.define("new", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		return &ActiveRecordRecord{rec: m.m.Build(arAttrs(args)), model: m}
	})
	// #insert_sql(attrs) renders the INSERT statement (byte-faithful).
	cls.define("insert_sql", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).m.InsertSQL(arAttrs(args)))
	})
	// #create(attrs) builds, validates, and (if valid) inserts a Record, running
	// the INSERT through the adapter. #create! raises RecordInvalid instead of
	// returning an unsaved invalid record.
	cls.define("create", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.arCreateRecord(self(v), args, false)
	})
	cls.define("create!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.arCreateRecord(self(v), args, true)
	})
}

// registerActiveRecordModelDSL installs the model-block receiver: #column and the
// validates_* / association declarations.
func (vm *VM) registerActiveRecordModelDSL(dsl *RClass) {
	self := func(v object.Value) *activerecord.Model { return v.(*ActiveRecordModelBuilder).m }

	dsl.define("column", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		typ := "string"
		if len(args) > 1 {
			typ = arStr(args[1])
		}
		self(v).AddColumn(arStr(args[0]), typ)
		return object.NilV
	})
	dsl.define("validates_presence_of", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		for _, a := range args {
			self(v).ValidatesPresence(arStr(a))
		}
		return object.NilV
	})
	dsl.define("validates_length_of", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		self(v).ValidatesLength(arStr(args[0]), arLengthOpts(args))
		return object.NilV
	})
	dsl.define("validates_inclusion_of", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		self(v).ValidatesInclusion(arStr(args[0]), arInList(args))
		return object.NilV
	})
	dsl.define("belongs_to", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		self(v).BelongsTo(arStr(args[0]), arClassName(args))
		return object.NilV
	})
	dsl.define("has_many", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		self(v).HasMany(arStr(args[0]), arClassName(args))
		return object.NilV
	})
}

// registerActiveRecordRelation installs the chainable Relation surface + #to_sql
// and the adapter-backed execution methods (#to_a / #count / #exists? / #pluck /
// #first).
func (vm *VM) registerActiveRecordRelation(mod *RClass) {
	cls := newClass("ActiveRecord::Relation", vm.cObject)
	mod.consts["Relation"] = cls
	vm.consts["ActiveRecord::Relation"] = cls

	self := func(v object.Value) *ActiveRecordRelation { return v.(*ActiveRecordRelation) }
	chain := func(v object.Value, r *activerecord.Relation) object.Value {
		return &ActiveRecordRelation{r: r, model: self(v).model}
	}

	cls.define("where", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).r.Where(arCondArgs(args)...))
	})
	cls.define("not", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).r.Not(arCondArgs(args)...))
	})
	cls.define("or", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return chain(v, self(v).r.Or(args[0].(*ActiveRecordRelation).r))
	})
	cls.define("select", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).r.Select(arAnyArgs(args)...))
	})
	cls.define("order", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).r.Order(arAnyArgs(args)...))
	})
	cls.define("group", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).r.Group(arAnyArgs(args)...))
	})
	cls.define("having", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).r.Having(arCondArgs(args)...))
	})
	cls.define("joins", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).r.Joins(arAnyArgs(args)...))
	})
	cls.define("limit", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).r.Limit(arInt(args)))
	})
	cls.define("offset", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).r.Offset(arInt(args)))
	})
	cls.define("distinct", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).r.Distinct())
	})

	cls.define("to_sql", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).r.ToSQL())
	})
	// #to_s falls through to Object#to_s -> the wrapper's Go ToS (the rendered
	// SQL), so it need not be redefined here.

	// --- adapter-backed execution ---
	cls.define("to_a", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		rel := self(v)
		recs, err := activerecord.LoadAll(vm.arRequireAdapter(), rel.r)
		if err != nil {
			raise("ActiveRecord::StatementInvalid", "%s", err.Error())
		}
		arr := &object.Array{Elems: make([]object.Value, len(recs))}
		for i, rec := range recs {
			arr.Elems[i] = &ActiveRecordRecord{rec: rec, model: rel.model}
		}
		return arr
	})
	cls.define("count", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		n, err := activerecord.Count(vm.arRequireAdapter(), self(v).r)
		if err != nil {
			raise("ActiveRecord::StatementInvalid", "%s", err.Error())
		}
		return object.Integer(n)
	})
	cls.define("exists?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		ok, err := activerecord.Exists(vm.arRequireAdapter(), self(v).r)
		if err != nil {
			raise("ActiveRecord::StatementInvalid", "%s", err.Error())
		}
		return object.Bool(ok)
	})
	cls.define("first", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		rel := self(v)
		recs, err := activerecord.LoadAll(vm.arRequireAdapter(), rel.r.Limit(1))
		if err != nil {
			raise("ActiveRecord::StatementInvalid", "%s", err.Error())
		}
		if len(recs) == 0 {
			return object.NilV
		}
		return &ActiveRecordRecord{rec: recs[0], model: rel.model}
	})
	cls.define("pluck", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		rel := self(v)
		cols := arAnyArgs(args)
		recs, err := activerecord.LoadAll(vm.arRequireAdapter(), rel.r.Select(cols...))
		if err != nil {
			raise("ActiveRecord::StatementInvalid", "%s", err.Error())
		}
		return arPluck(recs, args)
	})
}

// registerActiveRecordRecord installs the Record attribute set: #[] / #attributes
// / #valid? / #errors / #changed? and the InsertSQL-driven #save.
func (vm *VM) registerActiveRecordRecord(mod *RClass) {
	cls := newClass("ActiveRecord::Record", vm.cObject)
	mod.consts["Record"] = cls
	vm.consts["ActiveRecord::Record"] = cls

	self := func(v object.Value) *ActiveRecordRecord { return v.(*ActiveRecordRecord) }
	cls.define("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		val, _ := self(v).rec.Get(arStr(args[0]))
		return arValueToRuby(val)
	})
	cls.define("[]=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).rec.Set(arStr(args[0]), arToGo(args[1]))
		return args[1]
	})
	cls.define("attributes", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		for k, val := range self(v).rec.Attributes() {
			h.Set(object.NewString(k), arValueToRuby(val))
		}
		return h
	})
	cls.define("valid?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		r := self(v)
		return object.Bool(r.model.m.Validate(r.rec).Empty())
	})
	cls.define("errors", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		r := self(v)
		return &ActiveRecordErrors{e: r.model.m.Validate(r.rec)}
	})
	cls.define("changed?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.Changed())
	})
	// Dynamic attribute accessors (u.name / u.name = x): a record answers each of
	// its attributes as a reader (and its "name=" as a writer), so an AR-loaded
	// row renders through the same u.name calls a Rails view uses.
	cls.define("method_missing", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		r := self(v)
		name := arStr(args[0])
		if strings.HasSuffix(name, "=") {
			attr := name[:len(name)-1]
			if len(args) < 2 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
			}
			r.rec.Set(attr, arToGo(args[1]))
			return args[1]
		}
		if val, ok := r.rec.Get(name); ok {
			return arValueToRuby(val)
		}
		raise("NoMethodError", "undefined method '%s' for %s", name, r.ToS())
		return object.NilV
	})
	cls.define("respond_to_missing?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			return object.False
		}
		name := strings.TrimSuffix(arStr(args[0]), "=")
		_, ok := self(v).rec.Get(name)
		return object.Bool(ok)
	})
}

// registerActiveRecordErrorsClass installs the ActiveModel::Errors-shaped value
// object a validation returns.
func (vm *VM) registerActiveRecordErrorsClass(mod *RClass) {
	cls := newClass("ActiveRecord::Errors", vm.cObject)
	mod.consts["Errors"] = cls
	vm.consts["ActiveRecord::Errors"] = cls

	self := func(v object.Value) *activerecord.Errors { return v.(*ActiveRecordErrors).e }
	cls.define("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Empty())
	})
	cls.define("count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(self(v).Count()))
	})
	cls.define("full_messages", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return arStrings(self(v).FullMessages())
	})
	cls.define("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return arStrings(self(v).On(arStr(args[0])))
	})
	cls.define("messages", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		for attr, msgs := range self(v).Messages() {
			h.Set(object.Symbol(attr), arStrings(msgs))
		}
		return h
	})
}

// registerActiveRecordSchema installs ActiveRecord::Schema DDL string generators
// (the deterministic schema layer): add_column / add_index / add_foreign_key.
func (vm *VM) registerActiveRecordSchema(mod *RClass) {
	cls := newClass("ActiveRecord::Schema", nil)
	cls.isModule = true
	mod.consts["Schema"] = cls
	vm.consts["ActiveRecord::Schema"] = cls

	cls.smethods["add_column_sql"] = &Method{name: "add_column_sql", owner: cls, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 3)", len(args))
		}
		return object.NewString(activerecord.AddColumnSQL(activerecord.SQLite, arStr(args[0]), arStr(args[1]), arStr(args[2])))
	}}
	cls.smethods["add_index_sql"] = &Method{name: "add_index_sql", owner: cls, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..)", len(args))
		}
		unique := len(args) > 2 && args[2].Truthy()
		return object.NewString(activerecord.AddIndexSQL(activerecord.SQLite, arStr(args[0]), arStrList(args[1]), unique, ""))
	}}

	// Schema.define { create_table … } executes the DDL against the connection.
	vm.registerActiveRecordSchemaDSL(cls)
}
