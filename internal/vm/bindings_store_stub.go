// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core && !((darwin && (amd64 || arm64)) || (freebsd && (386 || amd64 || arm || arm64)) || (linux && (386 || amd64 || arm || arm64 || loong64 || ppc64le || riscv64 || s390x)) || (netbsd && amd64) || (openbsd && (amd64 || arm64)) || windows)

// This file is the counterpart of the store bindings on every target their
// drivers do not build for (see bindings_store.go for the list and the reason):
// js/wasm and wasip1, linux/mips*, linux/ppc64, AIX, Solaris, illumos,
// DragonFly, and NetBSD and OpenBSD outside amd64/arm64. The bindings are
// SQLite3 (modernc.org/sqlite), Sequel and ActiveRecord (executor wired to
// SQLite3), Bolt and Bleve (go.etcd.io/bbolt) and Etcd (go.etcd.io/etcd +
// coreos/go-systemd). Their real implementations live in files carrying the
// complementary constraint, so this file supplies the symbols the shared code
// still references.
//
// The chosen degradation strategy is a graceful module, consistently across every
// guarded gem: `require "sqlite3"` (etc.) still succeeds (the feature name is in
// require.go's providedFeatures), the module and its exception class are defined
// so conditional code and `rescue`s load, and only an actual operation
// (SQLite3::Database.new, Sequel.connect, Bolt::DB.open, Bleve.new, Etcd.new,
// Confd.render, ActiveRecord::Base.establish_connection, …) raises a clean
// NotImplementedError "<gem> is not supported on <target>" — the same error kind
// the spawn/xstr wasm stubs raise. No behaviour changes where the drivers build:
// these definitions exist only where they do not.

package vm

import (
	"runtime"
	"strings"

	activerecord "github.com/go-ruby-activerecord/activerecord"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// classOfStoreBinding is the stub half of the store bindings' classOf seam: their
// value types can never be constructed here.
func (vm *VM) classOfStoreBinding(v object.Value) (*RClass, bool) { _, _ = vm, v; return nil, false }

// unsupportedTarget names this target the way the NotImplementedError messages
// do: "wasm" for both wasm ports (the wording the browser build has always
// used), GOOS/GOARCH otherwise.
func unsupportedTarget() string {
	if runtime.GOARCH == "wasm" {
		return "wasm"
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}

// storeUnsupported returns a native method that raises the standard
// "<gem> is not supported on <target>" NotImplementedError.
func storeUnsupported(gem string) NativeFn {
	return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return raise("NotImplementedError", "%s is not supported on %s", gem, unsupportedTarget())
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
		c.smethods[n] = &Method{name: n, owner: c, native: storeUnsupported(gem)}
	}
}

// registerSQLite3 installs a graceful SQLite3 module: SQLite3::Exception and a
// SQLite3::Database whose .new/.open raise.
func (vm *VM) registerSQLite3() {
	mod := vm.stubBindingModule("SQLite3", "SQLite3::Exception")
	db := vm.stubBindingClass(mod, "SQLite3::Database")
	stubSMethods(db, "sqlite3", "new", "open")
}

// registerSequel installs a graceful Sequel module: Sequel::Error and the
// Sequel.sqlite / .connect / .mock factory methods, all raising.
func (vm *VM) registerSequel() {
	mod := vm.stubBindingModule("Sequel", "Sequel::Error")
	stubSMethods(mod, "sequel", "sqlite", "connect", "mock")
}

// registerBolt installs a graceful Bolt module: Bolt::Error and a Bolt::DB whose
// .open/.new raise.
func (vm *VM) registerBolt() {
	mod := vm.stubBindingModule("Bolt", "Bolt::Error")
	db := vm.stubBindingClass(mod, "Bolt::DB")
	stubSMethods(db, "bolt", "open", "new")
}

// registerBleve installs a graceful Bleve module: Bleve::Error and the
// Bleve.new / .open / .new_mem_index factory methods, all raising.
func (vm *VM) registerBleve() {
	mod := vm.stubBindingModule("Bleve", "Bleve::Error")
	stubSMethods(mod, "bleve", "new", "open", "new_mem_index")
}

// registerEtcd installs graceful Etcd and Etcdv3 modules (Etcd::Error) whose
// .new/.connect raise.
func (vm *VM) registerEtcd() {
	mod := vm.stubBindingModule("Etcd", "Etcd::Error")
	stubSMethods(mod, "etcd", "new", "connect")
	alias := newClass("Etcdv3", nil)
	alias.isModule = true
	vm.consts["Etcdv3"] = alias
	stubSMethods(alias, "etcd", "new", "connect")
}

// registerConfd installs a graceful Confd module: Confd::Error and Confd.render,
// which raises.
func (vm *VM) registerConfd() {
	mod := vm.stubBindingModule("Confd", "Confd::Error")
	stubSMethods(mod, "confd", "render")
}

// arSQLiteAdapter is the stub of the ActiveRecord adapter. ActiveRecord itself
// builds everywhere; only its executor is wired to SQLite3, so the adapter seam
// is stubbed here. The type still satisfies activerecord.Adapter (so the shared
// ActiveRecord code that passes it to the core compiles) and every method
// raises — though none is reached, because arConnect/arRequireAdapter raise
// first.
type arSQLiteAdapter struct{}

func (a *arSQLiteAdapter) Execute(sql string) ([]activerecord.Row, error) {
	raiseARUnsupported()
	return nil, nil
}

func (a *arSQLiteAdapter) ExecuteDML(sql string) (affected int64, lastInsertID int64, err error) {
	raiseARUnsupported()
	return 0, 0, nil
}

func (a *arSQLiteAdapter) AdapterName() string { return "sqlite3" }

// raiseARUnsupported raises ActiveRecord's NotImplementedError for this target.
func raiseARUnsupported() object.Value {
	return raise("NotImplementedError", "active_record (sqlite3) is not supported on %s", unsupportedTarget())
}

// arConnect raises: there is no SQLite3 driver to open a connection.
func (vm *VM) arConnect(path string) {
	_ = path
	raiseARUnsupported()
}

// arRequireAdapter raises (a connection can never be established), so any query
// path degrades to a clean error rather than a nil dereference.
func (vm *VM) arRequireAdapter() *arSQLiteAdapter {
	raiseARUnsupported()
	return nil
}

// arRawConnection is the stub of ActiveRecord::Base.connection's SQLite3 tail;
// it raises, matching the establish_connection path.
func (vm *VM) arRawConnection() object.Value {
	return raiseARUnsupported()
}
