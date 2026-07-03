// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"sort"

	sequel "github.com/go-ruby-sequel/sequel"
	sqlite3 "github.com/go-ruby-sqlite3/sqlite3"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// deterministic SQL-generation core of github.com/go-ruby-sequel/sequel. The
// Dataset query builder, expression DSL, schema DSL and per-dialect
// literalization live in that library; rbgo supplies the *executor seam* — the
// thing that actually runs the generated SQL — and maps Ruby values to and from
// the library's small Value model.
//
// The executor seam is wired to go-ruby-sqlite3 (already bound into rbgo as a
// real, functional database), so `DB[:t].all` truly runs the generated SELECT
// against SQLite and returns live rows; `create_table` / `insert` / `update` /
// `delete` execute their DDL/DML for real. A Database created without a driver
// (Sequel.mock) generates SQL and logs DDL but runs nothing.

// SequelDBObj is the Ruby wrapper around a *sequel.Database (Sequel::Database).
// DB[:table] returns a SequelDatasetObj; create_table / run drive the executor.
type SequelDBObj struct {
	cls *RClass
	db  *sequel.Database
	// sqlite is the backing SQLite3::Database wrapper when the executor is wired
	// to go-ruby-sqlite3, kept reachable and returned by #_sqlite3.
	sqlite object.Value
}

func (d *SequelDBObj) ToS() string     { return "#<Sequel::Database>" }
func (d *SequelDBObj) Inspect() string { return "#<Sequel::Database>" }
func (d *SequelDBObj) Truthy() bool    { return true }

// SequelDatasetObj is the Ruby wrapper around a *sequel.Dataset (Sequel::Dataset).
// The chainable methods (where/select/order/join/…) return new wrappers; the
// terminal methods (sql/all/first/insert/update/delete) build or run SQL.
type SequelDatasetObj struct {
	cls    *RClass
	db     *SequelDBObj
	ds     *sequel.Dataset
	frozen bool // documentary: datasets are immutable, each op clones
}

func (d *SequelDatasetObj) ToS() string     { return "#<Sequel::Dataset: " + d.ds.SQL() + ">" }
func (d *SequelDatasetObj) Inspect() string { return d.ToS() }
func (d *SequelDatasetObj) Truthy() bool    { return true }

// sqliteExecutor adapts a bound *SQLite3Database to the sequel.Executor seam: it
// runs generated SQL through the go-ruby-sqlite3 ExecuteHash so each row comes
// back as a column->value map in the shape sequel expects. This is what makes
// Sequel execution real against SQLite.
type sqliteExecutor struct {
	db *sqlite3.Database
}

// Execute runs sql (with no binds — sequel literalizes values inline) and
// returns its rows as column->value maps. An empty result set (INSERT/UPDATE/DDL)
// yields no rows.
func (e *sqliteExecutor) Execute(sql string) ([]map[string]sequel.Value, error) {
	rows, err := e.db.ExecuteHash(sql, nil)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]sequel.Value, len(rows))
	for i, r := range rows {
		m := make(map[string]sequel.Value, len(r))
		for k, v := range r {
			m[k] = v
		}
		out[i] = m
	}
	return out, nil
}

// --- Ruby value -> sequel.Value --------------------------------------------

// sequelValue maps a Ruby value to the library's Value model so it literalizes
// as the right SQL: nil -> NULL, Integer/Float/Bool/String map straight, a
// Symbol becomes a column Identifier, an Array becomes an IN-list, a Hash becomes
// an ordered AND-of-equalities condition (the where(a: 1, b: 2) form).
func sequelValue(v object.Value) sequel.Value {
	switch n := v.(type) {
	case nil, object.Nil:
		return nil
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int64(n)
	case *object.Bignum:
		return n.I
	case object.Float:
		return float64(n)
	case *object.String:
		if n.IsBinary() {
			return sequel.Blob(n.Bytes())
		}
		return n.Str()
	case object.Symbol:
		return sequel.Ident(string(n))
	case *object.Array:
		vals := make([]sequel.Value, len(n.Elems))
		for i, e := range n.Elems {
			vals[i] = sequelValue(e)
		}
		return vals
	case *object.Hash:
		return sequelHashCond(n)
	}
	return v.ToS()
}

// sequelHashCond turns a Ruby Hash into the library's ordered hash condition
// (sequel.H), preserving key order — the Go form of where(col: val, …).
func sequelHashCond(h *object.Hash) sequel.Expr {
	kv := make([]sequel.Value, 0, h.Len()*2)
	for i := 0; i < h.Len(); i++ {
		k := h.Keys[i]
		val, _ := h.Get(k)
		kv = append(kv, sequelColumn(k), sequelValue(val))
	}
	return sequel.H(kv...)
}

// sequelColumn maps a Ruby value used as a column reference: a Symbol or String
// becomes an Identifier, anything else its literal value.
func sequelColumn(v object.Value) sequel.Value {
	switch n := v.(type) {
	case object.Symbol:
		return sequel.Ident(string(n))
	case *object.String:
		return sequel.Ident(n.Str())
	}
	return sequelValue(v)
}

// sequelColumns maps a slice of Ruby column references (select/order/group args).
func sequelColumns(vals []object.Value) []sequel.Value {
	out := make([]sequel.Value, len(vals))
	for i, v := range vals {
		out[i] = sequelColumn(v)
	}
	return out
}

// --- sequel.Value -> Ruby value --------------------------------------------

// sequelRubyValue maps a value scanned from the executor (the small Value model)
// back into the object graph. The go-ruby-sqlite3 executor produces the same Go
// types the sqlite3 binding maps, so this mirrors sqlite3Value.
func sequelRubyValue(v sequel.Value) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(n)
	case int64:
		return object.IntValue(n)
	case int:
		return object.IntValue(int64(n))
	case float64:
		return object.Float(n)
	case string:
		return object.NewString(n)
	case []byte:
		return object.NewStringBytesEnc(n, "ASCII-8BIT")
	}
	// The executor only ever produces the cases above.
	return object.NilV
}

// sequelRow maps an executor row (column->value map) to a Ruby Hash keyed by a
// Symbol column name, matching Sequel's default symbol-keyed rows. The executor
// hands back a Go map (unordered), so the keys are sorted for a deterministic
// row shape.
func sequelRow(row map[string]sequel.Value) *object.Hash {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := object.NewHash()
	for _, k := range keys {
		h.Set(object.Symbol(k), sequelRubyValue(row[k]))
	}
	return h
}

// sequelRows maps a slice of executor rows to a Ruby Array of Hashes.
func sequelRows(rows []map[string]sequel.Value) *object.Array {
	arr := object.NewArrayFromSlice(make([]object.Value, len(rows)))
	for i, r := range rows {
		arr.Elems[i] = sequelRow(r)
	}
	return arr
}

// raiseSequelError re-raises an executor error as a Sequel::DatabaseError.
func raiseSequelError(err error) {
	raise("Sequel::DatabaseError", "%s", err.Error())
}

// sequelCountValue extracts the scalar a count(*) query returned: the sole value
// of the sole row. A mock (executor-less) database returns no rows, so the count
// is 0; a real count(*) always returns one single-column row.
func sequelCountValue(rows []map[string]sequel.Value) object.Value {
	for _, row := range rows {
		for _, val := range row {
			return sequelRubyValue(val)
		}
	}
	return object.IntValue(0)
}
