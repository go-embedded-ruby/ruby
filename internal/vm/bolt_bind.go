// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"os"
	"time"

	bbolt "github.com/go-ruby-bbolt/bbolt"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent key/value store of github.com/go-ruby-bbolt/bbolt (a
// pure-Go, no-cgo façade over go.etcd.io/bbolt, the reference embedded B+tree
// store). It carries the four instance value types Bolt wraps — an open
// database, a transaction, a bucket and a cursor — plus the byte conversions
// that move Ruby String keys and values in and out of the store and the error
// bridge that re-raises the library's Bolt::Error sentinel tree as the matching
// Ruby exceptions. All storage is delegated to go-ruby-bbolt; a file rbgo writes
// is read by the reference bbolt tooling and vice-versa. See bolt.go for the
// module wiring.

// BoltDB is an instance of Bolt::DB: an open, memory-mapped Bolt database backed
// by a go-ruby-bbolt *bbolt.DB. It hands out read-write (#update) and read-only
// (#view) block transactions and explicit ones (#begin).
type BoltDB struct {
	cls *RClass
	db  *bbolt.DB
}

func (d *BoltDB) ToS() string     { return "#<Bolt::DB>" }
func (d *BoltDB) Inspect() string { return "#<Bolt::DB>" }
func (d *BoltDB) Truthy() bool    { return true }

// BoltTx is an instance of Bolt::Tx: a Bolt transaction backed by a go-ruby-bbolt
// *bbolt.Tx. A writable transaction may create, delete and mutate buckets; a
// read-only one may only read. A Tx yielded to an #update / #view block is only
// valid for the duration of that block.
type BoltTx struct {
	cls *RClass
	tx  *bbolt.Tx
}

func (t *BoltTx) ToS() string     { return "#<Bolt::Tx>" }
func (t *BoltTx) Inspect() string { return "#<Bolt::Tx>" }
func (t *BoltTx) Truthy() bool    { return true }

// BoltBucket is an instance of Bolt::Bucket: a named, ordered collection of
// key/value pairs backed by a go-ruby-bbolt *bbolt.Bucket, which may also hold
// nested sub-buckets.
type BoltBucket struct {
	cls *RClass
	b   *bbolt.Bucket
}

func (b *BoltBucket) ToS() string     { return "#<Bolt::Bucket>" }
func (b *BoltBucket) Inspect() string { return "#<Bolt::Bucket>" }
func (b *BoltBucket) Truthy() bool    { return true }

// BoltCursor is an instance of Bolt::Cursor: a cursor over a bucket's keys in
// byte order, backed by a go-ruby-bbolt *bbolt.Cursor.
type BoltCursor struct {
	cls *RClass
	c   *bbolt.Cursor
}

func (c *BoltCursor) ToS() string     { return "#<Bolt::Cursor>" }
func (c *BoltCursor) Inspect() string { return "#<Bolt::Cursor>" }
func (c *BoltCursor) Truthy() bool    { return true }

// raiseBoltError re-raises a go-ruby-bbolt error as the matching Ruby exception.
// A *bbolt.Error carries the Bolt::Error subclass its sentinel maps to (e.g.
// Bolt::BucketNotFound); any other error — including one a user's #update block
// returned or raised to force a rollback — raises the Bolt::Error base. It never
// returns (raise panics).
func raiseBoltError(err error) {
	var be *bbolt.Error
	if errors.As(err, &be) {
		raise(be.Class, "%s", be.Error())
	}
	raise("Bolt::Error", "%s", err.Error())
}

// boltBytes coerces a key / value / bucket-name argument to its bytes: a String
// yields its contents, and any other value raises TypeError, matching how a
// Bolt store only stores opaque byte strings.
func boltBytes(v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	raise("TypeError", "no implicit conversion of %s into String", v.Inspect())
	return nil
}

// boltValue maps a value read from the store back into the object graph: a nil
// slice (an absent key, or a key that names a nested bucket) becomes nil, and any
// present value a copied ASCII-8BIT String, since stored values are opaque bytes
// and the source slice aliases the memory-mapped file only for the life of the
// transaction.
func boltValue(b []byte) object.Value {
	if b == nil {
		return object.NilV
	}
	return object.NewStringBytesEnc(append([]byte(nil), b...), "ASCII-8BIT")
}

// boltPair renders a cursor position (key, value) as a two-element Ruby Array, or
// nil when the key is nil (the cursor has moved past the end or before the start
// of the bucket).
func boltPair(key, value []byte) object.Value {
	if key == nil {
		return object.NilV
	}
	return object.NewArrayFromSlice([]object.Value{boltValue(key), boltValue(value)})
}

// boltNames maps a list of bucket names to a Ruby Array of ASCII-8BIT Strings.
// The library already returns caller-owned copies, so each name is wrapped
// directly.
func boltNames(names [][]byte) *object.Array {
	out := object.NewArrayFromSlice(make([]object.Value, len(names)))
	for i, n := range names {
		out.Elems[i] = object.NewStringBytesEnc(n, "ASCII-8BIT")
	}
	return out
}

// boltOptions builds a *bbolt.Options from an optional trailing Ruby keyword
// Hash: mode: (an Integer file mode), read_only:, nogrowsync:, nofreelistsync:
// (booleans) and timeout: (a Float or Integer number of seconds the open waits
// for the file lock). A nil / absent Hash yields nil, opening a read-write
// database with the library's default mode.
func boltOptions(h *object.Hash) *bbolt.Options {
	if h == nil {
		return nil
	}
	opts := &bbolt.Options{}
	if v, ok := h.Get(object.Symbol("mode")); ok {
		opts.Mode = os.FileMode(intArg(v))
	}
	if v, ok := h.Get(object.Symbol("read_only")); ok {
		opts.ReadOnly = v.Truthy()
	}
	if v, ok := h.Get(object.Symbol("nogrowsync")); ok {
		opts.NoGrowSync = v.Truthy()
	}
	if v, ok := h.Get(object.Symbol("nofreelistsync")); ok {
		opts.NoFreelistSync = v.Truthy()
	}
	if v, ok := h.Get(object.Symbol("timeout")); ok {
		opts.Timeout = time.Duration(boltSeconds(v) * float64(time.Second))
	}
	return opts
}

// boltSeconds reads a timeout: option as a number of seconds, accepting an
// Integer or a Float and raising TypeError for anything else.
func boltSeconds(v object.Value) float64 {
	switch n := v.(type) {
	case object.Integer:
		return float64(n)
	case object.Float:
		return float64(n)
	}
	raise("TypeError", "no implicit conversion of %s into Numeric", v.Inspect())
	return 0
}
