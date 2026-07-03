// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

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

// activeRecordConnPath reads the establish_connection argument: a Hash yields its
// :database / :adapter (":memory:" default), a String is the path directly.
func activeRecordConnPath(args []object.Value) string {
	if len(args) == 0 {
		return ":memory:"
	}
	{
		__sw3 := args[0]
		switch {
		case object.IsKind[*object.Hash](__sw3):
			v := object.Kind[*object.Hash](__sw3)
			_ = v
			if db, ok := v.Get(object.SymVal(string(object.Symbol("database")))); ok {
				return arStr(db)
			}
			if db, ok := v.Get(object.Wrap(object.NewString("database"))); ok {
				return arStr(db)
			}
			return ":memory:"
		default:
			v := __sw3
			_ = v
			return arStr(args[0])
		}
	}
}

// arValueToRuby maps a value scanned from the adapter (int64 / float64 / string /
// []byte / bool / nil) back into the rbgo object graph, mirroring the sqlite3
// binding's own scan mapping.
func arValueToRuby(v any) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilVal()
	case int64:
		return object.IntValue(n)
	case int:
		return object.IntValue(int64(n))
	case float64:
		return object.FloatValue(float64(object.Float(n)))
	case string:
		return object.Wrap(object.NewString(n))
	case []byte:
		return object.Wrap(object.NewStringBytesEnc(n, "ASCII-8BIT"))
	case bool:
		return object.BoolValue(bool(object.Bool(n)))
	}
	return object.NilVal()
}
