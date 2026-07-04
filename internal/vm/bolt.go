// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	bbolt "github.com/go-ruby-bbolt/bbolt"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerBolt installs the Bolt module (require "bolt"): the Bolt::DB embedded
// key/value store and its Tx, Bucket and Cursor value types, plus the
// Bolt::Error exception tree. All storage is delegated to
// github.com/go-ruby-bbolt/bbolt — a pure-Go, no-cgo façade over go.etcd.io/bbolt,
// the reference embedded B+tree store — so a file rbgo writes is read by the
// reference bbolt tooling and vice-versa. The instance value types and the byte
// conversions live in bolt_bind.go.
func (vm *VM) registerBolt() {
	mod := newClass("Bolt", nil)
	mod.isModule = true
	vm.consts["Bolt"] = mod

	vm.registerBoltErrors(mod)
	cursor := vm.registerBoltCursor(mod)
	bucket := vm.registerBoltBucket(mod, cursor)
	tx := vm.registerBoltTx(mod, bucket)
	vm.registerBoltDB(mod, tx)
}

// registerBoltErrors installs the Bolt::Error exception tree (Error <
// StandardError; every store failure < Error). Each class is registered both as a
// nested constant of Bolt (so Ruby Bolt::BucketNotFound resolves it) and under
// its qualified name in the top-level table (so a re-raised library sentinel's
// exception lookup finds the same class), exactly as the Age and SQLite3 trees
// are. The specific classes are named after the library's own *bbolt.Error
// classes, so the mapping in raiseBoltError can never name a class that is not
// registered here.
func (vm *VM) registerBoltErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(qualified string, super *RClass) *RClass {
		simple := qualified[len("Bolt::"):]
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("Bolt::Error", std)
	for _, e := range []*bbolt.Error{
		bbolt.ErrDatabaseNotOpen, bbolt.ErrInvalid, bbolt.ErrInvalidMapping,
		bbolt.ErrVersionMismatch, bbolt.ErrChecksum, bbolt.ErrTimeout,
		bbolt.ErrTxNotWritable, bbolt.ErrTxClosed, bbolt.ErrDatabaseReadOnly,
		bbolt.ErrFreePagesNotLoaded, bbolt.ErrBucketNotFound, bbolt.ErrBucketExists,
		bbolt.ErrBucketNameRequired, bbolt.ErrKeyRequired, bbolt.ErrKeyTooLarge,
		bbolt.ErrValueTooLarge, bbolt.ErrIncompatibleValue,
	} {
		reg(e.Class, base)
	}
}

// registerBoltDB installs Bolt::DB and its instance methods, returning nothing;
// tx is the Bolt::Tx class used to wrap the transactions yielded to #update /
// #view and returned by #begin.
func (vm *VM) registerBoltDB(mod *RClass, tx *RClass) {
	cls := newClass("Bolt::DB", vm.cObject)
	mod.consts["DB"] = cls
	vm.consts["Bolt::DB"] = cls

	// Bolt::DB.open(path, **opts) / .new(path, **opts): open (creating if
	// necessary) the database at path. opts may carry mode:, read_only:, timeout:,
	// nogrowsync: and nofreelistsync:. A block form yields the database and closes
	// it afterwards, returning the block's value.
	open := func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		path := strArg(args[0])
		db, err := bbolt.Open(path, boltOptions(boltOptsHash(args[1:])))
		if err != nil {
			raiseBoltError(err)
		}
		wrap := &BoltDB{cls: cls, db: db}
		if blk != nil {
			defer func() { _ = db.Close() }()
			return vm.callBlock(blk, []object.Value{wrap})
		}
		return wrap
	}
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: open}
	cls.smethods["open"] = &Method{name: "open", owner: cls, native: open}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *bbolt.DB { return v.(*BoltDB).db }

	// #update { |tx| ... } runs the block inside a read-write transaction,
	// committing on normal completion and rolling back if the block raises (or
	// returns), and returns the block's value.
	d("update", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		var result object.Value = object.NilV
		err := self(v).Update(func(btx *bbolt.Tx) error {
			result = vm.callBlock(blk, []object.Value{&BoltTx{cls: tx, tx: btx}})
			return nil
		})
		if err != nil {
			raiseBoltError(err)
		}
		return result
	})

	// #view { |tx| ... } runs the block inside a read-only transaction and returns
	// the block's value. Writes attempted inside the block raise Bolt::TxNotWritable.
	d("view", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		var result object.Value = object.NilV
		err := self(v).View(func(btx *bbolt.Tx) error {
			result = vm.callBlock(blk, []object.Value{&BoltTx{cls: tx, tx: btx}})
			return nil
		})
		if err != nil {
			raiseBoltError(err)
		}
		return result
	})

	// #begin(writable = false) starts an explicit transaction the caller must
	// #commit or #rollback. Only one writable transaction may be open at a time.
	d("begin", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		writable := len(args) > 0 && args[0].Truthy()
		btx, err := self(v).Begin(writable)
		if err != nil {
			raiseBoltError(err)
		}
		return &BoltTx{cls: tx, tx: btx}
	})

	// #path returns the database file path.
	d("path", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Path())
	})

	// #read_only? reports whether the database was opened read-only.
	d("read_only?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).IsReadOnly())
	})

	// #stats returns a Hash snapshot of database-level counters.
	d("stats", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self(v).Stats()
		h := object.NewHash()
		h.Set(object.Symbol("tx_n"), object.IntValue(int64(s.TxN)))
		h.Set(object.Symbol("open_tx_n"), object.IntValue(int64(s.OpenTxN)))
		h.Set(object.Symbol("free_page_n"), object.IntValue(int64(s.FreePageN)))
		h.Set(object.Symbol("pending_page_n"), object.IntValue(int64(s.PendingPageN)))
		h.Set(object.Symbol("free_alloc"), object.IntValue(int64(s.FreeAlloc)))
		h.Set(object.Symbol("freelist_inuse"), object.IntValue(int64(s.FreelistInuse)))
		return h
	})

	// #close releases all resources and unmaps the file. A close error can only
	// come from an OS-level unmap/file-close fault (a second #close on an
	// already-closed database is a harmless no-op), so — as the SQLite3 binding
	// does with its handle — the result is discarded rather than raised.
	d("close", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		_ = self(v).Close()
		return object.NilV
	})
}

// registerBoltTx installs Bolt::Tx and its instance methods, returning the class;
// bucket is the Bolt::Bucket class used to wrap the buckets it hands out.
func (vm *VM) registerBoltTx(mod *RClass, bucket *RClass) *RClass {
	cls := newClass("Bolt::Tx", vm.cObject)
	mod.consts["Tx"] = cls
	vm.consts["Bolt::Tx"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *bbolt.Tx { return v.(*BoltTx).tx }
	wrapBucket := func(b *bbolt.Bucket) object.Value {
		if b == nil {
			return object.NilV
		}
		return &BoltBucket{cls: bucket, b: b}
	}

	// #bucket(name) returns the top-level bucket, or nil if it does not exist.
	d("bucket", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return wrapBucket(self(v).Bucket(boltBytes(boltArg(args))))
	})

	// #create_bucket(name) creates a new top-level bucket, raising Bolt::BucketExists
	// if it already exists.
	d("create_bucket", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		b, err := self(v).CreateBucket(boltBytes(boltArg(args)))
		if err != nil {
			raiseBoltError(err)
		}
		return wrapBucket(b)
	})

	// #create_bucket_if_not_exists(name) creates a top-level bucket unless it
	// already exists, returning it either way.
	d("create_bucket_if_not_exists", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		b, err := self(v).CreateBucketIfNotExists(boltBytes(boltArg(args)))
		if err != nil {
			raiseBoltError(err)
		}
		return wrapBucket(b)
	})

	// #delete_bucket(name) removes a top-level bucket, raising Bolt::BucketNotFound
	// if it does not exist.
	d("delete_bucket", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self(v).DeleteBucket(boltBytes(boltArg(args))); err != nil {
			raiseBoltError(err)
		}
		return object.NilV
	})

	// #buckets returns the names of all top-level buckets, in byte order.
	d("buckets", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return boltNames(self(v).Buckets())
	})

	// #writable? reports whether the transaction can perform writes.
	d("writable?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Writable())
	})

	// #id returns the transaction id.
	d("id", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).ID()))
	})

	// #commit writes all changes and closes the transaction. When the commit is
	// rejected outright — a read-only or already-closed transaction — bbolt returns
	// the error without releasing the transaction, so it would keep its read lock on
	// the memory map and deadlock the next write that has to grow the file. Roll it
	// back first (a no-op / harmless ErrTxClosed on an already-closed tx) so the lock
	// is always freed, then raise.
	d("commit", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		btx := self(v)
		if err := btx.Commit(); err != nil {
			_ = btx.Rollback()
			raiseBoltError(err)
		}
		return object.NilV
	})

	// #rollback discards all changes and closes the transaction.
	d("rollback", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Rollback(); err != nil {
			raiseBoltError(err)
		}
		return object.NilV
	})

	return cls
}

// registerBoltBucket installs Bolt::Bucket and its instance methods, returning
// the class; cursor is the Bolt::Cursor class used to wrap the cursors it hands
// out.
func (vm *VM) registerBoltBucket(mod *RClass, cursor *RClass) *RClass {
	cls := newClass("Bolt::Bucket", vm.cObject)
	mod.consts["Bucket"] = cls
	vm.consts["Bolt::Bucket"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *bbolt.Bucket { return v.(*BoltBucket).b }
	wrapBucket := func(b *bbolt.Bucket) object.Value {
		if b == nil {
			return object.NilV
		}
		return &BoltBucket{cls: cls, b: b}
	}

	// #put(key, value) stores value under key. An empty key raises Bolt::KeyRequired;
	// a read-only transaction raises Bolt::TxNotWritable.
	d("put", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		if err := self(v).Put(boltBytes(args[0]), boltBytes(args[1])); err != nil {
			raiseBoltError(err)
		}
		return args[1]
	})

	// #get(key) returns the value stored under key, or nil if the key is absent (or
	// names a nested bucket).
	d("get", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return boltValue(self(v).Get(boltBytes(boltArg(args))))
	})

	// #[](key) is an alias for #get.
	d("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return boltValue(self(v).Get(boltBytes(boltArg(args))))
	})

	// #delete(key) removes key from the bucket, if present.
	d("delete", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self(v).Delete(boltBytes(boltArg(args))); err != nil {
			raiseBoltError(err)
		}
		return object.NilV
	})

	// #each { |key, value| ... } iterates every key/value pair in byte order. A key
	// that names a nested bucket is yielded with a nil value.
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		err := self(v).Each(func(k, val []byte) error {
			vm.callBlock(blk, []object.Value{boltValue(k), boltValue(val)})
			return nil
		})
		if err != nil {
			raiseBoltError(err)
		}
		return v
	})

	// #cursor returns a Bolt::Cursor over the bucket.
	d("cursor", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &BoltCursor{cls: cursor, c: self(v).Cursor()}
	})

	// #bucket(name) returns the nested sub-bucket, or nil if it does not exist.
	d("bucket", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return wrapBucket(self(v).Bucket(boltBytes(boltArg(args))))
	})

	// #create_bucket(name) creates a nested sub-bucket, raising Bolt::BucketExists
	// if it already exists.
	d("create_bucket", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		b, err := self(v).CreateBucket(boltBytes(boltArg(args)))
		if err != nil {
			raiseBoltError(err)
		}
		return wrapBucket(b)
	})

	// #create_bucket_if_not_exists(name) creates a nested sub-bucket unless it
	// already exists, returning it either way.
	d("create_bucket_if_not_exists", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		b, err := self(v).CreateBucketIfNotExists(boltBytes(boltArg(args)))
		if err != nil {
			raiseBoltError(err)
		}
		return wrapBucket(b)
	})

	// #delete_bucket(name) removes a nested sub-bucket, raising Bolt::BucketNotFound
	// if it does not exist.
	d("delete_bucket", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self(v).DeleteBucket(boltBytes(boltArg(args))); err != nil {
			raiseBoltError(err)
		}
		return object.NilV
	})

	// #buckets returns the names of the immediate nested sub-buckets, in byte order.
	d("buckets", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return boltNames(self(v).Buckets())
	})

	// #writable? reports whether the bucket belongs to a writable transaction.
	d("writable?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Writable())
	})

	// #sequence returns the bucket's current monotonic sequence counter.
	d("sequence", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Sequence()))
	})

	// #next_sequence returns a new, auto-incrementing sequence value unique within
	// the bucket.
	d("next_sequence", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		n, err := self(v).NextSequence()
		if err != nil {
			raiseBoltError(err)
		}
		return object.IntValue(int64(n))
	})

	return cls
}

// registerBoltCursor installs Bolt::Cursor and its movement methods, returning
// the class. Each movement returns a [key, value] Array, or nil when the cursor
// has moved past the end (or before the start) of the bucket.
func (vm *VM) registerBoltCursor(mod *RClass) *RClass {
	cls := newClass("Bolt::Cursor", vm.cObject)
	mod.consts["Cursor"] = cls
	vm.consts["Bolt::Cursor"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *bbolt.Cursor { return v.(*BoltCursor).c }

	d("first", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return boltPair(self(v).First())
	})
	d("last", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return boltPair(self(v).Last())
	})
	d("next", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return boltPair(self(v).Next())
	})
	d("prev", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return boltPair(self(v).Prev())
	})

	// #seek(key) moves to the first key at or after key, returning that pair or nil.
	d("seek", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return boltPair(self(v).Seek(boltBytes(boltArg(args))))
	})

	// #delete removes the key/value pair the cursor currently points at.
	d("delete", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Delete(); err != nil {
			raiseBoltError(err)
		}
		return object.NilV
	})

	return cls
}

// boltArg returns the single required argument of a name/key method, raising
// ArgumentError when none was given.
func boltArg(args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return args[0]
}

// boltOptsHash returns the trailing keyword Hash of Bolt::DB.open (the mode: /
// read_only: / timeout: options), or nil when the last argument is not a Hash.
func boltOptsHash(rest []object.Value) *object.Hash {
	if len(rest) == 0 {
		return nil
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	if !ok {
		return nil
	}
	return h
}
