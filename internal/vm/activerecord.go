// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
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
	vm.consts["ActiveRecord"] = mod

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
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("ActiveRecord::ActiveRecordError", std)
	mod.consts["ActiveRecordError"] = base
	vm.consts["ActiveRecord::ActiveRecordError"] = base

	for _, name := range []string{"StatementInvalid", "RecordInvalid", "ConnectionNotEstablished"} {
		c := newClass("ActiveRecord::"+name, base)
		mod.consts[name] = c
		vm.consts["ActiveRecord::"+name] = c
	}
}

// registerActiveRecordBase installs ActiveRecord::Base and its
// establish_connection / connected? / connection class methods (the adapter seam).
func (vm *VM) registerActiveRecordBase(mod *RClass) {
	base := newClass("ActiveRecord::Base", vm.cObject)
	mod.consts["Base"] = base
	vm.consts["ActiveRecord::Base"] = base

	// establish_connection(database: ":memory:") / establish_connection(path)
	// opens a sqlite3 database and installs it as the process adapter.
	base.smethods["establish_connection"] = &Method{name: "establish_connection", owner: base, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		path := activeRecordConnPath(args)
		vm.arConnect(path)
		return object.NilV
	}}
	base.smethods["connected?"] = &Method{name: "connected?", owner: base, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.arAdapter != nil)
	}}
	// connection returns the underlying SQLite3::Database so raw #execute works.
	base.smethods["connection"] = &Method{name: "connection", owner: base, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.arAdapter == nil {
			raise("ActiveRecord::ConnectionNotEstablished", "No connection pool for ActiveRecord::Base")
		}
		return &SQLite3Database{db: vm.arAdapter.db}
	}}
}
