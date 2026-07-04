// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"context"
	"errors"
	"time"

	etcd "github.com/go-ruby-etcd/etcd"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent etcd v3 client of github.com/go-ruby-etcd/etcd — a
// pure-Go (CGO=0), etcdv3-gem-flavoured surface over the official
// go.etcd.io/etcd/client/v3 transport. It carries the value types the Etcd
// module wraps — an open Client, its key/value results, a Lease, a Watch event,
// the transaction builder and its result, a held Lock and the cluster
// Member/Status views — plus the argument coercions and the error bridge that
// re-raises the library's *etcd.Error status tree as the matching Ruby
// exceptions. All protocol work is delegated to go-ruby-etcd; see etcd.go for
// the module and method wiring.

// etcdDialTimeout bounds a Client's initial connection when the constructor does
// not override it, and etcdOpTimeout bounds a single request/reply operation, so
// a call against an unreachable cluster fails as a clean Ruby exception rather
// than hanging the single-threaded VM.
const (
	etcdDialTimeout = 5 * time.Second
	etcdOpTimeout   = 5 * time.Second
)

// etcdClasses holds every Ruby class the Etcd binding constructs, resolved once
// at registration and threaded through the value wrappers so a result built deep
// in a bridge (a watch Event, a transaction reply) reports the right class
// without a consts lookup. The Etcd and Etcdv3 modules share these classes.
type etcdClasses struct {
	client, keyValue, getResult, putResult, delResult *RClass
	lease, event, txn, cmp, op, txnResult             *RClass
	lock, member, status                              *RClass
}

// EtcdClient is an instance of Etcd::Client (Etcdv3): a connection bound to an
// etcd v3 cluster through a go-ruby-etcd *etcd.Client. It builds requests, drives
// them over the client and maps the replies back into the object graph.
type EtcdClient struct {
	cls       *RClass
	c         *etcd.Client
	endpoints []string
	opTimeout time.Duration
	cl        *etcdClasses
}

func (c *EtcdClient) ToS() string     { return "#<Etcd::Client>" }
func (c *EtcdClient) Inspect() string { return "#<Etcd::Client>" }
func (c *EtcdClient) Truthy() bool    { return true }

// EtcdKeyValue is an instance of Etcd::KeyValue: one key/value pair and its
// revision metadata, mirroring etcd's mvccpb.KeyValue.
type EtcdKeyValue struct {
	cls *RClass
	kv  etcd.KeyValue
}

func (k *EtcdKeyValue) ToS() string { return "#<Etcd::KeyValue " + k.kv.Key + ">" }
func (k *EtcdKeyValue) Inspect() string {
	return "#<Etcd::KeyValue " + k.kv.Key + "=" + k.kv.Value + ">"
}
func (k *EtcdKeyValue) Truthy() bool { return true }

// EtcdGetResult is an instance of Etcd::GetResult: the key-values a #get matched
// plus the range metadata (count, more), mirroring the gem's range response.
type EtcdGetResult struct {
	cls *RClass
	res *etcd.GetResult
}

func (r *EtcdGetResult) ToS() string     { return "#<Etcd::GetResult>" }
func (r *EtcdGetResult) Inspect() string { return "#<Etcd::GetResult>" }
func (r *EtcdGetResult) Truthy() bool    { return true }

// EtcdPutResult is an instance of Etcd::PutResult: a #put's store revision and,
// when requested, the previous value at the key.
type EtcdPutResult struct {
	cls *RClass
	res *etcd.PutResult
}

func (r *EtcdPutResult) ToS() string     { return "#<Etcd::PutResult>" }
func (r *EtcdPutResult) Inspect() string { return "#<Etcd::PutResult>" }
func (r *EtcdPutResult) Truthy() bool    { return true }

// EtcdDelResult is an instance of Etcd::DelResult: how many keys a #del removed
// and, when requested, their previous values.
type EtcdDelResult struct {
	cls *RClass
	res *etcd.DelResult
}

func (r *EtcdDelResult) ToS() string     { return "#<Etcd::DelResult>" }
func (r *EtcdDelResult) Inspect() string { return "#<Etcd::DelResult>" }
func (r *EtcdDelResult) Truthy() bool    { return true }

// EtcdLease is an instance of Etcd::Lease: a granted lease and its TTL, holding a
// back-reference to its client so it can renew (#keep_alive), report
// (#ttl_info) and revoke (#revoke) itself.
type EtcdLease struct {
	cls    *RClass
	client *EtcdClient
	id     etcd.LeaseID
	ttl    int64
}

func (l *EtcdLease) ToS() string     { return "#<Etcd::Lease>" }
func (l *EtcdLease) Inspect() string { return "#<Etcd::Lease>" }
func (l *EtcdLease) Truthy() bool    { return true }

// EtcdEvent is an instance of Etcd::Event: one change a watch delivered — the
// kind (PUT / DELETE), the affected key-value and, when the watch requested it,
// the previous value.
type EtcdEvent struct {
	cls *RClass
	ev  etcd.Event
}

func (e *EtcdEvent) ToS() string { return "#<Etcd::Event " + e.ev.Type.String() + ">" }
func (e *EtcdEvent) Inspect() string {
	return "#<Etcd::Event " + e.ev.Type.String() + " " + e.ev.Kv.Key + ">"
}
func (e *EtcdEvent) Truthy() bool { return true }

// EtcdTxn is an instance of Etcd::Txn: the transaction builder yielded to a
// #transaction block. It accumulates the comparisons and the success / failure
// operations set through compare= / success= / failure=, which #transaction then
// commits atomically.
type EtcdTxn struct {
	cls     *RClass
	client  *EtcdClient
	cmps    []etcd.Cmp
	thenOps []etcd.Op
	elseOps []etcd.Op
}

func (t *EtcdTxn) ToS() string     { return "#<Etcd::Txn>" }
func (t *EtcdTxn) Inspect() string { return "#<Etcd::Txn>" }
func (t *EtcdTxn) Truthy() bool    { return true }

// EtcdCmp is an instance of Etcd::Compare: one transaction comparison built by a
// Txn comparison method (#value / #version / …), collected into compare=.
type EtcdCmp struct {
	cls *RClass
	cmp etcd.Cmp
}

func (c *EtcdCmp) ToS() string     { return "#<Etcd::Compare>" }
func (c *EtcdCmp) Inspect() string { return "#<Etcd::Compare>" }
func (c *EtcdCmp) Truthy() bool    { return true }

// EtcdOp is an instance of Etcd::Operation: one transaction operation built by a
// Txn op method (#put / #get / #del), collected into success= / failure=.
type EtcdOp struct {
	cls *RClass
	op  etcd.Op
}

func (o *EtcdOp) ToS() string     { return "#<Etcd::Operation>" }
func (o *EtcdOp) Inspect() string { return "#<Etcd::Operation>" }
func (o *EtcdOp) Truthy() bool    { return true }

// EtcdTxnResult is an instance of Etcd::TxnResult: whether a transaction's
// comparisons held and the per-operation replies of the branch that ran.
type EtcdTxnResult struct {
	cls *RClass
	res *etcd.TxnResult
	cl  *etcdClasses
}

func (r *EtcdTxnResult) ToS() string     { return "#<Etcd::TxnResult>" }
func (r *EtcdTxnResult) Inspect() string { return "#<Etcd::TxnResult>" }
func (r *EtcdTxnResult) Truthy() bool    { return true }

// EtcdLock is an instance of Etcd::Lock: a held distributed lock — the
// ownership key and the lease keeping it alive — released with Client#unlock.
type EtcdLock struct {
	cls  *RClass
	lock *etcd.Lock
}

func (l *EtcdLock) ToS() string     { return "#<Etcd::Lock " + l.lock.Key + ">" }
func (l *EtcdLock) Inspect() string { return "#<Etcd::Lock " + l.lock.Key + ">" }
func (l *EtcdLock) Truthy() bool    { return true }

// EtcdMember is an instance of Etcd::Member: one cluster member's identity and
// URLs, mirroring the gem's member object.
type EtcdMember struct {
	cls *RClass
	m   etcd.Member
}

func (m *EtcdMember) ToS() string     { return "#<Etcd::Member " + m.m.Name + ">" }
func (m *EtcdMember) Inspect() string { return "#<Etcd::Member " + m.m.Name + ">" }
func (m *EtcdMember) Truthy() bool    { return true }

// EtcdStatus is an instance of Etcd::Status: a member's version, database size
// and raft state, mirroring the gem's #status.
type EtcdStatus struct {
	cls *RClass
	s   *etcd.StatusResult
}

func (s *EtcdStatus) ToS() string     { return "#<Etcd::Status " + s.s.Version + ">" }
func (s *EtcdStatus) Inspect() string { return "#<Etcd::Status " + s.s.Version + ">" }
func (s *EtcdStatus) Truthy() bool    { return true }

// etcdCtx returns a background context bounded by d together with its cancel
// function; the caller defers cancel so a completed (or panicking) operation
// always releases the timer.
func etcdCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// raiseEtcdError re-raises a go-ruby-etcd error as the matching Ruby exception.
// An *etcd.Error carries the status-code class name its gRPC code maps to (e.g.
// "NotFound"), so it raises the registered Etcd::NotFound when that subclass
// exists and the Etcd::Error base otherwise. Any other error raises the base. It
// never returns (raise panics).
func raiseEtcdError(err error) {
	name := "Error"
	var ee *etcd.Error
	if errors.As(err, &ee) {
		name = ee.Name
	}
	raise("Etcd::"+name, "%s", err.Error())
}

// etcdKV wraps an etcd.KeyValue as an Etcd::KeyValue.
func etcdKV(cl *etcdClasses, kv etcd.KeyValue) *EtcdKeyValue {
	return &EtcdKeyValue{cls: cl.keyValue, kv: kv}
}

// etcdKVPtr wraps a possibly-nil *etcd.KeyValue as an Etcd::KeyValue or nil (an
// absent previous value).
func etcdKVPtr(cl *etcdClasses, kv *etcd.KeyValue) object.Value {
	if kv == nil {
		return object.NilV
	}
	return etcdKV(cl, *kv)
}

// etcdKVArray wraps a slice of etcd.KeyValue as a Ruby Array of Etcd::KeyValue.
func etcdKVArray(cl *etcdClasses, kvs []etcd.KeyValue) *object.Array {
	out := make([]object.Value, len(kvs))
	for i := range kvs {
		out[i] = etcdKV(cl, kvs[i])
	}
	return object.NewArrayFromSlice(out)
}

// etcdKeyArg coerces the required key / name argument of an operation to a
// String, raising ArgumentError when it was omitted.
func etcdKeyArg(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return strArg(args[0])
}

// etcdOptsHash returns the trailing keyword Hash of an operation (the options
// after its positional arguments), or nil when the last argument at or after
// from is not a Hash.
func etcdOptsHash(args []object.Value, from int) *object.Hash {
	if len(args) <= from {
		return nil
	}
	h, ok := args[len(args)-1].(*object.Hash)
	if !ok {
		return nil
	}
	return h
}

// etcdBool reads a boolean option: any truthy value enables it.
func etcdBool(h *object.Hash, key string) bool {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v.Truthy()
	}
	return false
}

// etcdIntOpt reads an Integer option, returning ok=false when it is absent.
func etcdIntOpt(h *object.Hash, key string) (int64, bool) {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return intArg(v), true
	}
	return 0, false
}

// etcdSeconds reads a duration option expressed in seconds, accepting an Integer
// or a Float and raising TypeError for anything else.
func etcdSeconds(v object.Value) float64 {
	switch n := v.(type) {
	case object.Integer:
		return float64(n)
	case object.Float:
		return float64(n)
	}
	raise("TypeError", "no implicit conversion of %s into Numeric", v.Inspect())
	return 0
}

// etcdRangeOpts builds the read/delete range OpOptions common to #get, #del and
// #watch from an options Hash: prefix:, range_end:, from_key:, revision:,
// limit:, count_only:, keys_only: and serializable:. A nil Hash yields no
// options (a single-key operation).
func etcdRangeOpts(h *object.Hash) []etcd.OpOption {
	var opts []etcd.OpOption
	if h == nil {
		return opts
	}
	if etcdBool(h, "prefix") {
		opts = append(opts, etcd.WithPrefix())
	}
	if v, ok := h.Get(object.Symbol("range_end")); ok {
		opts = append(opts, etcd.WithRange(strArg(v)))
	}
	if etcdBool(h, "from_key") {
		opts = append(opts, etcd.WithFromKey())
	}
	if n, ok := etcdIntOpt(h, "revision"); ok {
		opts = append(opts, etcd.WithRevision(n))
	}
	if n, ok := etcdIntOpt(h, "limit"); ok {
		opts = append(opts, etcd.WithLimit(n))
	}
	if etcdBool(h, "count_only") {
		opts = append(opts, etcd.WithCountOnly())
	}
	if etcdBool(h, "keys_only") {
		opts = append(opts, etcd.WithKeysOnly())
	}
	if etcdBool(h, "serializable") {
		opts = append(opts, etcd.WithSerializable())
	}
	return opts
}

// etcdPutOpts builds the OpOptions of a #put from an options Hash: lease: (an
// Integer id or an Etcd::Lease) and prev_kv:.
func etcdPutOpts(h *object.Hash) []etcd.OpOption {
	var opts []etcd.OpOption
	if h == nil {
		return opts
	}
	if v, ok := h.Get(object.Symbol("lease")); ok {
		opts = append(opts, etcd.WithLease(etcdLeaseID(v)))
	}
	if etcdBool(h, "prev_kv") {
		opts = append(opts, etcd.WithPrevKV())
	}
	return opts
}

// etcdDelOpts builds the OpOptions of a #del from an options Hash: the range
// selectors plus prev_kv:.
func etcdDelOpts(h *object.Hash) []etcd.OpOption {
	opts := etcdRangeOpts(h)
	if h != nil && etcdBool(h, "prev_kv") {
		opts = append(opts, etcd.WithPrevKV())
	}
	return opts
}

// etcdLeaseID coerces a lease argument — an Etcd::Lease or an Integer id — to an
// etcd.LeaseID, raising TypeError for anything else.
func etcdLeaseID(v object.Value) etcd.LeaseID {
	switch x := v.(type) {
	case *EtcdLease:
		return x.id
	case object.Integer:
		return etcd.LeaseID(int64(x))
	}
	raise("TypeError", "no implicit conversion of %s into an Etcd::Lease or Integer", v.Inspect())
	return 0
}

// etcdCmpOp maps a Ruby comparison operator — a Symbol (:equal / :not_equal /
// :greater / :less) or the literal operator String ("=", "!=", ">", "<") — to
// the clientv3 operator string, raising ArgumentError for anything else.
func etcdCmpOp(v object.Value) string {
	var s string
	switch x := v.(type) {
	case object.Symbol:
		s = string(x)
	case *object.String:
		s = x.Str()
	default:
		raise("ArgumentError", "expected a comparison operator (Symbol or String), got %s", v.Inspect())
	}
	switch s {
	case "equal", "==", "=":
		return "="
	case "not_equal", "!=":
		return "!="
	case "greater", ">":
		return ">"
	case "less", "<":
		return "<"
	}
	raise("ArgumentError", "unknown comparison operator %q", s)
	return ""
}

// etcdCmpVal maps a comparison's target value into the Go value clientv3.Compare
// expects: a String compares a key's value, and an Integer compares a numeric
// target (version / revision / lease). Anything else raises TypeError.
func etcdCmpVal(v object.Value) any {
	switch x := v.(type) {
	case *object.String:
		return x.Str()
	case object.Integer:
		return int64(x)
	}
	raise("TypeError", "no implicit conversion of %s into a comparison value", v.Inspect())
	return nil
}

// etcdCmpList coerces a compare= assignment into the comparison slice: an Array
// of Etcd::Compare (or a single Etcd::Compare). A wrong element type raises
// TypeError.
func etcdCmpList(v object.Value) []etcd.Cmp {
	var out []etcd.Cmp
	for _, e := range etcdElems(v) {
		c, ok := e.(*EtcdCmp)
		if !ok {
			raise("TypeError", "expected an Etcd::Compare, got %s", e.Inspect())
		}
		out = append(out, c.cmp)
	}
	return out
}

// etcdOpList coerces a success= / failure= assignment into the operation slice:
// an Array of Etcd::Operation (or a single Etcd::Operation). A wrong element type
// raises TypeError.
func etcdOpList(v object.Value) []etcd.Op {
	var out []etcd.Op
	for _, e := range etcdElems(v) {
		o, ok := e.(*EtcdOp)
		if !ok {
			raise("TypeError", "expected an Etcd::Operation, got %s", e.Inspect())
		}
		out = append(out, o.op)
	}
	return out
}

// etcdElems normalises a transaction branch assignment into a slice: an Array
// yields its elements, and any single value yields a one-element slice.
func etcdElems(v object.Value) []object.Value {
	if arr, ok := v.(*object.Array); ok {
		return arr.Elems
	}
	return []object.Value{v}
}

// etcdTxnResult wraps an etcd.TxnResult as an Etcd::TxnResult.
func etcdTxnResult(cl *etcdClasses, res *etcd.TxnResult) *EtcdTxnResult {
	return &EtcdTxnResult{cls: cl.txnResult, res: res, cl: cl}
}

// etcdTxnResponses renders a transaction's per-operation replies as a Ruby Array:
// each reply is the Etcd::GetResult / Etcd::PutResult / Etcd::DelResult of the
// operation that produced it.
func etcdTxnResponses(cl *etcdClasses, res *etcd.TxnResult) *object.Array {
	out := make([]object.Value, len(res.Responses))
	for i, r := range res.Responses {
		out[i] = object.NilV
		switch {
		case r.Get != nil:
			out[i] = &EtcdGetResult{cls: cl.getResult, res: r.Get}
		case r.Put != nil:
			out[i] = &EtcdPutResult{cls: cl.putResult, res: r.Put}
		case r.Del != nil:
			out[i] = &EtcdDelResult{cls: cl.delResult, res: r.Del}
		}
	}
	return object.NewArrayFromSlice(out)
}
