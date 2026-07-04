// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	mysql "github.com/go-ruby-mysql/mysql"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerMySQL installs the Mysql2 module (require "mysql2" / "mysql"): the
// mysql2 gem surface — Mysql2::Client (connect / query / prepare / escape /
// close / affected_rows / last_id / server_info), the enumerable
// Mysql2::Result, the prepared Mysql2::Statement, and the Mysql2::Error tree
// (Mysql2::Error < StandardError, with ConnectionError / TimeoutError beneath
// it). The connection, SQL execution and MySQL<->Ruby casts live in the
// github.com/go-ruby-mysql/mysql library (a pure-Go mysql2 over
// go-sql-driver/mysql); this file is the class + method wiring (see
// mysql_bind.go for the wrappers and value conversions).
func (vm *VM) registerMySQL() {
	mod := newClass("Mysql2", nil)
	mod.isModule = true
	vm.consts["Mysql2"] = mod

	vm.registerMySQLErrors(mod)
	cClient := vm.mysqlClass(mod, "Client", "Mysql2::Client")
	cResult := vm.mysqlClass(mod, "Result", "Mysql2::Result")
	cStatement := vm.mysqlClass(mod, "Statement", "Mysql2::Statement")

	vm.registerMySQLClient(cClient)
	vm.registerMySQLResult(cResult)
	vm.registerMySQLStatement(cStatement)
}

// includeMySQLEnumerable makes Mysql2::Result include Enumerable (mysql2's
// Result is Enumerable), so #map / #select / #to_a / … flow through the defined
// #each. Enumerable is a prelude module, so this runs after the prelude has
// loaded (see the post-prelude phase in New), not during registerMySQL.
func (vm *VM) includeMySQLEnumerable() {
	cResult := vm.consts["Mysql2::Result"].(*RClass)
	en := vm.consts["Enumerable"].(*RClass)
	cResult.includes = append(cResult.includes, en)
}

// mysqlClass creates a Mysql2::* class under cObject, records it flat (for
// classOf) and nests it under the Mysql2 module by its simple name.
func (vm *VM) mysqlClass(mod *RClass, simple, qualified string) *RClass {
	c := newClass(qualified, vm.cObject)
	vm.consts[qualified] = c
	mod.consts[simple] = c
	return c
}

// registerMySQLErrors installs the Mysql2::Error tree, mirroring the gem: the
// root Mysql2::Error < StandardError with #error_number / #errno / #sql_state,
// and the ConnectionError / TimeoutError subclasses beneath it (a connection
// failure raises ConnectionError; a SQL error raises Mysql2::Error).
func (vm *VM) registerMySQLErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("Mysql2::Error", std)
	mod.consts["Error"] = base
	vm.consts["Mysql2::Error"] = base

	for _, name := range []string{"ConnectionError", "TimeoutError"} {
		c := newClass("Mysql2::Error::"+name, base)
		base.consts[name] = c
		vm.consts["Mysql2::Error::"+name] = c
	}

	base.define("error_number", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@error_number")
	})
	base.define("errno", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@error_number")
	})
	base.define("sql_state", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@sql_state")
	})
}

// registerMySQLClient installs Mysql2::Client: the connecting constructor and
// the query / prepare / escape / lifecycle surface.
func (vm *VM) registerMySQLClient(cls *RClass) {
	// Mysql2::Client.new(host:, port:, username:, password:, database:, ...)
	// opens the connection (eager, like the gem) and raises
	// Mysql2::Error::ConnectionError on failure.
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		opts, qdef := mysqlConnectOptions(mysqlHashArg(args, 0))
		c, err := mysql.NewClient(opts)
		if err != nil {
			vm.raiseMySQL("Mysql2::Error::ConnectionError", err)
		}
		return &MySQLClient{cls: cls, c: c, qdef: qdef}
	}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *MySQLClient { return v.(*MySQLClient) }

	// #query(sql, options = {}) runs sql and returns a Mysql2::Result for a
	// row-producing statement, or nil for a statement that changes rows.
	d("query", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		c := self(v)
		qo := c.qdef
		if h := mysqlHashArg(args, 1); h != nil {
			qo = mysqlQueryOptions(c.qdef, h)
		}
		res, err := c.c.Query(mysqlStr(args[0]), qo)
		if err != nil {
			vm.raiseMySQL("Mysql2::Error", err)
		}
		return vm.mysqlResultOrNil(res)
	})

	// #prepare(sql) compiles a Mysql2::Statement with positional `?` binds.
	d("prepare", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		c := self(v)
		st, err := c.c.Prepare(mysqlStr(args[0]))
		if err != nil {
			vm.raiseMySQL("Mysql2::Error", err)
		}
		return &MySQLStatement{cls: vm.consts["Mysql2::Statement"].(*RClass), s: st, client: c}
	})

	// #escape(str) / #escape_string(str) escapes for safe interpolation.
	escapeFn := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.NewString(self(v).c.Escape(mysqlStr(args[0])))
	}
	d("escape", escapeFn)
	d("escape_string", escapeFn)

	// #affected_rows / #last_id / #insert_id report the most recent statement's
	// side effects.
	d("affected_rows", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).c.AffectedRows())
	})
	lastIDFn := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).c.LastID())
	}
	d("last_id", lastIDFn)
	d("insert_id", lastIDFn)

	// #ping checks the connection is alive; #close closes it (idempotent);
	// #closed? reports whether it is closed.
	d("ping", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).c.Ping())
	})
	d("close", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		// Close is idempotent and only fails on an already-broken pool, which
		// mysql2's #close swallows; ignore the error to match the gem.
		_ = self(v).c.Close()
		return object.NilV
	})
	d("closed?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).c.Closed())
	})

	// #server_info returns the {version:, id:} Hash mysql2 exposes.
	d("server_info", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		si, err := self(v).c.ServerInfo()
		if err != nil {
			vm.raiseMySQL("Mysql2::Error", err)
		}
		h := object.NewHash()
		h.Set(object.Symbol("version"), object.NewString(si.Version))
		h.Set(object.Symbol("id"), object.IntValue(int64(si.VersionNumber)))
		return h
	})

	// #query_options returns the client's default query options Hash
	// (mysql2's Client#query_options).
	d("query_options", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		qd := self(v).qdef
		h := object.NewHash()
		h.Set(object.Symbol("as"), object.Symbol(qd.As))
		h.Set(object.Symbol("symbolize_keys"), object.Bool(qd.SymbolizeKeys))
		h.Set(object.Symbol("cast"), object.Bool(qd.Cast))
		h.Set(object.Symbol("cast_booleans"), object.Bool(qd.CastBooleans))
		return h
	})
}

// mysqlResultOrNil wraps a *mysql.Result as a Mysql2::Result, or returns nil for
// a row-changing statement (mysql2's #query returns nil there).
func (vm *VM) mysqlResultOrNil(res *mysql.Result) object.Value {
	if res == nil {
		return object.NilV
	}
	return &MySQLResult{cls: vm.consts["Mysql2::Result"].(*RClass), r: res}
}

// registerMySQLResult installs the enumerable Mysql2::Result surface.
func (vm *VM) registerMySQLResult(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *mysql.Result { return v.(*MySQLResult).r }

	// #each yields every row in the result's shape (a Hash by default, an Array
	// with as: :array) and returns the result. It is the basis of the included
	// Enumerable (#map / #select / #to_a / …).
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		for _, row := range vm.mysqlRows(self(v)) {
			vm.callBlock(blk, []object.Value{row})
		}
		return v
	})

	// #to_a / #entries return every row as an Array.
	toaFn := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(vm.mysqlRows(self(v)))
	}
	d("to_a", toaFn)
	d("entries", toaFn)

	// #count / #size return the row count.
	countFn := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Count()))
	}
	d("count", countFn)
	d("size", countFn)

	// #fields returns the column names in column order.
	d("fields", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		names := self(v).Fields()
		arr := object.NewArrayFromSlice(make([]object.Value, len(names)))
		for i, name := range names {
			arr.Elems[i] = object.NewString(name)
		}
		return arr
	})

	// #first returns the first row (in the result's shape), or nil when empty.
	d("first", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		rows := vm.mysqlRows(self(v))
		if len(rows) == 0 {
			return object.NilV
		}
		return rows[0]
	})
}

// registerMySQLStatement installs the prepared Mysql2::Statement surface.
func (vm *VM) registerMySQLStatement(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *MySQLStatement { return v.(*MySQLStatement) }

	// #execute(*binds) binds the positional parameters and runs the statement,
	// returning a Mysql2::Result for a row-producing statement or nil otherwise
	// (after which the owning client's #affected_rows / #last_id reflect it).
	d("execute", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		res, err := self(v).s.Execute(mysqlBinds(args)...)
		if err != nil {
			vm.raiseMySQL("Mysql2::Error", err)
		}
		return vm.mysqlResultOrNil(res)
	})

	// #sql returns the statement's source text.
	d("sql", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).s.SQL())
	})

	// #close releases the statement (idempotent); #closed? reports its state.
	d("close", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		_ = self(v).s.Close()
		return object.NilV
	})
	d("closed?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).s.Closed())
	})
}
