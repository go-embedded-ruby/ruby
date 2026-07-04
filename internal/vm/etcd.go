// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	etcd "github.com/go-ruby-etcd/etcd"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerEtcd installs the Etcd module (require "etcd") and its Etcdv3 alias
// (require "etcdv3"): the etcdv3-gem-flavoured client for etcd v3, reimplemented
// in pure Go (CGO=0) by github.com/go-ruby-etcd/etcd on top of the official
// go.etcd.io/etcd/client/v3 transport. The library owns the protocol; this file
// is the thin shell mapping its surface onto rbgo classes:
//
//	Etcdv3.new(endpoints: '...')  — the connection (Etcd::Client): #put/#get/#del/
//	                                #exists?, ranges & prefixes, #transaction,
//	                                #watch, lease and lock operations
//	Etcd::KeyValue / GetResult / PutResult / DelResult — the reply value objects
//	Etcd::Lease                   — a granted lease (#keep_alive/#revoke/#ttl_info)
//	Etcd::Txn / Compare / Operation / TxnResult — the transaction builder & reply
//	Etcd::Event                   — one watch change (PUT / DELETE)
//	Etcd::Lock / Member / Status  — the concurrency and maintenance views
//	Etcd::Error (< StandardError) — the gRPC-status error tree
//
// Etcd and Etcdv3 name the same classes, so a value's #class is stable however
// it was required. The value types, argument coercions and the error bridge live
// in etcd_bind.go. Watch delivery is synchronous — #watch drives the client's
// event channel on the calling VM thread and yields each event to the block
// directly, so nothing crosses onto the single-threaded VM from another
// goroutine and the watch is deterministic and leak-free (its context is always
// cancelled on return).
func (vm *VM) registerEtcd() {
	mod := newClass("Etcd", nil)
	mod.isModule = true
	vm.consts["Etcd"] = mod

	cl := vm.registerEtcdClasses(mod)
	vm.registerEtcdErrors(mod)
	vm.registerEtcdClient(cl)
	vm.registerEtcdKeyValue(cl)
	vm.registerEtcdGetResult(cl)
	vm.registerEtcdPutResult(cl)
	vm.registerEtcdDelResult(cl)
	vm.registerEtcdLease(cl)
	vm.registerEtcdEvent(cl)
	vm.registerEtcdTxn(cl)
	vm.registerEtcdTxnResult(cl)
	vm.registerEtcdLock(cl)
	vm.registerEtcdMember(cl)
	vm.registerEtcdStatus(cl)

	// Etcdv3 is a second module naming exactly the same classes, so
	// require "etcdv3" and require "etcd" are interchangeable and a value's
	// #class is the same object either way.
	alias := newClass("Etcdv3", nil)
	alias.isModule = true
	alias.consts = mod.consts
	vm.consts["Etcdv3"] = alias
	vm.etcdModuleNew(mod, cl)
	vm.etcdModuleNew(alias, cl)
}

// registerEtcdClasses creates every Etcd value class under the module and
// returns the resolved set the bridges construct from. Each class is registered
// both scoped (Etcd::Client) and flat in vm.consts so raise and classOf resolve
// it by qualified name.
func (vm *VM) registerEtcdClasses(mod *RClass) *etcdClasses {
	mk := func(name string) *RClass {
		full := "Etcd::" + name
		cls := newClass(full, vm.cObject)
		mod.consts[name] = cls
		vm.consts[full] = cls
		return cls
	}
	return &etcdClasses{
		client:    mk("Client"),
		keyValue:  mk("KeyValue"),
		getResult: mk("GetResult"),
		putResult: mk("PutResult"),
		delResult: mk("DelResult"),
		lease:     mk("Lease"),
		event:     mk("Event"),
		txn:       mk("Txn"),
		cmp:       mk("Compare"),
		op:        mk("Operation"),
		txnResult: mk("TxnResult"),
		lock:      mk("Lock"),
		member:    mk("Member"),
		status:    mk("Status"),
	}
}

// registerEtcdErrors installs the Etcd exception tree: Etcd::Error <
// StandardError, and one subclass per gRPC status code (Etcd::NotFound,
// Etcd::InvalidArgument, …) named exactly as the library's *etcd.Error reports,
// so raiseEtcdError can map any library error onto a registered class.
func (vm *VM) registerEtcdErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("Etcd::Error", std)
	mod.consts["Error"] = base
	vm.consts["Etcd::Error"] = base
	for _, name := range []string{
		"Canceled", "Unknown", "InvalidArgument", "DeadlineExceeded",
		"NotFound", "AlreadyExists", "PermissionDenied", "ResourceExhausted",
		"FailedPrecondition", "Aborted", "OutOfRange", "Unimplemented",
		"Internal", "Unavailable", "DataLoss", "Unauthenticated",
	} {
		c := newClass("Etcd::"+name, base)
		mod.consts[name] = c
		vm.consts["Etcd::"+name] = c
	}
}

// etcdConnect builds an *etcd.Client from a connection options Hash: endpoints:
// (a String or Array of Strings, with url:/hosts: aliases), dial_timeout: (a
// number of seconds), and username:/user: + password:. It raises
// ArgumentError when no endpoints were given and Etcd::Error on a malformed
// configuration.
func (vm *VM) etcdConnect(cl *etcdClasses, args []object.Value) object.Value {
	h := etcdOptsHash(args, 0)
	if h == nil {
		raise("ArgumentError", "expected connection options (endpoints:)")
	}
	eps := etcdEndpoints(h)
	if len(eps) == 0 {
		raise("ArgumentError", "no endpoints given")
	}
	cfg := etcd.Config{Endpoints: eps, DialTimeout: etcdDialTimeout}
	if v, ok := h.Get(object.Symbol("dial_timeout")); ok {
		cfg.DialTimeout = time.Duration(etcdSeconds(v) * float64(time.Second))
	}
	cfg.Username = etcdStrOpt(h, "username", "user")
	cfg.Password = etcdStrOpt(h, "password", "")
	// command_timeout: (the etcdv3 gem keyword) bounds each request/reply
	// operation; timeout: is accepted as an alias. It defaults to etcdOpTimeout.
	opTimeout := etcdOpTimeout
	if v, ok := h.Get(object.Symbol("command_timeout")); ok {
		opTimeout = time.Duration(etcdSeconds(v) * float64(time.Second))
	} else if v, ok := h.Get(object.Symbol("timeout")); ok {
		opTimeout = time.Duration(etcdSeconds(v) * float64(time.Second))
	}
	c, err := etcd.New(cfg)
	if err != nil {
		raiseEtcdError(err)
	}
	return &EtcdClient{cls: cl.client, c: c, endpoints: eps, opTimeout: opTimeout, cl: cl}
}

// etcdEndpoints reads the endpoints: (or url: / hosts:) option as a list of
// Strings, accepting a single String or an Array of Strings.
func etcdEndpoints(h *object.Hash) []string {
	for _, key := range []string{"endpoints", "url", "hosts"} {
		v, ok := h.Get(object.Symbol(key))
		if !ok {
			continue
		}
		switch x := v.(type) {
		case *object.String:
			return []string{x.Str()}
		case *object.Array:
			out := make([]string, len(x.Elems))
			for i, e := range x.Elems {
				out[i] = strArg(e)
			}
			return out
		default:
			raise("TypeError", "endpoints must be a String or Array, got %s", v.Inspect())
		}
	}
	return nil
}

// etcdStrOpt reads a String option under key, falling back to alt (when
// non-empty), returning "" when neither is set.
func etcdStrOpt(h *object.Hash, key, alt string) string {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return strArg(v)
	}
	if alt != "" {
		if v, ok := h.Get(object.Symbol(alt)); ok {
			return strArg(v)
		}
	}
	return ""
}

// etcdModuleNew installs the module-level Etcdv3.new / Etcd.new constructor that
// mirrors the gem's Etcdv3.new(endpoints: …), returning an Etcd::Client.
func (vm *VM) etcdModuleNew(mod *RClass, cl *etcdClasses) {
	mod.smethods["new"] = &Method{name: "new", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.etcdConnect(cl, args)
		}}
	mod.smethods["connect"] = mod.smethods["new"]
}

// registerEtcdClient installs Etcd::Client and its operation surface.
func (vm *VM) registerEtcdClient(cl *etcdClasses) {
	cls := cl.client
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.etcdConnect(cl, args)
		}}
	self := func(v object.Value) *EtcdClient { return v.(*EtcdClient) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// #put(key, value, **opts) stores value at key and returns an
	// Etcd::PutResult. opts may carry lease: and prev_kv:.
	d("put", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		ctx, cancel := etcdCtx(self(v).opTimeout)
		defer cancel()
		res, err := self(v).c.Put(ctx, strArg(args[0]), strArg(args[1]), etcdPutOpts(etcdOptsHash(args, 2))...)
		if err != nil {
			raiseEtcdError(err)
		}
		return &EtcdPutResult{cls: cl.putResult, res: res}
	})

	// #get(key, **opts) reads key, or a range/prefix when a range option is
	// given, and returns an Etcd::GetResult.
	d("get", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		key := etcdKeyArg(args)
		ctx, cancel := etcdCtx(self(v).opTimeout)
		defer cancel()
		res, err := self(v).c.Get(ctx, key, etcdRangeOpts(etcdOptsHash(args, 1))...)
		if err != nil {
			raiseEtcdError(err)
		}
		return &EtcdGetResult{cls: cl.getResult, res: res}
	})

	// #del(key, **opts) / #delete deletes key, or a range/prefix, and returns an
	// Etcd::DelResult.
	del := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		key := etcdKeyArg(args)
		ctx, cancel := etcdCtx(self(v).opTimeout)
		defer cancel()
		res, err := self(v).c.Del(ctx, key, etcdDelOpts(etcdOptsHash(args, 1))...)
		if err != nil {
			raiseEtcdError(err)
		}
		return &EtcdDelResult{cls: cl.delResult, res: res}
	}
	d("del", del)
	d("delete", del)

	// #exists?(key) reports whether key is present, using a count-only read.
	d("exists?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		key := etcdKeyArg(args)
		ctx, cancel := etcdCtx(self(v).opTimeout)
		defer cancel()
		ok, err := self(v).c.Exists(ctx, key)
		if err != nil {
			raiseEtcdError(err)
		}
		return object.Bool(ok)
	})

	// #transaction { |txn| ... } builds a transaction through the yielded
	// Etcd::Txn (compare= / success= / failure=), commits it atomically and
	// returns an Etcd::TxnResult.
	d("transaction", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		w := &EtcdTxn{cls: cl.txn, client: self(v)}
		vm.callBlock(blk, []object.Value{w})
		ctx, cancel := etcdCtx(self(v).opTimeout)
		defer cancel()
		res, err := self(v).c.Transaction(ctx, func(t *etcd.Txn) {
			t.If(w.cmps...).Then(w.thenOps...).Else(w.elseOps...)
		})
		if err != nil {
			raiseEtcdError(err)
		}
		return etcdTxnResult(cl, res)
	})

	// #watch(key, **opts) { |event| ... } watches key (or a prefix/range) and
	// yields each Etcd::Event to the block, returning the number delivered. opts
	// may carry prefix:, range_end:, from_key:, start_revision:/revision:,
	// prev_kv:, timeout: (seconds the watch waits) and max_events: (a delivery
	// cap that stops the watch early). The watch context is always cancelled on
	// return, so no goroutine is leaked.
	d("watch", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		key := etcdKeyArg(args)
		return self(v).watch(vm, key, etcdOptsHash(args, 1), blk)
	})

	// #lease_grant(ttl) grants a lease expiring after ttl seconds and returns an
	// Etcd::Lease.
	d("lease_grant", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		ctx, cancel := etcdCtx(self(v).opTimeout)
		defer cancel()
		res, err := self(v).c.LeaseGrant(ctx, intArg(args[0]))
		if err != nil {
			raiseEtcdError(err)
		}
		return &EtcdLease{cls: cl.lease, client: self(v), id: res.ID, ttl: res.TTL}
	})

	// #lease_keep_alive(id) renews a lease once and returns its new remaining
	// TTL. id is an Etcd::Lease or an Integer lease id.
	d("lease_keep_alive", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.IntValue(self(v).leaseKeepAlive(etcdLeaseID(args[0])))
	})

	// #lease_revoke(id) revokes a lease, deleting every key attached to it.
	d("lease_revoke", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		self(v).leaseRevoke(etcdLeaseID(args[0]))
		return object.NilV
	})

	// #lease_ttl(id, keys: false) returns a lease's remaining TTL as a Hash
	// {ttl:, granted_ttl:, keys:}; keys: true also lists the attached keys.
	d("lease_ttl", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		withKeys := etcdBool2(etcdOptsHash(args, 1), "keys")
		return self(v).leaseTTL(etcdLeaseID(args[0]), withKeys)
	})

	// #lock(name, ttl) acquires a distributed lock named name backed by a lease
	// of ttl seconds, blocking until held, and returns an Etcd::Lock.
	d("lock", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		ctx, cancel := etcdCtx(self(v).opTimeout)
		defer cancel()
		lk, err := self(v).c.Lock(ctx, strArg(args[0]), intArg(args[1]))
		if err != nil {
			raiseEtcdError(err)
		}
		return &EtcdLock{cls: cl.lock, lock: lk}
	})

	// #unlock(lock) releases a held Etcd::Lock: it deletes the ownership key and
	// revokes its lease.
	d("unlock", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		lk, ok := args[0].(*EtcdLock)
		if !ok {
			raise("TypeError", "expected an Etcd::Lock, got %s", args[0].Inspect())
		}
		ctx, cancel := etcdCtx(self(v).opTimeout)
		defer cancel()
		if err := self(v).c.Unlock(ctx, lk.lock); err != nil {
			raiseEtcdError(err)
		}
		return object.NilV
	})

	// #members lists the cluster's members as an Array of Etcd::Member.
	d("members", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		ctx, cancel := etcdCtx(self(v).opTimeout)
		defer cancel()
		res, err := self(v).c.Members(ctx)
		if err != nil {
			raiseEtcdError(err)
		}
		out := make([]object.Value, len(res.Members))
		for i := range res.Members {
			out[i] = &EtcdMember{cls: cl.member, m: res.Members[i]}
		}
		return object.NewArrayFromSlice(out)
	})

	// #status(endpoint) returns the Etcd::Status of the member serving endpoint.
	d("status", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		ctx, cancel := etcdCtx(self(v).opTimeout)
		defer cancel()
		res, err := self(v).c.Status(ctx, strArg(args[0]))
		if err != nil {
			raiseEtcdError(err)
		}
		return &EtcdStatus{cls: cl.status, s: res}
	})

	// #endpoints returns the endpoints the client was configured with.
	d("endpoints", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		eps := self(v).endpoints
		out := make([]object.Value, len(eps))
		for i, e := range eps {
			out[i] = object.NewString(e)
		}
		return object.NewArrayFromSlice(out)
	})

	// #close releases the client's resources.
	d("close", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		_ = self(v).c.Close()
		return object.NilV
	})
}

// watch drives the client's event channel synchronously on the calling VM
// thread, yielding each Etcd::Event to blk and returning the count delivered.
// The context is bounded by timeout: (defaulting to the client's
// command_timeout) and always cancelled on return; a max_events: cap stops the
// watch as soon as it is met.
func (c *EtcdClient) watch(vm *VM, key string, h *object.Hash, blk *Proc) object.Value {
	// A no-keyword watch arrives with a nil Hash; normalise it to an empty one so
	// the option readers below (and etcdWatchOpts) have a single, uniform path.
	if h == nil {
		h = object.NewHash()
	}
	// The watch blocks until it delivers max_events:, its timeout: elapses, or —
	// absent a timeout: — the client's command_timeout bounds it, so a watch is
	// never able to hang the single-threaded VM indefinitely.
	wait := c.opTimeout
	if v, ok := h.Get(object.Symbol("timeout")); ok {
		wait = time.Duration(etcdSeconds(v) * float64(time.Second))
	}
	max := int64(0)
	if n, ok := etcdIntOpt(h, "max_events"); ok {
		max = n
	}
	ctx, cancel := etcdCtx(wait)
	defer cancel()
	delivered := int64(0)
	for r := range c.c.Watch(ctx, key, etcdWatchOpts(h)...) {
		for i := range r.Events {
			vm.callBlock(blk, []object.Value{&EtcdEvent{cls: c.cl.event, ev: r.Events[i]}})
			delivered++
			if max > 0 && delivered >= max {
				return object.IntValue(delivered)
			}
		}
	}
	return object.IntValue(delivered)
}

// etcdWatchOpts builds a watch's OpOptions from its options Hash: the range
// selectors (prefix:/range_end:/from_key:), start_revision: (or revision:) and
// prev_kv:.
func etcdWatchOpts(h *object.Hash) []etcd.OpOption {
	var opts []etcd.OpOption
	if etcdBool(h, "prefix") {
		opts = append(opts, etcd.WithPrefix())
	}
	if v, ok := h.Get(object.Symbol("range_end")); ok {
		opts = append(opts, etcd.WithRange(strArg(v)))
	}
	if etcdBool(h, "from_key") {
		opts = append(opts, etcd.WithFromKey())
	}
	if n, ok := etcdIntOpt(h, "start_revision"); ok {
		opts = append(opts, etcd.WithRevision(n))
	} else if n, ok := etcdIntOpt(h, "revision"); ok {
		opts = append(opts, etcd.WithRevision(n))
	}
	if etcdBool(h, "prev_kv") {
		opts = append(opts, etcd.WithPrevKV())
	}
	return opts
}

// etcdBool2 reads a boolean option from a possibly-nil Hash.
func etcdBool2(h *object.Hash, key string) bool {
	return h != nil && etcdBool(h, key)
}

// leaseKeepAlive renews id once and returns its new TTL, raising the mapped Ruby
// exception on failure.
func (c *EtcdClient) leaseKeepAlive(id etcd.LeaseID) int64 {
	ctx, cancel := etcdCtx(c.opTimeout)
	defer cancel()
	res, err := c.c.LeaseKeepAliveOnce(ctx, id)
	if err != nil {
		raiseEtcdError(err)
	}
	return res.TTL
}

// leaseRevoke revokes id, raising the mapped Ruby exception on failure.
func (c *EtcdClient) leaseRevoke(id etcd.LeaseID) {
	ctx, cancel := etcdCtx(c.opTimeout)
	defer cancel()
	if _, err := c.c.LeaseRevoke(ctx, id); err != nil {
		raiseEtcdError(err)
	}
}

// leaseTTL returns id's remaining TTL as a Hash {ttl:, granted_ttl:, keys:};
// withKeys also lists the attached keys.
func (c *EtcdClient) leaseTTL(id etcd.LeaseID, withKeys bool) object.Value {
	ctx, cancel := etcdCtx(c.opTimeout)
	defer cancel()
	res, err := c.c.LeaseTTL(ctx, id, withKeys)
	if err != nil {
		raiseEtcdError(err)
	}
	h := object.NewHash()
	h.Set(object.Symbol("id"), object.IntValue(int64(res.ID)))
	h.Set(object.Symbol("ttl"), object.IntValue(res.TTL))
	h.Set(object.Symbol("granted_ttl"), object.IntValue(res.GrantedTTL))
	keys := make([]object.Value, len(res.Keys))
	for i, k := range res.Keys {
		keys[i] = object.NewString(k)
	}
	h.Set(object.Symbol("keys"), object.NewArrayFromSlice(keys))
	return h
}

// registerEtcdKeyValue installs Etcd::KeyValue and its readers.
func (vm *VM) registerEtcdKeyValue(cl *etcdClasses) {
	cls := cl.keyValue
	self := func(v object.Value) etcd.KeyValue { return v.(*EtcdKeyValue).kv }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("key", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Key)
	})
	d("value", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Value)
	})
	d("version", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).Version)
	})
	d("create_revision", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).CreateRevision)
	})
	d("mod_revision", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).ModRevision)
	})
	d("lease", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).Lease)
	})
	d("to_h", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		kv := self(v)
		h := object.NewHash()
		h.Set(object.Symbol("key"), object.NewString(kv.Key))
		h.Set(object.Symbol("value"), object.NewString(kv.Value))
		h.Set(object.Symbol("version"), object.IntValue(kv.Version))
		h.Set(object.Symbol("create_revision"), object.IntValue(kv.CreateRevision))
		h.Set(object.Symbol("mod_revision"), object.IntValue(kv.ModRevision))
		h.Set(object.Symbol("lease"), object.IntValue(kv.Lease))
		return h
	})
}

// registerEtcdGetResult installs Etcd::GetResult and its range surface.
func (vm *VM) registerEtcdGetResult(cl *etcdClasses) {
	cls := cl.getResult
	self := func(v object.Value) *EtcdGetResult { return v.(*EtcdGetResult) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("kvs", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return etcdKVArray(cl, self(v).res.Kvs)
	})
	d("count", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).res.Count)
	})
	d("more?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).res.More)
	})
	d("empty?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(len(self(v).res.Kvs) == 0)
	})
	// #first returns the first Etcd::KeyValue, or nil when the result is empty.
	d("first", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		kv := self(v).res.First()
		if kv == nil {
			return object.NilV
		}
		return etcdKV(cl, *kv)
	})
	// #value returns the first key's value, or nil when the result is empty — the
	// common single-key read idiom conn.get(k).value.
	d("value", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		kv := self(v).res.First()
		if kv == nil {
			return object.NilV
		}
		return object.NewString(kv.Value)
	})
	d("to_a", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return etcdKVArray(cl, self(v).res.Kvs)
	})
	// #each { |kv| ... } yields each Etcd::KeyValue, returning self.
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		for _, kv := range self(v).res.Kvs {
			vm.callBlock(blk, []object.Value{etcdKV(cl, kv)})
		}
		return v
	})
}

// registerEtcdPutResult installs Etcd::PutResult and its readers.
func (vm *VM) registerEtcdPutResult(cl *etcdClasses) {
	cls := cl.putResult
	self := func(v object.Value) *etcd.PutResult { return v.(*EtcdPutResult).res }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("revision", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).Header.Revision)
	})
	// #prev_kv returns the previous Etcd::KeyValue (when prev_kv: was set), or nil.
	d("prev_kv", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return etcdKVPtr(cl, self(v).PrevKv)
	})
}

// registerEtcdDelResult installs Etcd::DelResult and its readers.
func (vm *VM) registerEtcdDelResult(cl *etcdClasses) {
	cls := cl.delResult
	self := func(v object.Value) *etcd.DelResult { return v.(*EtcdDelResult).res }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("deleted", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).Deleted)
	})
	d("prev_kvs", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return etcdKVArray(cl, self(v).PrevKvs)
	})
}

// registerEtcdLease installs Etcd::Lease and its lifecycle surface, each method
// delegating to the client the lease was granted by.
func (vm *VM) registerEtcdLease(cl *etcdClasses) {
	cls := cl.lease
	self := func(v object.Value) *EtcdLease { return v.(*EtcdLease) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("id", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).id))
	})
	d("ttl", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).ttl)
	})
	// #keep_alive renews the lease once and returns its new remaining TTL.
	d("keep_alive", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		l := self(v)
		return object.IntValue(l.client.leaseKeepAlive(l.id))
	})
	// #revoke revokes the lease, deleting every key attached to it.
	d("revoke", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		l := self(v)
		l.client.leaseRevoke(l.id)
		return object.NilV
	})
	// #ttl_info(keys: false) returns the lease's live TTL Hash.
	d("ttl_info", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		l := self(v)
		return l.client.leaseTTL(l.id, etcdBool2(etcdOptsHash(args, 0), "keys"))
	})
}

// registerEtcdEvent installs Etcd::Event and its readers.
func (vm *VM) registerEtcdEvent(cl *etcdClasses) {
	cls := cl.event
	self := func(v object.Value) etcd.Event { return v.(*EtcdEvent).ev }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// #type is the change kind as the gem renders it ("PUT" / "DELETE").
	d("type", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Type.String())
	})
	d("put?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Type == etcd.EventPut)
	})
	d("delete?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Type == etcd.EventDelete)
	})
	d("kv", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return etcdKV(cl, self(v).Kv)
	})
	d("key", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Kv.Key)
	})
	d("value", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Kv.Value)
	})
	// #prev_kv returns the previous Etcd::KeyValue (when the watch set prev_kv:), or nil.
	d("prev_kv", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return etcdKVPtr(cl, self(v).PrevKv)
	})
}

// registerEtcdTxn installs Etcd::Txn: the builder yielded to #transaction. Its
// comparison methods (#value/#version/#create_revision/#mod_revision/#lease)
// return Etcd::Compare and its op methods (#put/#get/#del) return
// Etcd::Operation; compare= / success= / failure= collect them into the
// transaction's If / Then / Else branches.
func (vm *VM) registerEtcdTxn(cl *etcdClasses) {
	cls := cl.txn
	self := func(v object.Value) *EtcdTxn { return v.(*EtcdTxn) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// cmp builds a comparison method: target(key) picks the field to compare and
	// the method takes (key, op, value).
	cmp := func(name string, target func(string) etcd.Cmp) {
		d(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 3 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 3) for %s", len(args), name)
			}
			c := etcd.Compare(target(strArg(args[0])), etcdCmpOp(args[1]), etcdCmpVal(args[2]))
			return &EtcdCmp{cls: cl.cmp, cmp: c}
		})
	}
	cmp("value", etcd.Value)
	cmp("version", etcd.Version)
	cmp("create_revision", etcd.CreateRevision)
	cmp("mod_revision", etcd.ModRevision)
	cmp("lease", etcd.LeaseValue)

	d("put", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		return &EtcdOp{cls: cl.op, op: etcd.OpPut(strArg(args[0]), strArg(args[1]), etcdPutOpts(etcdOptsHash(args, 2))...)}
	})
	d("get", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		key := etcdKeyArg(args)
		return &EtcdOp{cls: cl.op, op: etcd.OpGet(key, etcdRangeOpts(etcdOptsHash(args, 1))...)}
	})
	txnDel := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		key := etcdKeyArg(args)
		return &EtcdOp{cls: cl.op, op: etcd.OpDelete(key, etcdDelOpts(etcdOptsHash(args, 1))...)}
	}
	d("del", txnDel)
	d("delete", txnDel)

	// compare= / success= / failure= set the transaction's branches.
	d("compare=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).cmps = etcdCmpList(args[0])
		return args[0]
	})
	d("success=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).thenOps = etcdOpList(args[0])
		return args[0]
	})
	d("failure=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).elseOps = etcdOpList(args[0])
		return args[0]
	})
}

// registerEtcdTxnResult installs Etcd::TxnResult and its readers.
func (vm *VM) registerEtcdTxnResult(cl *etcdClasses) {
	cls := cl.txnResult
	self := func(v object.Value) *EtcdTxnResult { return v.(*EtcdTxnResult) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	succeeded := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).res.Succeeded)
	}
	d("succeeded?", succeeded)
	d("success?", succeeded)
	d("revision", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).res.Header.Revision)
	})
	// #responses returns the per-operation replies of the branch that ran.
	d("responses", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return etcdTxnResponses(self(v).cl, self(v).res)
	})
}

// registerEtcdLock installs Etcd::Lock and its readers.
func (vm *VM) registerEtcdLock(cl *etcdClasses) {
	cls := cl.lock
	self := func(v object.Value) *etcd.Lock { return v.(*EtcdLock).lock }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("key", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Key)
	})
	d("lease", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Lease))
	})
}

// registerEtcdMember installs Etcd::Member and its readers.
func (vm *VM) registerEtcdMember(cl *etcdClasses) {
	cls := cl.member
	self := func(v object.Value) etcd.Member { return v.(*EtcdMember).m }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("id", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).ID))
	})
	d("name", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Name)
	})
	d("peer_urls", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return etcdStrArray(self(v).PeerURLs)
	})
	d("client_urls", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return etcdStrArray(self(v).ClientURLs)
	})
	d("learner?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).IsLearner)
	})
}

// registerEtcdStatus installs Etcd::Status and its readers.
func (vm *VM) registerEtcdStatus(cl *etcdClasses) {
	cls := cl.status
	self := func(v object.Value) *etcd.StatusResult { return v.(*EtcdStatus).s }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("version", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Version)
	})
	d("db_size", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).DbSize)
	})
	d("leader", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Leader))
	})
	d("raft_index", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).RaftIndex))
	})
	d("raft_term", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).RaftTerm))
	})
	d("learner?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).IsLearner)
	})
}

// etcdStrArray wraps a Go string slice as a Ruby Array of Strings.
func etcdStrArray(ss []string) *object.Array {
	out := make([]object.Value, len(ss))
	for i, s := range ss {
		out[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(out)
}
