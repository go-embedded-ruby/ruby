// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	sqlite3 "github.com/go-ruby-sqlite3/sqlite3"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent SQLite3 API of
// github.com/go-ruby-sqlite3/sqlite3, which drives modernc.org/sqlite (pure-Go,
// no cgo). The database engine itself lives in that library; rbgo only maps Ruby
// String SQL and Array binds onto its calls and maps the scanned Go values
// (int64 / float64 / string / []byte / nil) back into the object graph, applying
// the gem's INTEGER->Integer / REAL->Float / TEXT->String / BLOB->ASCII-8BIT
// String / NULL->nil type map by construction.

// sqlite3Open opens (or creates) the database at path, raising a Ruby
// SQLite3::Exception on failure.
func sqlite3Open(path string) *SQLite3Database {
	db, err := sqlite3.Open(path)
	if err != nil {
		raiseSQLite3Error(err)
	}
	return &SQLite3Database{db: db}
}

// raiseSQLite3Error re-raises a library error as the gem-faithful Ruby exception.
// A *sqlite3.Error carries the SQLite3::Exception subclass its result code maps
// to (e.g. SQLite3::BusyException); any other error raises the base
// SQLite3::Exception. It never returns (raise panics).
func raiseSQLite3Error(err error) {
	var se *sqlite3.Error
	if errors.As(err, &se) {
		raise(string(se.Class), "%s", se.Message)
	}
	raise(string(sqlite3.ExcException), "%s", err.Error())
}

// --- argument coercion -----------------------------------------------------

// sqlite3StringArg coerces a path / SQL argument to its string: a String yields
// its contents, any other value its to_s.
func sqlite3StringArg(v object.Value) string {
	if s, ok := object.KindOK[*object.String](v); ok {
		return s.Str()
	}
	return v.ToS()
}

// sqlite3IntArg coerces an argument to an int64, raising a TypeError for a
// non-integer.
func sqlite3IntArg(v object.Value) int64 {
	{
		__sw166 := v
		switch {
		case object.IsInt(__sw166):
			n := object.AsInteger(__sw166)
			_ = n
			return int64(n)
		case object.IsKind[*object.Bignum](__sw166):
			n := object.Kind[*object.Bignum](__sw166)
			_ = n
			return n.I.Int64()
		}
	}
	raise("TypeError", "no implicit conversion to Integer")
	return 0
}

// sqlite3ExecArgs splits an #execute / #query argument list into the SQL string
// and its positional binds. The binds are a trailing Array, or the remaining
// scalar arguments (SQLite3::Database#execute accepts both forms).
func sqlite3ExecArgs(args []object.Value) (string, []sqlite3.Value) {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
	}
	sql := sqlite3StringArg(args[0])
	rest := args[1:]
	if len(rest) == 1 {
		if arr, ok := object.KindOK[*object.Array](rest[0]); ok {
			return sql, sqlite3Binds(arr.Elems)
		}
	}
	return sql, sqlite3Binds(rest)
}

// sqlite3Spread expands a single trailing Array argument into its elements so
// #bind_params(*values) accepts both an explicit list and a single Array.
func sqlite3Spread(args []object.Value) []object.Value {
	if len(args) == 1 {
		if arr, ok := object.KindOK[*object.Array](args[0]); ok {
			return arr.Elems
		}
	}
	return args
}

// sqlite3Mode reads an optional transaction mode argument (:deferred /
// :immediate / :exclusive), defaulting to SQLite's deferred mode.
func sqlite3Mode(args []object.Value) sqlite3.TransactionMode {
	if len(args) == 0 {
		return sqlite3.Deferred
	}
	switch sqlite3StringArg(args[0]) {
	case "immediate", "IMMEDIATE":
		return sqlite3.Immediate
	case "exclusive", "EXCLUSIVE":
		return sqlite3.Exclusive
	default:
		return sqlite3.Deferred
	}
}

// sqlite3BindKey coerces a #bind_param key: an Integer is a 1-based positional
// index, a String / Symbol a named parameter.
func sqlite3BindKey(v object.Value) any {
	{
		__sw167 := v
		switch {
		case object.IsInt(__sw167):
			n := object.AsInteger(__sw167)
			_ = n
			return int(n)
		case object.IsKind[object.Symbol](__sw167):
			n := object.Kind[object.Symbol](__sw167)
			_ = n
			return string(n)
		case object.IsKind[*object.String](__sw167):
			n := object.Kind[*object.String](__sw167)
			_ = n
			return n.Str()
		}
	}
	return v.ToS()
}

// --- Ruby value -> bind (for Pack) -----------------------------------------

// sqlite3Bind maps a Ruby bind value to the driver argument type, applying the
// reverse of the result type map: Integer -> int64, Float -> float64, a UTF-8
// String -> string (TEXT), an ASCII-8BIT String -> []byte (BLOB), nil -> NULL,
// true/false -> 1/0 (SQLite has no boolean). Any other value binds its to_s.
func sqlite3Bind(v object.Value) sqlite3.Value {
	{
		__sw168 := v
		switch {
		case __sw168 == nil || object.IsNilObj(__sw168):
			n := __sw168
			_ = n
			return nil
		case object.IsBool(__sw168):
			n := object.AsBoolV(__sw168)
			_ = n
			if bool(n) {
				return int64(1)
			}
			return int64(0)
		case object.IsInt(__sw168):
			n := object.AsInteger(__sw168)
			_ = n
			return int64(n)
		case object.IsKind[*object.Bignum](__sw168):
			n := object.Kind[*object.Bignum](__sw168)
			_ = n
			return n.I.Int64()
		case object.IsFloat(__sw168):
			n := object.AsFloatV(__sw168)
			_ = n
			return float64(n)
		case object.IsKind[*object.String](__sw168):
			n := object.Kind[*object.String](__sw168)
			_ = n
			if n.IsBinary() {
				return []byte(n.Bytes())
			}
			return n.Str()
		case object.IsKind[object.Symbol](__sw168):
			n := object.Kind[object.Symbol](__sw168)
			_ = n
			return string(n)
		}
	}
	return v.ToS()
}

// sqlite3Binds maps a slice of Ruby bind values to driver arguments.
func sqlite3Binds(vals []object.Value) []sqlite3.Value {
	out := make([]sqlite3.Value, len(vals))
	for i, v := range vals {
		out[i] = sqlite3Bind(v)
	}
	return out
}

// --- driver value -> Ruby value (for scan) ---------------------------------

// sqlite3Value maps a value scanned from the driver back into the object graph:
// int64 -> Integer, float64 -> Float, string -> String (TEXT), []byte ->
// ASCII-8BIT String (BLOB), nil -> nil, matching the gem's SQLite<->Ruby types.
func sqlite3Value(vm *VM, v sqlite3.Value) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case int64:
		return object.IntValue(n)
	case float64:
		return object.Float(n)
	case string:
		return object.NewString(n)
	case []byte:
		return object.NewStringBytesEnc(n, "ASCII-8BIT")
	case bool:
		return object.Bool(n)
	}
	// The driver only ever produces the cases above; anything else is nil.
	return object.NilV
}

// sqlite3Row maps a positional result row to a Ruby Array of values.
func sqlite3Row(vm *VM, row sqlite3.Row) *object.Array {
	arr := object.NewArrayFromSlice(make([]object.Value, len(row)))
	for i, v := range row {
		arr.Elems[i] = sqlite3Value(vm, v)
	}
	return arr
}

// sqlite3HashRow maps a positional row plus its column names to a Ruby Hash
// keyed by column name (the results_as_hash shape).
func sqlite3HashRow(vm *VM, row sqlite3.Row, cols []string) *object.Hash {
	h := object.NewHash()
	for i, c := range cols {
		if i < len(row) {
			h.Set(object.NewString(c), sqlite3Value(vm, row[i]))
		}
	}
	return h
}

// sqlite3Strings maps a []string (column names / types) to a Ruby Array of
// Strings.
func sqlite3Strings(ss []string) *object.Array {
	arr := object.NewArrayFromSlice(make([]object.Value, len(ss)))
	for i, s := range ss {
		arr.Elems[i] = object.NewString(s)
	}
	return arr
}

// --- execution helpers -----------------------------------------------------

// sqlite3Execute runs sql and returns its rows in the shape the owning database
// selects (Array rows, or Hash rows when results_as_hash is set). With a block
// it yields each row and returns nil.
func (vm *VM) sqlite3Execute(db *sqlite3.Database, sql string, binds []sqlite3.Value, blk *Proc) object.Value {
	if db.ResultsAsHash() {
		rows, cols, err := sqlite3ExecuteHash(db, sql, binds)
		if err != nil {
			raiseSQLite3Error(err)
		}
		if blk != nil {
			for _, r := range rows {
				vm.callBlock(blk, []object.Value{sqlite3RawHashRow(vm, r, cols)})
			}
			return object.NilV
		}
		out := object.NewArrayFromSlice(make([]object.Value, len(rows)))
		for i, r := range rows {
			out.Elems[i] = sqlite3RawHashRow(vm, r, cols)
		}
		return out
	}
	rows, err := db.Execute(sql, binds)
	if err != nil {
		raiseSQLite3Error(err)
	}
	if blk != nil {
		for _, r := range rows {
			vm.callBlock(blk, []object.Value{sqlite3Row(vm, r)})
		}
		return object.NilV
	}
	out := object.NewArrayFromSlice(make([]object.Value, len(rows)))
	for i, r := range rows {
		out.Elems[i] = sqlite3Row(vm, r)
	}
	return out
}

// sqlite3ExecuteHash runs sql through a prepared statement so both the hash rows
// and their column names are available (Database#execute_hash returns only the
// maps, but the binding needs the ordered column names to build a Ruby Hash).
func sqlite3ExecuteHash(db *sqlite3.Database, sql string, binds []sqlite3.Value) ([]sqlite3.Row, []string, error) {
	st, err := db.Prepare(sql)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = st.Close() }()
	st.BindParams(binds)
	rows, err := st.Execute()
	if err != nil {
		return nil, nil, err
	}
	// Columns re-reads the cached column list of the already-executed statement,
	// so once Execute has succeeded it cannot fail.
	cols, _ := st.Columns()
	return rows, cols, nil
}

// sqlite3RawHashRow builds a Ruby Hash from a positional row and its columns.
func sqlite3RawHashRow(vm *VM, row sqlite3.Row, cols []string) object.Value {
	return sqlite3HashRow(vm, row, cols)
}

// sqlite3Execute2 runs sql and returns the column-name header row followed by
// the data rows (Database#execute2).
func (vm *VM) sqlite3Execute2(db *sqlite3.Database, sql string, binds []sqlite3.Value) object.Value {
	rows, cols, err := sqlite3ExecuteHash(db, sql, binds)
	if err != nil {
		raiseSQLite3Error(err)
	}
	out := object.NewArrayFromSlice(make([]object.Value, 0, len(rows)+1))
	out.Elems = append(out.Elems, sqlite3Strings(cols))
	for _, r := range rows {
		out.Elems = append(out.Elems, sqlite3Row(vm, r))
	}
	return out
}

// sqlite3EmitRows returns a statement's rows, yielding to a block when one is
// given (Statement#execute). The row shape follows the owning database's
// results_as_hash flag.
func (vm *VM) sqlite3EmitRows(sw *SQLite3Statement, rows []sqlite3.Row, blk *Proc) object.Value {
	if blk != nil {
		for _, r := range rows {
			vm.callBlock(blk, []object.Value{vm.sqlite3StepRow(sw, r)})
		}
		return object.NilV
	}
	out := object.NewArrayFromSlice(make([]object.Value, len(rows)))
	for i, r := range rows {
		out.Elems[i] = vm.sqlite3StepRow(sw, r)
	}
	return out
}

// sqlite3StepRow renders one row in the shape the statement's owning database
// selects: a Hash keyed by column name when results_as_hash is set, an Array
// otherwise.
func (vm *VM) sqlite3StepRow(sw *SQLite3Statement, row sqlite3.Row) object.Value {
	if cols := sqlite3HashCols(sw); cols != nil {
		return sqlite3HashRow(vm, row, cols)
	}
	return sqlite3Row(vm, row)
}

// sqlite3HashCols returns the statement's column names when its owning database
// has results_as_hash set, or nil when rows should be positional Arrays. A
// statement built outside the Database methods (no owning wrapper) always yields
// positional Arrays.
func sqlite3HashCols(sw *SQLite3Statement) []string {
	if sw.db == nil || !sw.db.db.ResultsAsHash() {
		return nil
	}
	// Only ever called after a row has been produced (Step / Execute ran exec
	// successfully), so Columns re-reads the cached list and cannot fail here.
	cols, _ := sw.st.Columns()
	return cols
}

// sqlite3RunTransaction runs blk inside an already-open transaction, committing
// on normal completion and rolling back (then re-raising) if blk raises. It
// returns the block's value.
func (vm *VM) sqlite3RunTransaction(db *sqlite3.Database, blk *Proc, self object.Value) (result object.Value) {
	committed := false
	defer func() {
		if committed {
			return
		}
		// blk raised (or a later commit failed): roll back and let the panic
		// continue unwinding.
		_ = db.Rollback()
	}()
	result = vm.callBlock(blk, []object.Value{self})
	if err := db.Commit(); err != nil {
		raiseSQLite3Error(err)
	}
	committed = true
	return result
}
