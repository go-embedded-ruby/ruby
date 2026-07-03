// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	observer "github.com/go-ruby-observer/observer"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerObservable installs the Observable mixin (require "observer"), MRI's
// observer.rb. Observable is a module mixed into a publisher class (include
// Observable); each including object gets its own observer registry and changed
// flag. The pure-compute state — the ordered observer set, the changed-flag
// lifecycle and the notify decision/reset — is delegated to
// github.com/go-ruby-observer/observer's *Registry; rbgo wires the half the
// library deliberately leaves out: the actual dispatch onto each observer
// (vm.send(observer, method, args...)) and the respond_to? check add_observer
// uses to reject a non-responding observer.
//
// The registry is per-including-object state. It is stashed lazily in the
// receiver's @__observer_state ivar, boxed in an observerBox so it survives
// across method calls but is never user-visible (no Ruby method returns it).
func (vm *VM) registerObservable() {
	mod := newClass("Observable", nil)
	mod.isModule = true
	vm.consts["Observable"] = object.Wrap(mod)

	// add_observer(observer, func=:update): register observer, recording the
	// method to call on notification. Raises NoMethodError (MRI's verbatim
	// message) when the observer does not respond to func; on success returns the
	// func symbol, like MRI's `@observer_peers[observer] = func`.
	mod.define("add_observer", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		obs := args[0]
		fn := observer.DefaultFunc
		if len(args) > 1 {
			fn = methodNameArg(args[1])
		}
		reg := observerRegistry(self)
		respondTo := func(o observer.Observer, name string) bool {
			return vm.respondsTo(o.(object.Value), name)
		}
		if err := reg.AddObserver(obs, fn, respondTo); err != nil {
			// The library returns *NotRespondingError with MRI's exact text; surface
			// it as the NoMethodError MRI's add_observer raises.
			return raise("NoMethodError", "%s", err.Error())
		}
		observerFuncs(self)[obs] = fn
		return object.SymVal(string(object.Symbol(fn)))
	})

	// delete_observer(observer): remove observer; returns the func that was
	// registered for it, or nil when it was not registered, matching MRI's
	// `@observer_peers.delete observer`.
	mod.define("delete_observer", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		obs := args[0]
		funcs := observerFuncs(self)
		fn, had := funcs[obs]
		observerRegistry(self).DeleteObserver(obs)
		delete(funcs, obs)
		if !had {
			return object.NilVal()
		}
		return object.SymVal(string(object.Symbol(fn)))
	})

	// delete_observers: remove every observer; returns the (now empty) hash, as
	// MRI's `@observer_peers.clear` does.
	mod.define("delete_observers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		observerRegistry(self).DeleteObservers()
		clear(observerFuncs(self))
		return object.Wrap(&object.Hash{})
	})

	// count_observers: number of registered observers.
	mod.define("count_observers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(observerRegistry(self).CountObservers()))
	})

	// changed(state=true): set the changed flag; returns the state, matching MRI's
	// `@observer_state = state`.
	mod.define("changed", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		state := true
		if len(args) > 0 {
			state = args[0].Truthy()
		}
		observerRegistry(self).Changed(state)
		return object.BoolValue(bool(object.Bool(state)))
	})

	// changed?: whether the state is marked changed.
	mod.define("changed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(observerRegistry(self).ChangedQ())))
	})

	// notify_observers(*args): when changed?, dispatch each observer's method (in
	// insertion order) with args, then clear the changed flag and return false;
	// when not changed, a no-op returning nil. The library makes the notify
	// decision and hands back the ordered Entry list; rbgo performs the dispatch.
	mod.define("notify_observers", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		entries, _, ok := observerRegistry(self).NotifyObservers()
		if !ok {
			return object.NilVal()
		}
		for _, e := range entries {
			vm.send(e.Observer.(object.Value), e.Func, args, nil)
		}
		// MRI's notify_observers ends with `@observer_state = false`, whose value
		// (false) is the method's result.
		return object.BoolValue(bool(object.Bool(false)))
	})
}

// observerBox stashes the per-object *observer.Registry — plus a parallel
// observer->func table used only to answer delete_observer's MRI return value
// (the library's DeleteObserver is void) — inside the including object's
// @__observer_state ivar. It is never user-visible.
type observerBox struct {
	reg   *observer.Registry
	funcs map[object.Value]string
}

func (b *observerBox) ToS() string     { return "#<Observable state>" }
func (b *observerBox) Inspect() string { return "#<Observable state>" }
func (b *observerBox) Truthy() bool    { return true }

// observerState returns the receiver's observer box, creating and stashing it on
// first use (MRI lazily defines @observer_peers/@observer_state likewise).
func observerState(self object.Value) *observerBox {
	if b, ok := object.KindOK[*observerBox](getIvar(self, "@__observer_state")); ok {
		return b
	}
	b := &observerBox{reg: &observer.Registry{}, funcs: map[object.Value]string{}}
	setIvar(self, "@__observer_state", object.Wrap(b))
	return b
}

// observerRegistry returns the receiver's *observer.Registry.
func observerRegistry(self object.Value) *observer.Registry { return observerState(self).reg }

// observerFuncs returns the receiver's observer->func table.
func observerFuncs(self object.Value) map[object.Value]string { return observerState(self).funcs }

// methodNameArg coerces an add_observer func argument (a Symbol or String) to its
// text. MRI accepts either; a non-name value raises TypeError.
func methodNameArg(v object.Value) string {
	{
		__sw113 := v
		switch {
		case object.IsKind[object.Symbol](__sw113):
			n := object.Kind[object.Symbol](__sw113)
			_ = n
			return string(n)
		case object.IsKind[*object.String](__sw113):
			n := object.Kind[*object.String](__sw113)
			_ = n
			return n.Str()
		default:
			n := __sw113
			_ = n
			raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
			return ""
		}
	}
}
