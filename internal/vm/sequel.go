// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	sequel "github.com/go-ruby-sequel/sequel"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerSequel installs the Sequel toolkit (require "sequel"): Sequel.sqlite /
// Sequel.connect / Sequel.mock build a Sequel::Database; DB[:table] returns a
// Sequel::Dataset with the chainable query surface (where/select/order/join/…)
// whose terminal methods (sql/all/first/insert/update/delete) build or run SQL;
// create_table drives the schema DSL. The executor seam is wired to
// go-ruby-sqlite3 (Sequel.sqlite), so dataset execution really runs against a
// live SQLite database. The Sequel::Error tree is registered so a database error
// rescues as the gem-faithful class.
func (vm *VM) registerSequel() {
	mod := newClass("Sequel", nil)
	mod.isModule = true
	vm.consts["Sequel"] = mod
	vm.registerSequelErrors(mod)
	vm.registerSequelDatabase(mod)
	vm.registerSequelDataset(mod)

	// Sequel.sqlite(path = ":memory:") opens a SQLite-backed database with a real
	// executor (go-ruby-sqlite3). This is the real-execution entry point.
	mod.smethods["sqlite"] = &Method{name: "sqlite", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		path := ":memory:"
		if len(args) > 0 {
			path = pgStringArg(args[0])
		}
		sw := sqlite3Open(path)
		exec := &sqliteExecutor{db: sw.db}
		db := sequel.Connect("sqlite", exec)
		return &SequelDBObj{cls: sequelDBClass(vm, mod), db: db, sqlite: sw}
	}}

	// Sequel.connect(adapter: :sqlite, database: path) mirrors the gem's keyword
	// connect for the SQLite adapter; other adapters build an executor-less
	// (mock) database that generates SQL but runs nothing (no native socket yet).
	mod.smethods["connect"] = &Method{name: "connect", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.sequelConnect(mod, args)
	}}

	// Sequel.mock(host: :sqlite) builds an executor-less database (generates SQL,
	// logs DDL, runs nothing).
	mod.smethods["mock"] = &Method{name: "mock", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		dialect := "default"
		if len(args) > 0 {
			if h, ok := args[0].(*object.Hash); ok {
				if v, ok := sequelKw(h, "host"); ok {
					dialect = pgStringArg(v)
				}
			}
		}
		return &SequelDBObj{cls: sequelDBClass(vm, mod), db: sequel.Mock(dialect)}
	}}
}

// callBlockDSL runs a DSL block (create_table) with self bound to the generator
// (instance_eval semantics), also passing the generator as the block argument so
// both the bare `primary_key :id` and the `do |t| t.primary_key :id end` forms
// resolve.
func (vm *VM) callBlockDSL(blk *Proc, gen object.Value) object.Value {
	return vm.callBlockSelf(blk, gen, []object.Value{gen})
}

// sequelConnect handles Sequel.connect: a SQLite adapter with a database path
// gets a real go-ruby-sqlite3 executor; anything else is a mock database.
func (vm *VM) sequelConnect(mod *RClass, args []object.Value) object.Value {
	adapter, database := "", ":memory:"
	if len(args) > 0 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			if v, ok := sequelKw(h, "adapter"); ok {
				adapter = pgStringArg(v)
			}
			if v, ok := sequelKw(h, "database"); ok {
				database = pgStringArg(v)
			}
		} else if s, ok := args[0].(*object.String); ok {
			// A connection string "sqlite://path" / "sqlite::memory:".
			adapter, database = sequelParseURL(s.Str())
		}
	}
	if adapter == "sqlite" {
		sw := sqlite3Open(database)
		db := sequel.Connect("sqlite", &sqliteExecutor{db: sw.db})
		return &SequelDBObj{cls: sequelDBClass(vm, mod), db: db, sqlite: sw}
	}
	dialect := adapter
	if dialect == "" {
		dialect = "default"
	}
	return &SequelDBObj{cls: sequelDBClass(vm, mod), db: sequel.Mock(dialect)}
}

// sequelParseURL splits a "sqlite://path" / "postgres://..." connection string
// into its adapter and database. Only the adapter scheme and the path are read.
func sequelParseURL(url string) (adapter, database string) {
	i := 0
	for i < len(url) && url[i] != ':' {
		i++
	}
	adapter = url[:i]
	rest := url[i:]
	// Strip a leading "://" or ":".
	if len(rest) >= 3 && rest[:3] == "://" {
		database = rest[3:]
	} else if len(rest) >= 1 {
		database = rest[1:]
	}
	if database == "" {
		database = ":memory:"
	}
	return adapter, database
}

// sequelKw looks up a keyword by Symbol or String name in a Hash.
func sequelKw(h *object.Hash, name string) (object.Value, bool) {
	if v, ok := h.Get(object.Symbol(name)); ok {
		return v, true
	}
	return h.Get(object.NewString(name))
}

// registerSequelErrors installs the Sequel::Error tree (Error < StandardError;
// DatabaseError < Error).
func (vm *VM) registerSequelErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple string, super *RClass) *RClass {
		qualified := "Sequel::" + simple
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("Error", std)
	reg("DatabaseError", base)
}

// sequelDBClass returns the Sequel::Database class.
func sequelDBClass(vm *VM, mod *RClass) *RClass {
	return mod.consts["Database"].(*RClass)
}

// registerSequelDatabase installs Sequel::Database and its methods.
func (vm *VM) registerSequelDatabase(mod *RClass) {
	cls := newClass("Sequel::Database", vm.cObject)
	mod.consts["Database"] = cls
	vm.consts["Sequel::Database"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *SequelDBObj { return v.(*SequelDBObj) }

	// DB[:table] / DB.from(:table) return a dataset over the named source(s).
	d("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		o := self(v)
		return o.dataset(vm, o.db.From(sequelColumns(args)...))
	})
	d("from", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o := self(v)
		return o.dataset(vm, o.db.From(sequelColumns(args)...))
	})

	// DB.run(sql) executes a raw statement (returns nil; rows are for datasets).
	d("run", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if _, err := self(v).db.Run(pgStringArg(args[0])); err != nil {
			raiseSequelError(err)
		}
		return object.NilV
	})

	// DB.create_table(:t) { ... } builds and runs CREATE TABLE via the schema
	// DSL. The block yields a Sequel::Schema::Generator-like builder.
	d("create_table", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		table := sequelName(args[0])
		self(v).db.CreateTable(table, func(tb *sequel.TableBuilder) {
			gen := &SequelSchemaObj{cls: vm.sequelSchemaClass(), tb: tb}
			// Sequel's create_table block runs with self bound to the generator
			// (instance_eval), so a bare `primary_key :id` resolves; the generator
			// is also passed as the block argument for the `do |t| ... end` form.
			vm.callBlockDSL(blk, gen)
		})
		return object.NilV
	})

	// DB.drop_table(:t, ...) runs DROP TABLE.
	d("drop_table", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		names := make([]string, len(args))
		for i, a := range args {
			names[i] = sequelName(a)
		}
		self(v).db.DropTable(names...)
		return object.NilV
	})

	// DB.sqls returns and clears the logged DDL/statements (mock adapter).
	d("sqls", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return pgStrings(self(v).db.SQLs())
	})

	// DB._sqlite3 returns the backing SQLite3::Database (rbgo accessor), or nil.
	d("_sqlite3", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if o := self(v); o.sqlite != nil {
			return o.sqlite
		}
		return object.NilV
	})
}

// dataset wraps a *sequel.Dataset as a Ruby Sequel::Dataset bound to this DB.
func (o *SequelDBObj) dataset(vm *VM, ds *sequel.Dataset) *SequelDatasetObj {
	return &SequelDatasetObj{cls: vm.consts["Sequel::Dataset"].(*RClass), db: o, ds: ds}
}

// registerSequelDataset installs Sequel::Dataset and its chainable + terminal
// methods.
func (vm *VM) registerSequelDataset(mod *RClass) {
	cls := newClass("Sequel::Dataset", vm.cObject)
	mod.consts["Dataset"] = cls
	vm.consts["Sequel::Dataset"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *SequelDatasetObj { return v.(*SequelDatasetObj) }
	// chain builds a new dataset wrapper from a library dataset, keeping the DB.
	chain := func(v object.Value, ds *sequel.Dataset) object.Value {
		return &SequelDatasetObj{cls: self(v).cls, db: self(v).db, ds: ds}
	}

	// --- chainable query builders ---
	d("where", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).ds.Where(sequelCond(args)))
	})
	d("exclude", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).ds.Exclude(sequelCond(args)))
	})
	d("select", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).ds.Select(sequelColumns(args)...))
	})
	d("order", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).ds.Order(sequelColumns(args)...))
	})
	d("reverse", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).ds.Reverse())
	})
	d("group", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).ds.Group(sequelColumns(args)...))
	})
	d("having", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).ds.Having(sequelCond(args)))
	})
	d("distinct", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).ds.Distinct())
	})
	d("limit", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 2 {
			return chain(v, self(v).ds.LimitOffset(int(pgIntArg(args[0])), int(pgIntArg(args[1]))))
		}
		return chain(v, self(v).ds.Limit(int(pgIntArg(pgArg0(args)))))
	})
	d("offset", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return chain(v, self(v).ds.Offset(int(pgIntArg(pgArg0(args)))))
	})
	d("join", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		table, cond := sequelJoinArgs(args)
		return chain(v, self(v).ds.Join(table, cond))
	})
	d("inner_join", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		table, cond := sequelJoinArgs(args)
		return chain(v, self(v).ds.InnerJoin(table, cond))
	})
	d("left_join", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		table, cond := sequelJoinArgs(args)
		return chain(v, self(v).ds.LeftJoin(table, cond))
	})
	d("right_join", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		table, cond := sequelJoinArgs(args)
		return chain(v, self(v).ds.RightJoin(table, cond))
	})

	// --- terminal SQL / execution methods ---
	// #sql / #select_sql return the SELECT text.
	sqlFn := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ds.SQL())
	}
	d("sql", sqlFn)
	d("select_sql", sqlFn)

	// #insert_sql / #update_sql / #delete_sql return the DML text.
	d("insert_sql", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ds.InsertSQL(sequelKVArgs(args)...))
	})
	d("update_sql", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ds.UpdateSQL(sequelKVArgs(args)...))
	})
	d("delete_sql", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ds.DeleteSQL())
	})

	// #all runs the SELECT through the executor and returns its rows as an Array
	// of symbol-keyed Hashes. With a block it yields each row and returns the
	// rows.
	d("all", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		o := self(v)
		rows, err := o.db.db.All(o.ds)
		if err != nil {
			raiseSequelError(err)
		}
		out := sequelRows(rows)
		if blk != nil {
			for _, r := range out.Elems {
				vm.callBlock(blk, []object.Value{r})
			}
		}
		return out
	})

	// #first runs the SELECT and returns the first row (a Hash), or nil.
	d("first", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self(v)
		rows, err := o.db.db.All(o.ds)
		if err != nil {
			raiseSequelError(err)
		}
		if len(rows) == 0 {
			return object.NilV
		}
		return sequelRow(rows[0])
	})

	// #each yields each row (a Hash) and returns the dataset.
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		o := self(v)
		rows, err := o.db.db.All(o.ds)
		if err != nil {
			raiseSequelError(err)
		}
		for _, r := range sequelRows(rows).Elems {
			vm.callBlock(blk, []object.Value{r})
		}
		return v
	})

	// #count runs SELECT count(*) and returns the count.
	d("count", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self(v)
		ds := o.ds.Select(sequel.Function("count", sequel.Lit("*")))
		rows, err := o.db.db.All(ds)
		if err != nil {
			raiseSequelError(err)
		}
		return sequelCountValue(rows)
	})

	// #insert runs the INSERT and returns the last inserted row id (SQLite).
	d("insert", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o := self(v)
		if _, err := o.db.db.Run(o.ds.InsertSQL(sequelKVArgs(args)...)); err != nil {
			raiseSequelError(err)
		}
		return o.lastInsertID()
	})

	// #update runs the UPDATE and returns the number of affected rows.
	d("update", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o := self(v)
		if _, err := o.db.db.Run(o.ds.UpdateSQL(sequelKVArgs(args)...)); err != nil {
			raiseSequelError(err)
		}
		return o.changes()
	})

	// #delete runs the DELETE and returns the number of affected rows.
	d("delete", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self(v)
		if _, err := o.db.db.Run(o.ds.DeleteSQL()); err != nil {
			raiseSequelError(err)
		}
		return o.changes()
	})
}

// lastInsertID returns the SQLite last-insert rowid when the dataset is backed by
// a real SQLite executor, or nil for a mock database. The rowid query cannot fail
// on the open connection a successful INSERT just ran against, so its error is
// not actionable and is ignored (id is 0 in that impossible case).
func (d *SequelDatasetObj) lastInsertID() object.Value {
	sw, ok := d.db.sqlite.(*SQLite3Database)
	if !ok {
		return object.NilV
	}
	id, _ := sw.db.LastInsertRowID()
	return object.IntValue(id)
}

// changes returns the SQLite affected-row count when backed by a real executor,
// or nil for a mock database. Like lastInsertID, the count query cannot fail on
// the open connection, so its error is ignored.
func (d *SequelDatasetObj) changes() object.Value {
	sw, ok := d.db.sqlite.(*SQLite3Database)
	if !ok {
		return object.NilV
	}
	n, _ := sw.db.Changes()
	return object.IntValue(n)
}
