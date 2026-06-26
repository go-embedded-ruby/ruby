package vm

import (
	"sync/atomic"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// iseqCaches returns the inline-cache slice for iseq, allocating it (one slot
// per instruction) on first use and memoising it on the ISeq. Touched only
// under the GVL, so the lazy fill needs no synchronisation.
func iseqCaches(iseq *bytecode.ISeq) []inlineCache {
	if iseq.Caches == nil {
		iseq.Caches = make([]inlineCache, len(iseq.Insns))
	}
	return iseq.Caches.([]inlineCache)
}

// iseqHasHandler reports whether iseq contains any rescue handler
// (OpPushHandler), so exec can skip the per-frame recover defer when there is
// none. The result is scanned once and memoised on the ISeq (handlerState).
func iseqHasHandler(iseq *bytecode.ISeq) bool {
	switch iseq.HandlerState() {
	case 1:
		return false
	case 2:
		return true
	}
	has := false
	for i := range iseq.Insns {
		if iseq.Insns[i].Op == bytecode.OpPushHandler {
			has = true
			break
		}
	}
	if has {
		iseq.SetHandlerState(2)
	} else {
		iseq.SetHandlerState(1)
	}
	return has
}

// globalMethodSerial is bumped on every change that could alter what
// lookupMethod returns: an instance-method definition or alias, an include /
// prepend, a super-chain edit, or the creation of a singleton class. Inline
// caches stamp themselves with the serial at fill time and re-validate against
// it on every hit, so any monkey-patch / define_method / include invalidates
// every cache in one cheap comparison.
//
// A single VM only ever touches it under the GVL (VM execution is
// single-threaded), but RClass.define has no VM handle and embedders may run
// several VMs in parallel goroutines; it is therefore a process-wide atomic so
// concurrent VMs never race on it. Inline caches live per-ISeq (never shared
// across VMs), so a globally monotonic serial only ever over-invalidates — never
// returns a stale method — which is always safe.
var globalMethodSerial atomic.Uint64

func init() { globalMethodSerial.Store(1) }

// methodSerial reads the current serial (atomic load on the cache-hit path).
func methodSerial() uint64 { return globalMethodSerial.Load() }

// bumpMethodSerial invalidates every inline cache. Call it from any path that
// mutates an instance-method table or the ancestry used by lookupMethod.
func bumpMethodSerial() { globalMethodSerial.Add(1) }

// inlineCache is a monomorphic send-site cache: the last receiver class seen at
// this call site and the method that resolved for it, valid while serial still
// equals globalMethodSerial. A miss (different class, or a stale serial) falls
// back to a full lookup and refills. This is the per-call-site method cache
// CRuby/YJIT rely on; it turns the hot monomorphic send (the common case in
// dispatch / fib / proc) from a method-table walk into a pointer compare.
type inlineCache struct {
	serial uint64
	class  *RClass
	method *Method
}

// lookupCached resolves name on recv's class, consulting and refilling the
// per-instruction cache ic. recv must not be an *RClass (class receivers use
// singleton dispatch, which this cache deliberately does not cover — see the
// OpSend fast path). Returns nil exactly when lookupMethod would, so the
// caller's method_missing / operator fallback is preserved.
func (vm *VM) lookupCached(ic *inlineCache, recv object.Value, name string) *Method {
	c := vm.classOf(recv)
	// An object with a singleton class can carry per-object methods, so its
	// dispatch must not be cached against the shared class — resolve it directly.
	if o, ok := recv.(*RObject); ok && o.singleton != nil {
		return undefAsNil(lookupMethod(o.singleton, name))
	}
	serial := methodSerial()
	if ic.class == c && ic.serial == serial {
		return ic.method
	}
	m := undefAsNil(lookupMethod(c, name))
	ic.class, ic.method, ic.serial = c, m, serial
	return m
}

// undefAsNil maps an `undef`-ed tombstone to nil so callers treat the name as
// unresolved (routing to the operator fallback / method_missing), while a real
// method passes through unchanged.
func undefAsNil(m *Method) *Method {
	if m != nil && m.undefined {
		return nil
	}
	return m
}
