// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	activerecord "github.com/go-ruby-activerecord/activerecord"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// ActiveRecordModel is the Ruby wrapper around a *activerecord.Model — a mapped
// model class (its table, columns, validations and associations). The
// query-building + schema-DDL + validations core lives in the
// github.com/go-ruby-activerecord/activerecord library, which renders SQL
// byte-faithful to ActiveRecord::Relation#to_sql. Actual database execution is a
// host seam wired here to go-ruby-sqlite3 (see activerecord_adapter.go), so a
// relation's #to_a / #count / #exists? / #pluck run against a real database once
// ActiveRecord::Base.establish_connection has opened one.
type ActiveRecordModel struct {
	m   *activerecord.Model
	cls *RClass
}

func (m *ActiveRecordModel) ToS() string     { return "#<ActiveRecord::Model " + m.m.Name + ">" }
func (m *ActiveRecordModel) Inspect() string { return m.ToS() }
func (m *ActiveRecordModel) Truthy() bool    { return true }

// ActiveRecordRelation is the Ruby wrapper around a *activerecord.Relation — a
// lazy, chainable query (every refining method returns a new relation). #to_sql
// renders it; the execution methods run it through the connected adapter.
type ActiveRecordRelation struct {
	r     *activerecord.Relation
	model *ActiveRecordModel
}

func (r *ActiveRecordRelation) ToS() string     { return r.r.ToSQL() }
func (r *ActiveRecordRelation) Inspect() string { return "#<ActiveRecord::Relation>" }
func (r *ActiveRecordRelation) Truthy() bool    { return true }

// ActiveRecordRecord is the Ruby wrapper around a *activerecord.Record — a single
// model instance's attribute set with dirty tracking and validations.
type ActiveRecordRecord struct {
	rec   *activerecord.Record
	model *ActiveRecordModel
}

func (r *ActiveRecordRecord) ToS() string     { return "#<ActiveRecord::Record>" }
func (r *ActiveRecordRecord) Inspect() string { return "#<ActiveRecord::Record>" }
func (r *ActiveRecordRecord) Truthy() bool    { return true }

// ActiveRecordErrors is the Ruby wrapper around a *activerecord.Errors — the
// ActiveModel::Errors shape a validation produces (#full_messages / #messages /
// #[] / #empty? / #count).
type ActiveRecordErrors struct {
	e *activerecord.Errors
}

func (e *ActiveRecordErrors) ToS() string     { return "#<ActiveRecord::Errors>" }
func (e *ActiveRecordErrors) Inspect() string { return e.ToS() }
func (e *ActiveRecordErrors) Truthy() bool    { return true }

// ActiveRecordModelBuilder is the DSL self a `ActiveRecord::Model.new(name, table)
// { … }` block runs against: #column declares columns and the validates_* /
// belongs_to / has_many methods declare validations and associations.
type ActiveRecordModelBuilder struct {
	m *activerecord.Model
}

func (b *ActiveRecordModelBuilder) ToS() string     { return "#<ActiveRecord::Model::DSL>" }
func (b *ActiveRecordModelBuilder) Inspect() string { return b.ToS() }
func (b *ActiveRecordModelBuilder) Truthy() bool    { return true }

// registerActiveRecord installs the ActiveRecord module and its Model / Relation
// / Record / Errors surface (require "active_record"): ActiveRecord::Base
// .establish_connection(database:) opens a sqlite3 connection (the adapter seam);
// ActiveRecord::Model.new(name, table) { column …; validates … } builds a model
// whose #where/#select/#order/#limit/#offset/#group/#having/#joins/#distinct/#not
// return chainable relations, #to_sql renders them, and #to_a/#count/#exists?/
// #pluck run through the adapter; ActiveRecord::Record carries a validating
// attribute set. The StatementInvalid / RecordInvalid error tree matches the gem.
func (vm *VM) registerActiveRecord() {
	mod := newClass("ActiveRecord", nil)
	mod.isModule = true
	vm.consts["ActiveRecord"] = object.Wrap(mod)

	vm.registerActiveRecordErrors(mod)
	vm.registerActiveRecordBase(mod)
	vm.registerActiveRecordModel(mod)
	vm.registerActiveRecordRelation(mod)
	vm.registerActiveRecordRecord(mod)
	vm.registerActiveRecordErrorsClass(mod)
	vm.registerActiveRecordSchema(mod)
}

// registerActiveRecordErrors installs the ActiveRecord error tree:
// ActiveRecord::ActiveRecordError < StandardError, StatementInvalid (a failed
// query) and RecordInvalid (a failed validation) under it, mirroring the gem.
func (vm *VM) registerActiveRecordErrors(mod *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	base := newClass("ActiveRecord::ActiveRecordError", std)
	mod.consts["ActiveRecordError"] = object.Wrap(base)
	vm.consts["ActiveRecord::ActiveRecordError"] = object.Wrap(base)

	for _, name := range []string{"StatementInvalid", "RecordInvalid", "ConnectionNotEstablished", "RecordNotFound"} {
		c := newClass("ActiveRecord::"+name, base)
		mod.consts[name] = object.Wrap(c)
		vm.consts["ActiveRecord::"+name] = object.Wrap(c)
	}
}

// registerActiveRecordBase installs ActiveRecord::Base and its
// establish_connection / connected? / connection class methods (the adapter seam).
func (vm *VM) registerActiveRecordBase(mod *RClass) {
	base := newClass("ActiveRecord::Base", vm.cObject)
	mod.consts["Base"] = object.Wrap(base)
	vm.consts["ActiveRecord::Base"] = object.Wrap(base)

	// establish_connection(database: ":memory:") / establish_connection(path)
	// opens a sqlite3 database and installs it as the process adapter.
	base.smethods["establish_connection"] = &Method{name: "establish_connection", owner: base, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		path := activeRecordConnPath(args)
		vm.arConnect(path)
		return object.NilVal()
	}}
	base.smethods["connected?"] = &Method{name: "connected?", owner: base, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(vm.arAdapter != nil)))
	}}
	// connection returns the underlying SQLite3::Database so raw #execute works.
	base.smethods["connection"] = &Method{name: "connection", owner: base, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.arAdapter == nil {
			raise("ActiveRecord::ConnectionNotEstablished", "No connection pool for ActiveRecord::Base")
		}
		return object.Wrap(&SQLite3Database{db: vm.arAdapter.db})
	}}

	vm.registerActiveRecordBaseModelMethods(base)
}

// registerActiveRecordBaseModelMethods installs the ORM class methods a
// `class User < ActiveRecord::Base` subclass inherits: table_name / table_name=
// and the query + persistence entry points (all / where / order / create /
// create! / count / first / find). Each resolves the receiver class to a lazily
// built model (its table inferred via activerecord.Tableize unless table_name=
// set one) and reuses the same chainable Relation surface the factory models do.
func (vm *VM) registerActiveRecordBaseModelMethods(base *RClass) {
	sm := func(name string, fn NativeFn) {
		base.smethods[name] = &Method{name: name, owner: base, native: fn}
	}
	rel := func(m *ActiveRecordModel, r *activerecord.Relation) object.Value {
		return object.Wrap(&ActiveRecordRelation{r: r, model: m})
	}

	sm("table_name", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(vm.arModelForClass(object.Kind[*RClass](self)).m.TableName))
	})
	sm("table_name=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		vm.arSetTableName(object.Kind[*RClass](self), arStr(args[0]))
		return args[0]
	})
	sm("all", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := vm.arModelForClass(object.Kind[*RClass](self))
		return rel(m, m.m.All())
	})
	sm("where", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := vm.arModelForClass(object.Kind[*RClass](self))
		return rel(m, m.m.Where(arCondArgs(args)...))
	})
	sm("order", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := vm.arModelForClass(object.Kind[*RClass](self))
		return rel(m, m.m.Order(arAnyArgs(args)...))
	})
	sm("count", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := vm.arModelForClass(object.Kind[*RClass](self))
		n, err := activerecord.Count(vm.arRequireAdapter(), m.m.All())
		if err != nil {
			raise("ActiveRecord::StatementInvalid", "%s", err.Error())
		}
		return object.IntValue(n)
	})
	sm("first", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := vm.arModelForClass(object.Kind[*RClass](self))
		recs, err := activerecord.LoadAll(vm.arRequireAdapter(), m.m.All().Limit(1))
		if err != nil {
			raise("ActiveRecord::StatementInvalid", "%s", err.Error())
		}
		if len(recs) == 0 {
			return object.NilVal()
		}
		return object.Wrap(&ActiveRecordRecord{rec: recs[0], model: m})
	})
	sm("find", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		m := vm.arModelForClass(object.Kind[*RClass](self))
		recs, err := activerecord.LoadAll(vm.arRequireAdapter(), m.m.Where(map[string]any{m.m.PrimaryKey: arToGo(args[0])}).Limit(1))
		if err != nil {
			raise("ActiveRecord::StatementInvalid", "%s", err.Error())
		}
		if len(recs) == 0 {
			raise("ActiveRecord::RecordNotFound", "Couldn't find %s with '%s'=%s", m.m.Name, m.m.PrimaryKey, args[0].Inspect())
		}
		return object.Wrap(&ActiveRecordRecord{rec: recs[0], model: m})
	})
	sm("create", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.arCreateRecord(vm.arModelForClass(object.Kind[*RClass](self)), args, false)
	})
	sm("create!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.arCreateRecord(vm.arModelForClass(object.Kind[*RClass](self)), args, true)
	})
}

// arModelForClass returns the cached model for an ActiveRecord::Base subclass,
// building it on first use. The table name is an explicit table_name= override
// or, absent one, inferred from the class name (activerecord.Tableize).
func (vm *VM) arModelForClass(c *RClass) *ActiveRecordModel {
	if vm.arModels == nil {
		vm.arModels = map[*RClass]*ActiveRecordModel{}
	}
	if m, ok := vm.arModels[c]; ok {
		return m
	}
	table := vm.arTableNames[c]
	if table == "" {
		table = activerecord.Tableize(c.name)
	}
	m := &ActiveRecordModel{m: activerecord.NewModel(c.name, table), cls: c}
	vm.arModels[c] = m
	return m
}

// arSetTableName records an explicit table_name override for a class and drops
// any cached model so the next access rebuilds against the new table.
func (vm *VM) arSetTableName(c *RClass, name string) {
	if vm.arTableNames == nil {
		vm.arTableNames = map[*RClass]string{}
	}
	vm.arTableNames[c] = name
	delete(vm.arModels, c)
}

// arCreateRecord builds a record from an attributes Hash, validates it, and
// (when valid) runs the INSERT through the adapter. On an invalid record create!
// raises ActiveRecord::RecordInvalid while create returns the unsaved record.
func (vm *VM) arCreateRecord(m *ActiveRecordModel, args []object.Value, bang bool) object.Value {
	rec := m.m.Build(arAttrs(args))
	if errs := m.m.Validate(rec); !errs.Empty() {
		if bang {
			raise("ActiveRecord::RecordInvalid", "Validation failed: %s", strings.Join(errs.FullMessages(), ", "))
		}
		return object.Wrap(&ActiveRecordRecord{rec: rec, model: m})
	}
	if _, _, err := vm.arRequireAdapter().ExecuteDML(m.m.InsertSQL(rec.Attributes())); err != nil {
		raise("ActiveRecord::StatementInvalid", "%s", err.Error())
	}
	return object.Wrap(&ActiveRecordRecord{rec: rec, model: m})
}
