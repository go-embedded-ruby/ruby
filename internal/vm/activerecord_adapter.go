// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !(js && wasm)

package vm

import (
	activerecord "github.com/go-ruby-activerecord/activerecord"
	sqlite3 "github.com/go-ruby-sqlite3/sqlite3"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// arSQLiteAdapter implements activerecord.Adapter over a live go-ruby-sqlite3
// database — the host seam that turns the deterministic SQL the activerecord core
// renders into real rows. It is the "adapter executor" wired to go-ruby-sqlite3
// so a Relation's #to_a / #count / #exists? / #pluck actually run.
type arSQLiteAdapter struct {
	db *sqlite3.Database
}

// Execute runs a row-returning statement (a SELECT / existence probe) and yields
// the rows as ActiveRecord Rows keyed by column name.
func (a *arSQLiteAdapter) Execute(sql string) ([]activerecord.Row, error) {
	rows, err := a.db.ExecuteHash(sql, nil)
	if err != nil {
		return nil, err
	}
	out := make([]activerecord.Row, len(rows))
	for i, r := range rows {
		out[i] = activerecord.Row(r)
	}
	return out, nil
}

// ExecuteDML runs an INSERT/UPDATE/DELETE and reports the affected-row count and
// last insert id the driver provides.
func (a *arSQLiteAdapter) ExecuteDML(sql string) (affected int64, lastInsertID int64, err error) {
	if err := a.db.ExecuteBatch(sql, nil); err != nil {
		return 0, 0, err
	}
	affected, _ = a.db.Changes()
	lastInsertID, _ = a.db.LastInsertRowID()
	return affected, lastInsertID, nil
}

// AdapterName reports the ActiveRecord adapter name, so the core picks the
// SQLite Dialect.
func (a *arSQLiteAdapter) AdapterName() string { return "sqlite3" }

// arConnect opens (or replaces) the process ActiveRecord connection at path,
// raising ActiveRecord::StatementInvalid on a failure to open.
func (vm *VM) arConnect(path string) {
	db, err := sqlite3.New(path)
	if err != nil {
		raise("ActiveRecord::StatementInvalid", "%s", err.Error())
	}
	vm.arAdapter = &arSQLiteAdapter{db: db}
}

// arRequireAdapter returns the process adapter or raises
// ActiveRecord::ConnectionNotEstablished when no connection has been opened (the
// documented deferred case: SQL is always available via #to_sql, execution needs
// a connection).
func (vm *VM) arRequireAdapter() *arSQLiteAdapter {
	if vm.arAdapter == nil {
		raise("ActiveRecord::ConnectionNotEstablished", "No connection pool for ActiveRecord::Base; call establish_connection first")
	}
	return vm.arAdapter
}

// arRawConnection returns the underlying SQLite3::Database wrapper for the
// process adapter, so ActiveRecord::Base.connection exposes raw #execute. It is
// the sqlite3-typed tail of the `connection` class method, split out so the
// method itself stays in the (build-tag-free) activerecord.go while the
// SQLite3::Database dependency stays behind the native build tag; the js/wasm
// build provides a stub that raises instead (see bindings_wasm.go). The nil
// check lives in the caller, so this is only reached with a live adapter.
func (vm *VM) arRawConnection() object.Value {
	return &SQLite3Database{db: vm.arAdapter.db}
}
