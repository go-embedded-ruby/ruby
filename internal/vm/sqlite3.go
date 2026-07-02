// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	sqlite3 "github.com/go-ruby-sqlite3/sqlite3"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// SQLite3Database is the Ruby wrapper around a *sqlite3.Database — a live
// database connection (SQLite3::Database). The real query engine lives in the
// github.com/go-ruby-sqlite3/sqlite3 library, which drives modernc.org/sqlite
// (pure-Go, no cgo); this shell is the thin wiring that maps Ruby String SQL and
// Array binds to the library's Execute / Prepare / Query calls and maps the
// scanned Go values (int64 / float64 / string / []byte / nil) back to Ruby
// Integer / Float / String / ASCII-8BIT String / nil (see sqlite3_bind.go). It
// is a real, functional database — `:memory:` and file paths both work.
type SQLite3Database struct {
	db *sqlite3.Database
}

func (d *SQLite3Database) ToS() string     { return "#<SQLite3::Database>" }
func (d *SQLite3Database) Inspect() string { return "#<SQLite3::Database>" }
func (d *SQLite3Database) Truthy() bool    { return true }

// SQLite3Statement is the Ruby wrapper around a *sqlite3.Statement — a compiled
// prepared statement (SQLite3::Statement). bind_param / step / columns / reset /
// close map straight onto the library's methods.
type SQLite3Statement struct {
	st *sqlite3.Statement
	// db is the owning database wrapper, so a stepped/executed row can follow the
	// database's results_as_hash flag (the library's Statement does not expose its
	// database). It is nil for a statement built outside the Database methods.
	db *SQLite3Database
}

func (s *SQLite3Statement) ToS() string     { return "#<SQLite3::Statement>" }
func (s *SQLite3Statement) Inspect() string { return "#<SQLite3::Statement>" }
func (s *SQLite3Statement) Truthy() bool    { return true }

// registerSQLite3 installs the SQLite3 module and its Database / Statement
// classes (require "sqlite3"): SQLite3::Database.new / .open, #execute, #query,
// #prepare, #get_first_row / #get_first_value, #transaction, #last_insert_row_id,
// #changes, results_as_hash, plus the SQLite3::Statement cursor API. The
// SQLite3::Exception hierarchy is registered so a database error rescues as the
// gem-faithful Ruby class.
func (vm *VM) registerSQLite3() {
	mod := newClass("SQLite3", nil)
	mod.isModule = true
	vm.consts["SQLite3"] = mod
	vm.registerSQLite3Errors(mod)

	vm.registerSQLite3Database(mod)
	vm.registerSQLite3Statement(mod)
}

// registerSQLite3Errors installs the SQLite3::Exception tree mirroring the gem's
// ext/sqlite3/exception.c hierarchy (Exception < StandardError; every code-mapped
// subclass < Exception). Each class is registered both as a nested constant of
// SQLite3 (so Ruby `SQLite3::BusyException` resolves it) and under its qualified
// name in the top-level table (so a raised library error's class-name lookup
// finds the same class), exactly as the JSON:: / MessagePack:: classes are.
func (vm *VM) registerSQLite3Errors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(qualified string, super *RClass) *RClass {
		simple := qualified[len("SQLite3::"):]
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	exc := reg(string(sqlite3.ExcException), std)
	for _, name := range []sqlite3.ExceptionClass{
		sqlite3.ExcSQLException, sqlite3.ExcInternalException, sqlite3.ExcPermissionException,
		sqlite3.ExcAbortException, sqlite3.ExcBusyException, sqlite3.ExcLockedException,
		sqlite3.ExcMemoryException, sqlite3.ExcReadOnlyException, sqlite3.ExcInterruptException,
		sqlite3.ExcIOException, sqlite3.ExcCorruptException, sqlite3.ExcNotFoundException,
		sqlite3.ExcFullException, sqlite3.ExcCantOpenException, sqlite3.ExcProtocolException,
		sqlite3.ExcEmptyException, sqlite3.ExcSchemaChangedException, sqlite3.ExcTooBigException,
		sqlite3.ExcConstraintException, sqlite3.ExcMismatchException, sqlite3.ExcMisuseException,
		sqlite3.ExcUnsupportedException, sqlite3.ExcAuthorizationException, sqlite3.ExcFormatException,
		sqlite3.ExcRangeException, sqlite3.ExcNotADatabaseException,
	} {
		reg(string(name), exc)
	}
}

// registerSQLite3Database installs SQLite3::Database and its instance methods.
func (vm *VM) registerSQLite3Database(mod *RClass) {
	cls := newClass("SQLite3::Database", vm.cObject)
	mod.consts["Database"] = cls
	vm.consts["SQLite3::Database"] = cls

	// SQLite3::Database.new(path) / .open(path): open (or create) the database at
	// path. ":memory:" opens a private in-memory database. A block form yields the
	// database and closes it afterwards, returning the block's value.
	open := func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		db := sqlite3Open(sqlite3StringArg(args[0]))
		if blk != nil {
			defer func() { _ = db.db.Close() }()
			return vm.callBlock(blk, []object.Value{db})
		}
		return db
	}
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: open}
	cls.smethods["open"] = &Method{name: "open", owner: cls, native: open}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *sqlite3.Database { return v.(*SQLite3Database).db }

	// #execute(sql, binds = []) runs sql and returns its rows. Positional binds
	// come from a trailing Array or the remaining arguments. With results_as_hash
	// true each row is a Hash keyed by column name; otherwise an Array. A block
	// form yields each row and returns nil.
	d("execute", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		db := self(v)
		sql, binds := sqlite3ExecArgs(args)
		return vm.sqlite3Execute(db, sql, binds, blk)
	})

	// #execute2 runs sql and returns the column-name header row followed by the
	// data rows (SQLite3::Database#execute2).
	d("execute2", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		db := self(v)
		sql, binds := sqlite3ExecArgs(args)
		return vm.sqlite3Execute2(db, sql, binds)
	})

	// #query(sql, binds = []) runs sql and returns a SQLite3::Statement cursor
	// positioned before the first row; step/next walks it.
	d("query", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		db := self(v)
		sql, binds := sqlite3ExecArgs(args)
		st, err := db.Query(sql, binds)
		if err != nil {
			raiseSQLite3Error(err)
		}
		return &SQLite3Statement{st: st, db: v.(*SQLite3Database)}
	})

	// #prepare(sql) compiles sql into a SQLite3::Statement without running it.
	d("prepare", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		db := self(v)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		st, err := db.Prepare(sqlite3StringArg(args[0]))
		if err != nil {
			raiseSQLite3Error(err)
		}
		return &SQLite3Statement{st: st, db: v.(*SQLite3Database)}
	})

	// #get_first_row(sql, binds = []) returns the first result row, or nil.
	d("get_first_row", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		db := self(v)
		sql, binds := sqlite3ExecArgs(args)
		row, err := db.GetFirstRow(sql, binds)
		if err != nil {
			raiseSQLite3Error(err)
		}
		if row == nil {
			return object.NilV
		}
		return sqlite3Row(vm, row)
	})

	// #get_first_value(sql, binds = []) returns the first column of the first row,
	// or nil.
	d("get_first_value", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		db := self(v)
		sql, binds := sqlite3ExecArgs(args)
		val, err := db.GetFirstValue(sql, binds)
		if err != nil {
			raiseSQLite3Error(err)
		}
		return sqlite3Value(vm, val)
	})

	// #transaction(mode = :deferred) { ... } runs the block inside a transaction,
	// committing on success and rolling back if the block raises. Without a block
	// it just begins a transaction.
	d("transaction", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		db := self(v)
		mode := sqlite3Mode(args)
		if blk == nil {
			if err := db.Begin(mode); err != nil {
				raiseSQLite3Error(err)
			}
			return object.Bool(true)
		}
		if err := db.Begin(mode); err != nil {
			raiseSQLite3Error(err)
		}
		return vm.sqlite3RunTransaction(db, blk, v)
	})

	// #commit / #rollback / #transaction_active? drive an explicit transaction.
	d("commit", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Commit(); err != nil {
			raiseSQLite3Error(err)
		}
		return object.Bool(true)
	})
	d("rollback", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Rollback(); err != nil {
			raiseSQLite3Error(err)
		}
		return object.Bool(true)
	})
	d("transaction_active?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		active, err := self(v).InTransaction()
		if err != nil {
			raiseSQLite3Error(err)
		}
		return object.Bool(active)
	})

	// #last_insert_row_id returns the rowid of the most recent INSERT.
	d("last_insert_row_id", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		id, err := self(v).LastInsertRowID()
		if err != nil {
			raiseSQLite3Error(err)
		}
		return object.Integer(id)
	})

	// #changes returns the number of rows the most recent statement changed.
	d("changes", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		n, err := self(v).Changes()
		if err != nil {
			raiseSQLite3Error(err)
		}
		return object.Integer(n)
	})

	// #total_changes returns the total rows changed since the connection opened.
	d("total_changes", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		n, err := self(v).TotalChanges()
		if err != nil {
			raiseSQLite3Error(err)
		}
		return object.Integer(n)
	})

	// #results_as_hash / #results_as_hash= read and set the row shape flag.
	d("results_as_hash", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ResultsAsHash())
	})
	d("results_as_hash=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		on := len(args) > 0 && args[0].Truthy()
		self(v).SetResultsAsHash(on)
		return object.Bool(on)
	})

	// #busy_timeout=(ms) sets the busy-handler timeout in milliseconds.
	d("busy_timeout=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ms := 0
		if len(args) > 0 {
			ms = int(sqlite3IntArg(args[0]))
		}
		if err := self(v).BusyTimeout(ms); err != nil {
			raiseSQLite3Error(err)
		}
		return args[0]
	})

	// #path returns the database file path (":memory:" for an in-memory database).
	d("path", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Path())
	})

	// #closed? reports whether #close has run.
	d("closed?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Closed())
	})

	// #close releases the connection.
	d("close", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Close(); err != nil {
			raiseSQLite3Error(err)
		}
		return object.NilV
	})
}

// registerSQLite3Statement installs SQLite3::Statement and its cursor methods.
func (vm *VM) registerSQLite3Statement(mod *RClass) {
	cls := newClass("SQLite3::Statement", vm.cObject)
	mod.consts["Statement"] = cls
	vm.consts["SQLite3::Statement"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *sqlite3.Statement { return v.(*SQLite3Statement).st }
	wrap := func(v object.Value) *SQLite3Statement { return v.(*SQLite3Statement) }

	// #bind_param(key, value) binds a positional (Integer key) or named (String /
	// Symbol key) parameter (SQLite3::Statement#bind_param).
	d("bind_param", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).BindParam(sqlite3BindKey(args[0]), sqlite3Bind(args[1]))
		return v
	})

	// #bind_params(*values) binds a whole positional parameter list at once.
	d("bind_params", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).BindParams(sqlite3Binds(sqlite3Spread(args)))
		return v
	})

	// #execute runs the statement (binding any accumulated params) and returns its
	// rows. With a block it yields each row and returns nil.
	d("execute", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		st := self(v)
		if len(args) > 0 {
			st.BindParams(sqlite3Binds(sqlite3Spread(args)))
		}
		rows, err := st.Execute()
		if err != nil {
			raiseSQLite3Error(err)
		}
		return vm.sqlite3EmitRows(wrap(v), rows, blk)
	})

	// #step advances to the next row and returns it (an Array, or a Hash when the
	// owning database's results_as_hash is set), or nil at the end.
	d("step", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		st := self(v)
		row, ok, err := st.Step()
		if err != nil {
			raiseSQLite3Error(err)
		}
		if !ok {
			return object.NilV
		}
		return vm.sqlite3StepRow(wrap(v), row)
	})
	d("next", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		st := self(v)
		row, ok, err := st.Step()
		if err != nil {
			raiseSQLite3Error(err)
		}
		if !ok {
			return object.NilV
		}
		return vm.sqlite3StepRow(wrap(v), row)
	})

	// #columns returns the result column names.
	d("columns", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		cols, err := self(v).Columns()
		if err != nil {
			raiseSQLite3Error(err)
		}
		return sqlite3Strings(cols)
	})

	// #types returns the declared column type names.
	d("types", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		types, err := self(v).Types()
		if err != nil {
			raiseSQLite3Error(err)
		}
		return sqlite3Strings(types)
	})

	// #reset rewinds the cursor so the statement can be re-run.
	d("reset", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Reset(); err != nil {
			raiseSQLite3Error(err)
		}
		return v
	})

	// #clear_bindings! forgets the accumulated parameters.
	d("clear_bindings!", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).ClearBindings()
		return v
	})

	// #sql returns the statement's source text.
	d("sql", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).SQL())
	})

	// #closed? reports whether #close has run.
	d("closed?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Closed())
	})

	// #close finalises the statement.
	d("close", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Close(); err != nil {
			raiseSQLite3Error(err)
		}
		return object.NilV
	})
}
