// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"sync"

	libpstore "github.com/go-ruby-pstore/pstore"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-marshal/marshal"
)

// PStore binds github.com/go-ruby-pstore/pstore — the pure-Go, MRI-4.0.5-faithful
// port of Ruby's `pstore` standard library — into rbgo (require "pstore"). The
// library owns the transaction state machine (load → run body → commit/abort, the
// read-only write guard, the not-in-transaction / read-only / nested guards, the
// commit-only-on-change Marshal round-trip) over two host-injected seams:
//
//   - the file Backend (Load/Store bytes): this file wires a real os.File opened
//     O_CREAT|O_RDWR with an advisory flock (LOCK_SH for a read-only transaction,
//     LOCK_EX otherwise) and a read-all / atomic write — the IO MRI's PStore does.
//   - the go-ruby-marshal codec: the table's keys/values are marshal.Values, the
//     very model rbgo already speaks for Marshal.dump/load. This file reuses the
//     same toMarshalValue / fromMarshalValue converters (see marshal.go), so a
//     PStore written by rbgo holds genuine Ruby objects and is byte-compatible with
//     a file MRI's PStore wrote, and vice versa.
//
// The block-driven control flow is the host's concern: MRI's PStore#commit /
// #abort throw to exit the transaction block, so this file runs the Ruby block
// under a recover that turns a pstoreSignal panic (raised by #commit / #abort)
// into the library's Commit / Abort sentinel, exactly as Kernel#catch/#throw is
// modelled — snapshotting the frame-tracking stacks so an early exit through the
// abandoned block frames leaves __FILE__ / caller / backtraces consistent.

// PStore is the Ruby wrapper around a go-ruby-pstore Store. It is itself the Ruby
// object (like Set), holding its path, the library Store and — only while a
// transaction runs — the active Tx and a mutex for the thread_safe form.
type PStore struct {
	path       string
	threadSafe bool
	store      *libpstore.Store
	mu         sync.Mutex    // serialises transactions when thread_safe is true
	tx         *libpstore.Tx // the in-flight transaction handle (nil outside one)
}

func (p *PStore) ToS() string     { return "#<PStore>" }
func (p *PStore) Inspect() string { return "#<PStore>" }
func (p *PStore) Truthy() bool    { return true }

// pstoreSignal is the panic raised by PStore#commit / #abort to exit a transaction
// block, recovered in transaction and translated to the library's Commit / Abort
// sentinel — the analogue of MRI throwing :pstore_abort_transaction.
type pstoreSignal struct{ abort bool }

// fileBackend implements libpstore.Backend over a real file. Load reads the whole
// file (O_CREAT, so a missing store reads as the empty table); Store truncates and
// rewrites it. flock is taken around the whole transaction by transaction(), not
// here, matching MRI's lock-then-read-then-write order.
type fileBackend struct{ path string }

// Load returns the store file's bytes, creating an empty file if it does not yet
// exist (MRI treats a fresh / empty file as the empty table {}).
func (b *fileBackend) Load() ([]byte, error) {
	data, err := os.ReadFile(b.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// tmpFile is the slice of os.File the atomic-write path uses; it is an interface so
// a test can inject a temp file whose Write / Close fail to exercise those branches
// (the standard rbgo fault-injection seam — flock and a full disk cannot be forced
// portably from a unit test).
type tmpFile interface {
	Write([]byte) (int, error)
	Close() error
	Name() string
}

// createTemp opens a temp file beside the store (overridable in tests). It returns a
// tmpFile so the Write / Close error branches of Store are reachable under a fault.
var createTemp = func(dir string) (tmpFile, error) { return os.CreateTemp(dir, ".pstore.tmp") }

// renameFile atomically moves the temp file onto the store path (overridable in
// tests; defaults to os.Rename).
var renameFile = os.Rename

// Store overwrites the store file with data via a temp file + atomic rename, the
// crash-safe save MRI's PStore uses (so a concurrent reader never sees a partial
// write). The temp file sits beside the target so the rename stays on one device.
func (b *fileBackend) Store(data []byte) error {
	tmp, err := createTemp(dirOf(b.path))
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return renameFile(name, b.path)
}

// dirOf returns the directory of path (".", not "", for a bare filename) so the
// temp file lands on the same filesystem as the store for an atomic rename.
// filepath.Dir handles both separators, so a Windows backslash path resolves to
// its real directory (not "." / the cwd, which would make the rename cross-drive).
func dirOf(path string) string { return filepath.Dir(path) }

// pstoreSelf asserts the receiver is a PStore (always true for a bound method).
func pstoreSelf(v object.Value) *PStore { return object.Kind[*PStore](v) }

// pstoreKey converts a Ruby key/value to the marshal model the library stores,
// reusing the Marshal binding so PStore holds genuine Ruby objects (and the bytes
// match MRI's Marshal.dump of the same object).
func pstoreKey(v object.Value) marshal.Value {
	return toMarshalValue(v, map[object.Value]marshal.Value{})
}

// pstoreVal converts a stored marshal value back to a Ruby value (the inverse of
// pstoreKey), again through the shared Marshal binding.
func pstoreVal(v marshal.Value) object.Value {
	if v == nil {
		return object.NilV
	}
	return fromMarshalValue(v, map[marshal.Value]object.Value{})
}

// raisePStore re-raises a library error as PStore::Error with MRI's verbatim
// message (the library's *Error carries exactly MRI's text). It never returns when
// err is non-nil.
func raisePStore(err error) {
	if err == nil {
		return
	}
	if _, ok := err.(*libpstore.Error); ok {
		raise("PStore::Error", "%s", err.Error())
		return
	}
	// A non-PStore error is the file Backend failing (an IO error); surface it as
	// PStore::Error too — MRI lets the underlying IOError/Errno propagate, but the
	// host's Backend only ever returns plain os errors here, and PStore::Error is
	// the closest faithful surface for them.
	raise("PStore::Error", "%s", err.Error())
}

// registerPStore installs the PStore class (require "pstore"). It runs after the
// prelude so PStore::Error < StandardError is in place. The class is itself the
// Ruby object; its methods dispatch on the active library Tx.
func (vm *VM) registerPStore() {
	// PStore::Error < StandardError (MRI's hierarchy), registered under its
	// qualified top-level name and re-attached as a nested PStore constant.
	std := object.Kind[*RClass](vm.consts["StandardError"])
	vm.cPStoreError = newClass("PStore::Error", std)
	vm.consts["PStore::Error"] = vm.cPStoreError

	cls := newClass("PStore", vm.cObject)
	vm.cPStore = cls
	vm.consts["PStore"] = cls
	cls.consts["Error"] = vm.cPStoreError

	// PStore.new(file, thread_safe=false): a store over file. The file is not
	// opened until a transaction runs (MRI opens it lazily too).
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			s, ok := object.KindOK[*object.String](args[0])
			if !ok {
				raise("TypeError", "no implicit conversion into String")
			}
			path := string(s.Bytes())
			ts := false
			if len(args) > 1 {
				ts = args[1].Truthy()
			}
			return &PStore{
				path:       path,
				threadSafe: ts,
				store:      libpstore.New(&fileBackend{path: path}),
			}
		}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("path", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(pstoreSelf(v).path)
	})

	d("transaction", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "no block given")
		}
		p := pstoreSelf(v)
		readOnly := len(args) > 0 && args[0].Truthy()
		return p.transaction(vm, readOnly, blk)
	})

	// In-transaction accessors. Each guards via the library (which raises the
	// not-in-transaction / read-only PStore::Error), so calling them outside a
	// transaction reproduces MRI's "not in transaction" error.
	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		p := pstoreSelf(v)
		val, _, err := p.curTx().Get(pstoreKey(args[0]))
		raisePStore(err)
		return pstoreVal(val)
	})
	d("[]=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		p := pstoreSelf(v)
		raisePStore(p.curTx().Set(pstoreKey(args[0]), pstoreKey(args[1])))
		return args[1]
	})
	d("delete", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		p := pstoreSelf(v)
		old, err := p.curTx().Delete(pstoreKey(args[0]))
		raisePStore(err)
		return pstoreVal(old)
	})
	d("fetch", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		p := pstoreSelf(v)
		var def marshal.Value // nil => MRI's no-default fetch (a miss raises)
		if len(args) > 1 {
			def = pstoreKey(args[1])
		}
		val, err := p.curTx().Fetch(pstoreKey(args[0]), def)
		raisePStore(err)
		return pstoreVal(val)
	})
	d("roots", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		p := pstoreSelf(v)
		ks, err := p.curTx().Roots()
		raisePStore(err)
		out := make([]object.Value, len(ks))
		for i, k := range ks {
			out[i] = pstoreVal(k)
		}
		return object.NewArrayFromSlice(out)
	})
	rootQ := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		p := pstoreSelf(v)
		ok, err := p.curTx().RootQ(pstoreKey(args[0]))
		raisePStore(err)
		return object.Bool(ok)
	}
	d("root?", rootQ)
	d("key?", rootQ)

	d("commit", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		// Guard outside a transaction (MRI's #commit raises "not in transaction"),
		// then unwind the block.
		pstoreSelf(v).curTx().inTx()
		panic(pstoreSignal{abort: false})
	})
	d("abort", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		pstoreSelf(v).curTx().inTx()
		panic(pstoreSignal{abort: true})
	})
}

// curTx returns the active transaction handle wrapped in a txGuard, which
// reproduces MRI's "not in transaction" PStore::Error for any accessor reached
// with no transaction running (when p.tx is nil the library handle does not exist
// yet, so the guard supplies the error the library would otherwise raise).
func (p *PStore) curTx() *txGuard {
	return &txGuard{tx: p.tx}
}

// txGuard wraps the library Tx (possibly nil) so each accessor reproduces MRI's
// "not in transaction" error uniformly, even when no transaction has begun and the
// library handle is therefore nil. With a live Tx every method delegates straight
// to the library (whose own guards add the read-only / undefined-key cases).
type txGuard struct{ tx *libpstore.Tx }

// inTx raises MRI's "not in transaction" PStore::Error (the message the library
// itself uses) when no transaction is active, and returns otherwise. It never
// returns when g.tx is nil. The library has no exported *Error constructor, so the
// host raises the Ruby exception directly here, with MRI's verbatim message.
func (g *txGuard) inTx() {
	if g.tx == nil {
		raise("PStore::Error", "not in transaction")
	}
}

func (g *txGuard) Get(k marshal.Value) (marshal.Value, bool, error) {
	g.inTx()
	return g.tx.Get(k)
}
func (g *txGuard) Set(k, val marshal.Value) error {
	g.inTx()
	return g.tx.Set(k, val)
}
func (g *txGuard) Delete(k marshal.Value) (marshal.Value, error) {
	g.inTx()
	return g.tx.Delete(k)
}
func (g *txGuard) Fetch(k, def marshal.Value) (marshal.Value, error) {
	g.inTx()
	return g.tx.Fetch(k, def)
}
func (g *txGuard) Roots() ([]marshal.Value, error) {
	g.inTx()
	return g.tx.Roots()
}
func (g *txGuard) RootQ(k marshal.Value) (bool, error) {
	g.inTx()
	return g.tx.RootQ(k)
}

// transaction runs blk as a PStore transaction. It takes an advisory flock around
// the whole transaction (LOCK_SH read-only, LOCK_EX read-write), drives the library
// state machine, and translates a #commit / #abort panic into the library's
// Commit / Abort sentinel so the block exits early exactly as in MRI.
func (p *PStore) transaction(vm *VM, readOnly bool, blk *Proc) object.Value {
	// A transaction already in flight on this store is MRI's "nested transaction"
	// PStore::Error. This is detected up front — before the flock — because the
	// advisory lock is taken per fd: a nested transaction would open a second fd on
	// the same file and block forever on LOCK_EX (a self-deadlock) before ever
	// reaching the library's own nested-transaction guard. The library raises this
	// exact message (its Transaction returns it before running any body); raising it
	// directly here reproduces MRI without touching the file or the lock.
	if p.tx != nil {
		raise("PStore::Error", "nested transaction")
	}

	if p.threadSafe {
		p.mu.Lock()
		defer p.mu.Unlock()
	}

	unlock, err := flockFile(p.path, readOnly)
	if err != nil {
		raisePStore(err)
	}
	defer unlock()

	// Snapshot the frame-tracking stacks so a #commit / #abort that unwinds the
	// block's frames straight to the recover below (bypassing each frame's normal
	// pop) leaves __FILE__ / caller / backtraces consistent — the Kernel#catch idiom.
	fileStackDepth := len(vm.fileStack)
	frameNamesDepth := len(vm.frameNames)
	frameFilesDepth := len(vm.frameFiles)
	requireDirsDepth := len(vm.requireDirs)

	body := func(t *libpstore.Tx) (bodyErr error) {
		p.tx = t
		defer func() {
			p.tx = nil
			if r := recover(); r != nil {
				if sig, ok := r.(pstoreSignal); ok {
					vm.fileStack = vm.fileStack[:fileStackDepth]
					vm.frameNames = vm.frameNames[:frameNamesDepth]
					vm.frameFiles = vm.frameFiles[:frameFilesDepth]
					vm.requireDirs = vm.requireDirs[:requireDirsDepth]
					if sig.abort {
						bodyErr = t.Abort()
					} else {
						bodyErr = t.Commit()
					}
					return
				}
				panic(r)
			}
		}()
		vm.callBlock(blk, []object.Value{p})
		return nil
	}

	raisePStore(p.store.Transaction(readOnly, body))
	return object.NilV
}

// flockFile takes the per-transaction advisory file lock, returning a closure that
// releases it. It is defined per platform (pstore_lock_unix.go / _windows.go): on
// Unix it opens the store O_CREAT|O_RDWR and holds a real flock(2) for the
// transaction; on Windows — which has no advisory flock — it opens NOTHING and is a
// pure no-op, so the atomic-rename Store is not blocked by a still-open target
// handle (Windows forbids renaming over an open file). The lock only serialises
// concurrent transactions, as MRI's does.
