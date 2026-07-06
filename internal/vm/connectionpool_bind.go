// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	cp "github.com/go-ruby-connection-pool/connection-pool"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent pool of github.com/go-ruby-connection-pool/connection-pool
// (a pure-Go, no-cgo reimplementation of the connection_pool gem). It carries the
// two instance value types the module exposes — a ConnectionPool and its
// method-delegating Wrapper — plus the seam wiring that lets the library call back
// into Ruby: the Factory that runs the pool's block, the reentrancy CallerKey that
// stands in for Thread.current, and the Dispatch that routes a Wrapper's delegated
// calls through Ruby's send. See connectionpool.go for the class/method wiring.
//
// # Caller key
//
// The library identifies a logical caller with a CallerKey so a nested #with on
// the same caller reuses one connection (the gem keys this on Thread.current in
// thread-local storage). rbgo supplies the current VM thread as the key, so two
// nested checkouts on the same Ruby thread nest exactly as the gem's do.
//
// # GVL cooperation on a blocking checkout
//
// A connection is created lazily by running the Ruby factory block, which must
// hold the GVL. The library only ever runs the factory on the non-blocking path
// (a connection is available or the pool is below capacity); once the pool is
// exhausted a checkout parks on a condition variable without touching the factory.
// cpCheckout exploits this: it first tries a zero-timeout checkout under the GVL
// (which may run the factory safely), and only if that reports exhaustion does it
// release the GVL and park for the real timeout — a path on which the factory can
// never run, so no other Ruby thread is starved while one waits for a connection.

// RConnectionPool is an instance of ConnectionPool: a thread-safe pool of reusable
// connections backed by a go-ruby-connection-pool *cp.ConnectionPool. Connections
// are created lazily by running the block captured at construction (factory), up
// to size of them, and a checkout blocks up to timeout before raising
// ConnectionPool::TimeoutError.
type RConnectionPool struct {
	cls     *RClass
	pool    *cp.ConnectionPool
	factory *Proc
	timeout time.Duration
	size    int
}

func (p *RConnectionPool) ToS() string     { return "#<ConnectionPool>" }
func (p *RConnectionPool) Inspect() string { return "#<ConnectionPool>" }
func (p *RConnectionPool) Truthy() bool    { return true }

// RConnectionPoolWrapper is an instance of ConnectionPool::Wrapper: a delegating
// façade over a pool that checks a connection out for the duration of every
// delegated call. It is backed by a go-ruby-connection-pool *cp.Wrapper wired to
// Ruby's send (dispatch) and to the current-thread caller key (keyFn); pool is the
// Ruby ConnectionPool it wraps, returned by #wrapped_pool.
type RConnectionPoolWrapper struct {
	cls  *RClass
	w    *cp.Wrapper
	pool *RConnectionPool
}

func (w *RConnectionPoolWrapper) ToS() string     { return "#<ConnectionPool::Wrapper>" }
func (w *RConnectionPoolWrapper) Inspect() string { return "#<ConnectionPool::Wrapper>" }
func (w *RConnectionPoolWrapper) Truthy() bool    { return true }

// cpNewPool builds an RConnectionPool from a ConnectionPool.new / .wrap options
// Hash (size:, timeout:) and the factory block. It mirrors the gem defaults
// (size 5, timeout 5s) and raises ArgumentError when no block is given, exactly as
// ConnectionPool.new does. The factory closure runs the block under the GVL to
// create a connection; the library invokes it lazily, at most size times.
func (vm *VM) cpNewPool(cls *RClass, args []object.Value, blk *Proc) *RConnectionPool {
	size := 5
	timeout := 5 * time.Second
	if h := cpOptions(args); h != nil {
		if v, ok := h.Get(object.Symbol("size")); ok {
			size = int(intArg(v))
		}
		if v, ok := h.Get(object.Symbol("timeout")); ok {
			timeout = cpDuration(v)
		}
	}
	if blk == nil {
		raise("ArgumentError", "Connection pool requires a block")
	}
	p := &RConnectionPool{cls: cls, timeout: timeout, size: size, factory: blk}
	p.pool = cp.New(size, timeout, func() any { return vm.callBlock(blk, nil) })
	return p
}

// cpOptions returns the trailing keyword Hash of a ConnectionPool.new / .wrap /
// Wrapper.new call, or nil when the call carries no options.
func cpOptions(args []object.Value) *object.Hash {
	if len(args) == 0 {
		return nil
	}
	if h, ok := args[len(args)-1].(*object.Hash); ok {
		return h
	}
	return nil
}

// cpDuration reads a timeout: option as a number of seconds, accepting an Integer
// or a Float and raising TypeError for anything else — the gem accepts either.
func cpDuration(v object.Value) time.Duration {
	switch n := v.(type) {
	case object.Integer:
		return time.Duration(int64(n)) * time.Second
	case object.Float:
		return time.Duration(float64(n) * float64(time.Second))
	}
	raise("TypeError", "no implicit conversion of %s into Numeric", v.Inspect())
	return 0
}

// cpKey returns the reentrancy CallerKey for the running Ruby thread. Two
// checkouts sharing this key nest — the second reuses the first's connection —
// standing in for the gem's per-Thread.current storage.
func (vm *VM) cpKey() cp.CallerKey { return cp.CallerKey(vm.currentThread) }

// cpTimeout returns the per-call timeout: the :timeout override in an options Hash
// when present, otherwise the pool's configured timeout.
func cpTimeout(p *RConnectionPool, args []object.Value) time.Duration {
	if h := cpOptions(args); h != nil {
		if v, ok := h.Get(object.Symbol("timeout")); ok {
			return cpDuration(v)
		}
	}
	return p.timeout
}

// cpCheckout borrows a connection for key, blocking up to timeout. It first tries
// a non-blocking checkout under the GVL (the only path on which the factory block
// runs, so it must hold the GVL); on exhaustion it releases the GVL and parks for
// the remaining timeout, a path the library never runs the factory on. It raises
// ConnectionPool::TimeoutError on exhaustion and ConnectionPool::PoolShuttingDownError
// after shutdown.
func (vm *VM) cpCheckout(p *RConnectionPool, key cp.CallerKey, timeout time.Duration) object.Value {
	conn, err := p.pool.Checkout(key, 0)
	if err == nil {
		return conn.(object.Value)
	}
	if _, ok := err.(*cp.TimeoutError); ok && timeout > 0 {
		vm.threadBlock(func() { conn, err = p.pool.Checkout(key, timeout) })
		if err == nil {
			return conn.(object.Value)
		}
	}
	return vm.raiseCPError(err)
}

// cpDispatch is the Wrapper's Dispatch seam: it invokes method name on a borrowed
// connection through Ruby's send, so a Wrapper behaves like the connection itself.
func (vm *VM) cpDispatch(conn any, name string, cargs ...any) any {
	rargs := make([]object.Value, len(cargs))
	for i, a := range cargs {
		rargs[i] = a.(object.Value)
	}
	return vm.send(conn.(object.Value), name, rargs, nil)
}

// raiseCPError re-raises a library pool error as the matching Ruby exception:
// *cp.TimeoutError → ConnectionPool::TimeoutError, *cp.PoolShuttingDownError →
// ConnectionPool::PoolShuttingDownError, and every other *cp.Error →
// ConnectionPool::Error.
func (vm *VM) raiseCPError(err error) object.Value {
	switch err.(type) {
	case *cp.TimeoutError:
		return raise("ConnectionPool::TimeoutError", "%s", err.Error())
	case *cp.PoolShuttingDownError:
		return raise("ConnectionPool::PoolShuttingDownError", "%s", err.Error())
	default:
		return raise("ConnectionPool::Error", "%s", err.Error())
	}
}
