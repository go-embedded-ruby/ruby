// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build wasm

// This file is the GOOS=js GOARCH=wasm counterpart of the database / search / KV
// bindings whose Go drivers do not compile for wasm — SQLite3 (modernc.org/libc),
// Sequel and ActiveRecord (executor wired to SQLite3), Bolt and Bleve
// (go.etcd.io/bbolt), and Etcd (go.etcd.io/etcd + coreos/go-systemd). Their real
// implementations live in files carried by the //go:build !(js && wasm) tag, so
// this file supplies the symbols the shared code still references on wasm.
//
// The chosen degradation strategy is a graceful module, consistently across every
// guarded gem: `require "sqlite3"` (etc.) still succeeds (the feature name is in
// require.go's providedFeatures), the module and its exception class are defined
// so conditional code and `rescue`s load, and only an actual operation
// (SQLite3::Database.new, Sequel.connect, Bolt::DB.open, Bleve.new, Etcd.new,
// Confd.render, ActiveRecord::Base.establish_connection, …) raises a clean
// NotImplementedError "<gem> is not supported on wasm" — the same error kind
// the existing spawn/xstr wasm stubs raise. No native behaviour changes: these
// definitions exist only on wasm.

package vm

import (
	"strings"

	activerecord "github.com/go-ruby-activerecord/activerecord"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// classOfExtBinding is the wasm half of the classOf seam: the guarded value types
// can never be constructed on wasm (their constructors are the stubs below), so
// no value ever needs mapping here.
func (vm *VM) classOfExtBinding(v object.Value) (*RClass, bool) { _, _ = vm, v; return nil, false }

// valueEqualExtBinding is the wasm half of the value-equality seam: the guarded
// value types (Arrow::DataType, …) can never be constructed on wasm, so no value
// ever needs its own equality here.
func valueEqualExtBinding(a, b object.Value) (bool, bool) { _, _ = a, b; return false, false }

// hasCustomEqExtBinding is the wasm half of the custom-== seam: the guarded value
// types (BSON::ObjectId, …) can never be constructed on wasm.
func hasCustomEqExtBinding(v object.Value) bool { _ = v; return false }

// wasmUnsupported returns a native method that raises the standard
// "<gem> is not supported on wasm" NotImplementedError.
func wasmUnsupported(gem string) NativeFn {
	return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return raise("NotImplementedError", "%s is not supported on wasm", gem)
	}
}

// simpleName returns the last "::"-segment of a qualified constant name.
func simpleName(qualified string) string {
	if i := strings.LastIndex(qualified, ":"); i >= 0 {
		return qualified[i+1:]
	}
	return qualified
}

// stubBindingModule installs a top-level module `name` plus its exception class
// `errQualified` (< StandardError), registered both as a nested constant and
// under its qualified name, mirroring the native register's error tree so a
// `rescue Gem::Error` still resolves.
func (vm *VM) stubBindingModule(name, errQualified string) *RClass {
	mod := newClass(name, nil)
	mod.isModule = true
	vm.consts[name] = mod
	std := vm.consts["StandardError"].(*RClass)
	ec := newClass(errQualified, std)
	mod.consts[simpleName(errQualified)] = ec
	vm.consts[errQualified] = ec
	return mod
}

// stubBindingClass installs a nested class `qualified` under mod (as vm.cObject
// subclass), registered both nested and top-level, like the native trees.
func (vm *VM) stubBindingClass(mod *RClass, qualified string) *RClass {
	c := newClass(qualified, vm.cObject)
	mod.consts[simpleName(qualified)] = c
	vm.consts[qualified] = c
	return c
}

// stubSMethods attaches raising singleton methods (class/module methods) named by
// names to c.
func stubSMethods(c *RClass, gem string, names ...string) {
	for _, n := range names {
		c.smethods[n] = &Method{name: n, owner: c, native: wasmUnsupported(gem)}
	}
}

// registerSQLite3 installs a graceful SQLite3 module: SQLite3::Exception and a
// SQLite3::Database whose .new/.open raise on wasm.
func (vm *VM) registerSQLite3() {
	mod := vm.stubBindingModule("SQLite3", "SQLite3::Exception")
	db := vm.stubBindingClass(mod, "SQLite3::Database")
	stubSMethods(db, "sqlite3", "new", "open")
}

// registerSequel installs a graceful Sequel module: Sequel::Error and the
// Sequel.sqlite / .connect / .mock factory methods, all raising on wasm.
func (vm *VM) registerSequel() {
	mod := vm.stubBindingModule("Sequel", "Sequel::Error")
	stubSMethods(mod, "sequel", "sqlite", "connect", "mock")
}

// registerBolt installs a graceful Bolt module: Bolt::Error and a Bolt::DB whose
// .open/.new raise on wasm.
func (vm *VM) registerBolt() {
	mod := vm.stubBindingModule("Bolt", "Bolt::Error")
	db := vm.stubBindingClass(mod, "Bolt::DB")
	stubSMethods(db, "bolt", "open", "new")
}

// registerBleve installs a graceful Bleve module: Bleve::Error and the
// Bleve.new / .open / .new_mem_index factory methods, all raising on wasm.
func (vm *VM) registerBleve() {
	mod := vm.stubBindingModule("Bleve", "Bleve::Error")
	stubSMethods(mod, "bleve", "new", "open", "new_mem_index")
}

// registerEtcd installs graceful Etcd and Etcdv3 modules (Etcd::Error) whose
// .new/.connect raise on wasm.
func (vm *VM) registerEtcd() {
	mod := vm.stubBindingModule("Etcd", "Etcd::Error")
	stubSMethods(mod, "etcd", "new", "connect")
	alias := newClass("Etcdv3", nil)
	alias.isModule = true
	vm.consts["Etcdv3"] = alias
	stubSMethods(alias, "etcd", "new", "connect")
}

// registerConfd installs a graceful Confd module: Confd::Error and Confd.render,
// which raises on wasm.
func (vm *VM) registerConfd() {
	mod := vm.stubBindingModule("Confd", "Confd::Error")
	stubSMethods(mod, "confd", "render")
}

// arSQLiteAdapter is the wasm stub of the ActiveRecord adapter. ActiveRecord
// itself builds for wasm; only its executor is wired to SQLite3, so the adapter
// seam is stubbed here. The type still satisfies activerecord.Adapter (so the
// shared ActiveRecord code that passes it to the core compiles) and every method
// raises — though none is reached, because arConnect/arRequireAdapter raise
// first.
type arSQLiteAdapter struct{}

func (a *arSQLiteAdapter) Execute(sql string) ([]activerecord.Row, error) {
	raise("NotImplementedError", "active_record (sqlite3) is not supported on wasm")
	return nil, nil
}

func (a *arSQLiteAdapter) ExecuteDML(sql string) (affected int64, lastInsertID int64, err error) {
	raise("NotImplementedError", "active_record (sqlite3) is not supported on wasm")
	return 0, 0, nil
}

func (a *arSQLiteAdapter) AdapterName() string { return "sqlite3" }

// arConnect raises on wasm: there is no SQLite3 driver to open a connection.
func (vm *VM) arConnect(path string) {
	_ = path
	raise("NotImplementedError", "active_record (sqlite3) is not supported on wasm")
}

// arRequireAdapter raises on wasm (a connection can never be established), so any
// query path degrades to a clean error rather than a nil dereference.
func (vm *VM) arRequireAdapter() *arSQLiteAdapter {
	raise("NotImplementedError", "active_record (sqlite3) is not supported on wasm")
	return nil
}

// arRawConnection is the wasm stub of ActiveRecord::Base.connection's SQLite3
// tail; it raises, matching the establish_connection path.
func (vm *VM) arRawConnection() object.Value {
	return raise("NotImplementedError", "active_record (sqlite3) is not supported on wasm")
}
