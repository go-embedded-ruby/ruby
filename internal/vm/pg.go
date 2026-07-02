// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	pg "github.com/go-ruby-pg/pg"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// PGConnObj is the Ruby wrapper around a *pg.Conn (PG::Connection). exec /
// exec_params / prepare / exec_prepared drive the v3 protocol over the injected
// IO seam; escape_string / quote_ident are pure string helpers.
type PGConnObj struct {
	cls  *RClass
	conn *pg.Conn
	// io is the injected IO-like seam object, kept reachable and returned by
	// #socket_io.
	io object.Value
}

func (c *PGConnObj) ToS() string     { return "#<PG::Connection>" }
func (c *PGConnObj) Inspect() string { return "#<PG::Connection>" }
func (c *PGConnObj) Truthy() bool    { return true }

// registerPG installs the PG module (require "pg"): PG.connect(...) plus the
// PG::Connection query surface and the PG::Result view. The TCP socket is the
// host seam — PG.connect takes an injected IO-like object (connection: io) that
// rubyConn bridges to the library's io.ReadWriter, then drives the
// StartupMessage + MD5/SCRAM handshake over it. The PG::Error tree is registered
// so a server error or transport fault rescues as the gem-faithful class.
func (vm *VM) registerPG() {
	mod := newClass("PG", nil)
	mod.isModule = true
	vm.consts["PG"] = mod
	vm.registerPGErrors(mod)
	vm.registerPGConnection(mod)
	vm.registerPGResult(mod)

	// PG.connect(connection: io, user:, password:, dbname:, ...) opens a session:
	// it writes the StartupMessage and runs the handshake (with a
	// PasswordAuthenticator when user/password are given) over the injected seam,
	// then returns the ready PG::Connection.
	mod.smethods["connect"] = &Method{name: "connect", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.pgConnect(mod, args)
	}}
}

// registerPGErrors installs the PG::Error hierarchy (Error < StandardError;
// ConnectionBad / ServerError < Error), mirroring the pg gem closely enough that
// a raised library error rescues as its gem-faithful class.
func (vm *VM) registerPGErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple string, super *RClass) *RClass {
		qualified := "PG::" + simple
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("Error", std)
	reg("ConnectionBad", base)
	reg("ServerError", base)
}

// pgConnect builds the seam, writes the StartupMessage and runs Startup, then
// returns a ready PG::Connection. It raises PG::ConnectionBad on a handshake
// failure.
func (vm *VM) pgConnect(mod *RClass, args []object.Value) object.Value {
	io, params, user, password, hasAuth := pgConnectArgs(args)
	if io == nil {
		raise("ArgumentError", "PG.connect requires a connection: IO-like object (rbgo has no native socket yet)")
	}
	conn := pg.NewConn(&rubyConn{vm: vm, obj: io})
	// The StartupMessage carries the parameter set; the caller owns it.
	if err := conn.RW.(*rubyConn).writeStartup(pg.EncodeStartup(params)); err != nil {
		raise("PG::ConnectionBad", "%s", err.Error())
	}
	var auth pg.Authenticator
	if hasAuth {
		auth = pg.NewPasswordAuthenticator(user, password)
	}
	if err := conn.Startup(auth); err != nil {
		raisePGError(err)
	}
	return &PGConnObj{cls: pgConnClass(vm, mod), conn: conn, io: io}
}

// writeStartup writes the untyped StartupMessage bytes to the seam.
func (c *rubyConn) writeStartup(b []byte) error {
	_, err := c.Write(b)
	return err
}

// pgConnClass returns the PG::Connection class.
func pgConnClass(vm *VM, mod *RClass) *RClass {
	return mod.consts["Connection"].(*RClass)
}

// registerPGConnection installs PG::Connection and its query methods.
func (vm *VM) registerPGConnection(mod *RClass) {
	cls := newClass("PG::Connection", vm.cObject)
	mod.consts["Connection"] = cls
	vm.consts["PG::Connection"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *pg.Conn { return v.(*PGConnObj).conn }

	// #exec(sql) / #query(sql) run a simple query and return a PG::Result.
	execFn := func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		res, err := self(v).Exec(pgStringArg(args[0]))
		if err != nil {
			raisePGError(err)
		}
		return vm.pgResultOrYield(res, blk)
	}
	d("exec", execFn)
	d("query", execFn)

	// #exec_params(sql, params = []) runs a parameterised query over the extended
	// protocol and returns a PG::Result.
	d("exec_params", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		params := pgArgs(pgParamArray(args[1:]))
		res, err := self(v).ExecParams(pgStringArg(args[0]), params...)
		if err != nil {
			raisePGError(err)
		}
		return vm.pgResultOrYield(res, blk)
	})

	// #prepare(name, sql) compiles a named statement.
	d("prepare", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		if err := self(v).Prepare(pgStringArg(args[0]), pgStringArg(args[1]), nil); err != nil {
			raisePGError(err)
		}
		return object.NilV
	})

	// #exec_prepared(name, params = []) binds and runs a prepared statement.
	d("exec_prepared", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		params := pgArgs(pgParamArray(args[1:]))
		res, err := self(v).ExecPrepared(pgStringArg(args[0]), params...)
		if err != nil {
			raisePGError(err)
		}
		return vm.pgResultOrYield(res, blk)
	})

	// #escape_string(s) / #escape_literal(s) / #escape_identifier(s) /
	// #quote_ident(s) are the pure string helpers.
	d("escape_string", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).EscapeString(pgStringArg(pgArg0(args))))
	})
	d("escape_literal", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).EscapeLiteral(pgStringArg(pgArg0(args))))
	})
	d("escape_identifier", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).EscapeIdentifier(pgStringArg(pgArg0(args))))
	})
	d("quote_ident", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).QuoteIdent(pgStringArg(pgArg0(args))))
	})

	// #finish sends Terminate (PG::Connection#finish's protocol half). A write
	// error on an already-broken connection is not actionable, so it is ignored.
	d("finish", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		_ = self(v).Terminate()
		return object.NilV
	})

	// #socket_io returns the injected seam object (rbgo accessor).
	d("socket_io", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return v.(*PGConnObj).io
	})
}

// pgResultOrYield wraps a *pg.Result and, when a block is given, yields the
// wrapper and returns the block's value (PG::Connection#exec with a block).
func (vm *VM) pgResultOrYield(res *pg.Result, blk *Proc) object.Value {
	w := &PGResultObj{cls: vm.consts["PG::Result"].(*RClass), res: res}
	if blk != nil {
		return vm.callBlock(blk, []object.Value{w})
	}
	return w
}

// registerPGResult installs PG::Result and its accessor methods.
func (vm *VM) registerPGResult(mod *RClass) {
	cls := newClass("PG::Result", vm.cObject)
	mod.consts["Result"] = cls
	vm.consts["PG::Result"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *pg.Result { return v.(*PGResultObj).res }

	// #ntuples / #num_tuples and #nfields / #num_fields.
	d("ntuples", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(self(v).Ntuples()))
	})
	d("num_tuples", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(self(v).Ntuples()))
	})
	d("nfields", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(self(v).Nfields()))
	})
	d("num_fields", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(self(v).Nfields()))
	})

	// #fields returns the column names.
	d("fields", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return pgStrings(self(v).Fields())
	})

	// #fname(i) returns column i's name.
	d("fname", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		name, err := self(v).Fname(int(pgIntArg(pgArg0(args))))
		if err != nil {
			raise("PG::Error", "%s", err.Error())
		}
		return object.NewString(name)
	})

	// #fnumber(name) returns the column index, or -1 if absent.
	d("fnumber", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(self(v).Fnumber(pgStringArg(pgArg0(args)))))
	})

	// #getvalue(row, col) returns the decoded value at (row, col).
	d("getvalue", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		val, err := self(v).Getvalue(int(pgIntArg(args[0])), int(pgIntArg(args[1])))
		if err != nil {
			raise("PG::Error", "%s", err.Error())
		}
		return vm.pgValue(val)
	})

	// #getisnull(row, col) reports whether the cell is SQL NULL.
	d("getisnull", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		null, err := self(v).Getisnull(int(pgIntArg(args[0])), int(pgIntArg(args[1])))
		if err != nil {
			raise("PG::Error", "%s", err.Error())
		}
		return object.Bool(null)
	})

	// #values returns every row as an Array of Arrays.
	d("values", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		rows := self(v).Values()
		out := &object.Array{Elems: make([]object.Value, len(rows))}
		for i, row := range rows {
			inner := &object.Array{Elems: make([]object.Value, len(row))}
			for j, cell := range row {
				inner.Elems[j] = vm.pgValue(cell)
			}
			out.Elems[i] = inner
		}
		return out
	})

	// #[](i) / #tuple(i) return row i as a Hash keyed by column name.
	tupleFn := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		t, err := self(v).Tuple(int(pgIntArg(pgArg0(args))))
		if err != nil {
			raise("PG::Error", "%s", err.Error())
		}
		return vm.pgTuple(self(v), t)
	}
	d("[]", tupleFn)
	d("tuple", tupleFn)

	// #each yields every row as a Hash and returns the result.
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		res := self(v)
		for i := 0; i < res.Ntuples(); i++ {
			// Tuple(i) cannot fail for an in-range i.
			t, _ := res.Tuple(i)
			vm.callBlock(blk, []object.Value{vm.pgTuple(res, t)})
		}
		return v
	})

	// #cmd_tuples returns the affected-row count and #cmd_status the raw tag.
	d("cmd_tuples", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(self(v).CmdTuples()))
	})
	d("cmd_status", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).CmdStatus())
	})

	// #clear is a no-op accessor (the pure Result holds no server handle).
	d("clear", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NilV
	})
}

// pgTuple builds a Ruby Hash from a row's name→value map, keeping the result's
// column order (a Go map has no order, so the fields drive iteration).
func (vm *VM) pgTuple(res *pg.Result, t map[string]any) *object.Hash {
	h := object.NewHash()
	for _, name := range res.Fields() {
		h.Set(object.NewString(name), vm.pgValue(t[name]))
	}
	return h
}
