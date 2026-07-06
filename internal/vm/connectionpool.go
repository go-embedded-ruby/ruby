// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	cp "github.com/go-ruby-connection-pool/connection-pool"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerConnectionPool installs the ConnectionPool class (require
// "connection_pool"): the generic, thread-safe pool of reusable connections from
// the connection_pool gem — lazy creation up to a fixed size, a timeout-bounded
// #with / #checkout that raises ConnectionPool::TimeoutError on exhaustion,
// per-caller reentrant checkout, #shutdown / #reload disposal, and the
// method-delegating ConnectionPool::Wrapper. The pool mechanics live in the
// github.com/go-ruby-connection-pool/connection-pool library — the pure-Go port of
// the gem — while this file is the thin shell mapping rbgo's block/thread model
// onto that library's Factory, CallerKey and Dispatch seams (see
// connectionpool_bind.go for the value types and seam wiring). The error tree
// mirrors the gem: ConnectionPool::Error < RuntimeError, its
// PoolShuttingDownError subclass, and TimeoutError < Timeout::Error.
func (vm *VM) registerConnectionPool() {
	cls := newClass("ConnectionPool", vm.cObject)
	vm.consts["ConnectionPool"] = cls

	wrapper := vm.registerConnectionPoolWrapper(cls)
	vm.registerConnectionPoolErrors(cls)

	// ConnectionPool.new(size: 5, timeout: 5) { <factory> } builds a pool that
	// lazily creates at most size connections by running the block and blocks up to
	// timeout seconds in a checkout waiting for a free one.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			return vm.cpNewPool(cls, args, blk)
		}}

	// ConnectionPool.wrap(size: 5, timeout: 5) { <factory> } builds a pool and
	// wraps it in one call, returning a ConnectionPool::Wrapper.
	cls.smethods["wrap"] = &Method{name: "wrap", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			return vm.cpWrap(wrapper, vm.cpNewPool(cls, args, blk))
		}}

	self := func(v object.Value) *RConnectionPool { return v.(*RConnectionPool) }

	// #with(options = {}) { |conn| ... } checks a connection out, yields it, and
	// checks it back in even if the block raises (the gem's ensure). A nested #with
	// on the same thread reuses the same connection. :timeout overrides the pool's.
	cls.define("with", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		p := self(v)
		key := vm.cpKey()
		conn := vm.cpCheckout(p, key, cpTimeout(p, args))
		defer func() { _ = p.pool.Checkin(key) }()
		return vm.callBlock(blk, []object.Value{conn})
	})

	// #checkout(options = {}) checks a connection out and returns it; the caller is
	// responsible for a matching #checkin. :timeout overrides the pool's.
	cls.define("checkout", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		p := self(v)
		return vm.cpCheckout(p, vm.cpKey(), cpTimeout(p, args))
	})

	// #checkin returns the running thread's connection to the pool, unwinding one
	// level of a reentrant checkout. It raises ConnectionPool::Error when the thread
	// holds no connection.
	cls.define("checkin", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).pool.Checkin(vm.cpKey()); err != nil {
			return vm.raiseCPError(err)
		}
		return object.NilV
	})

	// #shutdown { |conn| ... } disposes every idle connection through the block and
	// marks the pool shut down, so later checkouts raise
	// ConnectionPool::PoolShuttingDownError. A block is required.
	cls.define("shutdown", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "shutdown must receive a block")
		}
		self(v).pool.Shutdown(func(conn any) { vm.callBlock(blk, []object.Value{conn.(object.Value)}) })
		return object.NilV
	})

	// #reload { |conn| ... } disposes every idle connection through the block and
	// resets the pool for reuse, so it creates fresh connections on demand again.
	cls.define("reload", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "reload must receive a block")
		}
		self(v).pool.Reload(func(conn any) { vm.callBlock(blk, []object.Value{conn.(object.Value)}) })
		return object.NilV
	})

	// #size is the maximum number of connections the pool will create.
	cls.define("size", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).pool.Size()))
	})

	// #available is how many connections can be checked out right now without
	// waiting (idle plus not-yet-created).
	cls.define("available", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).pool.Available()))
	})
}

// registerConnectionPoolWrapper installs ConnectionPool::Wrapper, the delegating
// façade: every method it does not define itself is forwarded (via method_missing)
// to a connection borrowed from the pool for the duration of that call, through
// the library's Dispatch seam. It returns the class so ConnectionPool.wrap can
// build instances of it.
func (vm *VM) registerConnectionPoolWrapper(mod *RClass) *RClass {
	cls := newClass("ConnectionPool::Wrapper", vm.cObject)
	mod.consts["Wrapper"] = cls
	vm.consts["ConnectionPool::Wrapper"] = cls

	// ConnectionPool::Wrapper.new(pool: existing) or .new(size:, timeout:) { block }
	// wraps a given pool, or builds one from the options and block.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			if h := cpOptions(args); h != nil {
				if pv, ok := h.Get(object.Symbol("pool")); ok {
					pool, ok := pv.(*RConnectionPool)
					if !ok {
						raise("TypeError", "pool: must be a ConnectionPool")
					}
					return vm.cpWrap(cls, pool)
				}
			}
			return vm.cpWrap(cls, vm.cpNewPool(vm.consts["ConnectionPool"].(*RClass), args, blk))
		}}

	self := func(v object.Value) *RConnectionPoolWrapper { return v.(*RConnectionPoolWrapper) }

	// method_missing(name, *args) delegates the call to a borrowed connection: it
	// checks one out, sends name to it, and checks it back in.
	cls.define("method_missing", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		wr := self(v)
		name := builderName(args[0])
		cargs := make([]any, len(args)-1)
		for i, a := range args[1:] {
			cargs[i] = a
		}
		res, err := wr.w.Call(name, cargs...)
		if err != nil {
			return vm.raiseCPError(err)
		}
		return res.(object.Value)
	})

	// respond_to_missing?(name, include_private) reports whether a borrowed
	// connection would answer name (or the Wrapper answers it itself).
	cls.define("respond_to_missing?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		name := builderName(args[0])
		ok, err := self(v).w.RespondTo(name, func(conn any) bool {
			return vm.send(conn.(object.Value), "respond_to?", []object.Value{object.Symbol(name)}, nil).Truthy()
		})
		if err != nil {
			return vm.raiseCPError(err)
		}
		return object.Bool(ok)
	})

	// #with { |conn| ... } runs the block with a borrowed connection, exactly like
	// the pool's #with but keyed on the Wrapper's caller source.
	cls.define("with", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		res, err := self(v).w.With(func(conn any) (any, error) {
			return vm.callBlock(blk, []object.Value{conn.(object.Value)}), nil
		})
		if err != nil {
			return vm.raiseCPError(err)
		}
		return res.(object.Value)
	})

	// #wrapped_pool returns the underlying ConnectionPool.
	cls.define("wrapped_pool", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).pool
	})

	// #pool_shutdown { |conn| ... } disposes the underlying pool through the block.
	cls.define("pool_shutdown", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "shutdown must receive a block")
		}
		self(v).w.PoolShutdown(func(conn any) { vm.callBlock(blk, []object.Value{conn.(object.Value)}) })
		return object.NilV
	})

	// #pool_size / #pool_available report the underlying pool's size and current
	// availability.
	cls.define("pool_size", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).w.PoolSize()))
	})
	cls.define("pool_available", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).w.PoolAvailable()))
	})

	return cls
}

// cpWrap wraps pool in a ConnectionPool::Wrapper of class cls, wiring the library
// Wrapper to Ruby's send (Dispatch) and to the current-thread caller key (KeyFunc).
func (vm *VM) cpWrap(cls *RClass, pool *RConnectionPool) *RConnectionPoolWrapper {
	w := cp.NewWrapper(pool.pool, func() cp.CallerKey { return vm.cpKey() }, vm.cpDispatch)
	return &RConnectionPoolWrapper{cls: cls, w: w, pool: pool}
}

// registerConnectionPoolErrors installs the ConnectionPool error tree mirroring
// the gem: ConnectionPool::Error < RuntimeError, its PoolShuttingDownError
// subclass, and TimeoutError < Timeout::Error. Each is registered both scoped
// (under ConnectionPool) and flat in vm.consts so raise finds it by qualified name.
func (vm *VM) registerConnectionPoolErrors(mod *RClass) {
	runtimeErr := vm.consts["RuntimeError"].(*RClass)
	base := newClass("ConnectionPool::Error", runtimeErr)
	mod.consts["Error"] = base
	vm.consts["ConnectionPool::Error"] = base

	down := newClass("ConnectionPool::PoolShuttingDownError", base)
	mod.consts["PoolShuttingDownError"] = down
	vm.consts["ConnectionPool::PoolShuttingDownError"] = down

	timeoutErr := vm.consts["Timeout"].(*RClass).consts["Error"].(*RClass)
	tmo := newClass("ConnectionPool::TimeoutError", timeoutErr)
	mod.consts["TimeoutError"] = tmo
	vm.consts["ConnectionPool::TimeoutError"] = tmo
}
