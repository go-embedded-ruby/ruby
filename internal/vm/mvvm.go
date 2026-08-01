// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	mvvm "github.com/go-ruby-widgets/mvvm"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the `require "mvvm"` binding: it wires the interpreter-independent
// github.com/go-ruby-widgets/mvvm adapter (the Ruby-facing core of the go-widgets
// data-binding layer — Observable, Command and ObservableList over a shared
// event queue) into rbgo's object graph. Because a hosted Go library cannot call
// back into the Ruby runtime, every notification is appended to the module's
// queue as a Ruby-shaped Hash; the Ruby side drains it with Mvvm.drain_events on
// each UI tick and dispatches each Hash to the callback named by its
// "callback_id". The adapter exposes a single dynamic entry point, Call(receiver,
// method, args...) (any, error), plus Methods(receiver) listing the snake_case
// names it accepts on each receiver kind (its *Module and the *Observable /
// *Command / *ObservableList handles). This enumerates Methods once and installs
// each name as a real Ruby method: the *Module names are singleton methods on the
// Mvvm module, the handle names are instance methods on Mvvm::Observable /
// Mvvm::Command / Mvvm::ObservableList. Arguments and results marshal between
// rbgo object.Values and the adapter's Ruby-shaped Go values (Hash=map[string]any,
// Array=[]any, scalars, and the opaque handles wrapped as their Ruby classes).

// MvvmObservable is the Ruby wrapper around the adapter's *mvvm.Observable handle
// (a single observed value with drain-backed subscriptions). It reports the
// Mvvm::Observable class so get / set / subscribe / unsubscribe dispatch through
// the shared Call shim.
type MvvmObservable struct{ h *mvvm.Observable }

func (o *MvvmObservable) ToS() string     { return "#<Mvvm::Observable>" }
func (o *MvvmObservable) Inspect() string { return o.ToS() }
func (o *MvvmObservable) Truthy() bool    { return true }

// MvvmCommand is the Ruby wrapper around the adapter's *mvvm.Command handle (an
// action whose executability and body are owned by Ruby). It reports the
// Mvvm::Command class so can_execute / execute / set_can_execute /
// raise_can_execute_changed dispatch through Call.
type MvvmCommand struct{ h *mvvm.Command }

func (c *MvvmCommand) ToS() string     { return "#<Mvvm::Command>" }
func (c *MvvmCommand) Inspect() string { return c.ToS() }
func (c *MvvmCommand) Truthy() bool    { return true }

// MvvmList is the Ruby wrapper around the adapter's *mvvm.ObservableList handle (a
// mutable, observed collection). It reports the Mvvm::ObservableList class so
// add / insert / remove_at / set / move / clear / get / size / slice / observe /
// unobserve dispatch through Call.
type MvvmList struct{ h *mvvm.ObservableList }

func (l *MvvmList) ToS() string     { return "#<Mvvm::ObservableList>" }
func (l *MvvmList) Inspect() string { return l.ToS() }
func (l *MvvmList) Truthy() bool    { return true }

// registerMvvm installs the Mvvm module (require "mvvm") and its three handle
// classes over one shared adapter Module that owns the event queue. The module
// singleton methods are the factories (observable / command / observable_list)
// plus drain_events; the handle instance methods are the primitives' own surface.
// An out-of-range ObservableList.get raises Mvvm::Error.
func (vm *VM) registerMvvm() {
	mod := newClass("Mvvm", nil)
	mod.isModule = true
	vm.consts["Mvvm"] = mod

	std := vm.consts["StandardError"].(*RClass)
	errCls := newClass("Mvvm::Error", std)
	mod.consts["Error"] = errCls
	vm.consts["Mvvm::Error"] = errCls

	// Mvvm.<name> singleton methods dispatch against one shared adapter Module,
	// whose event queue drain_events drains; every handle it makes enqueues there.
	mm := mvvm.NewModule()
	for _, name := range mvvm.Methods(mm) {
		mname := name
		mod.smethods[mname] = &Method{name: mname, owner: mod,
			native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return vm.mvvmDispatch(mm, mname, args)
			}}
	}

	// The three handle classes: each name the adapter exposes on the handle becomes
	// an instance method dispatching against the wrapped Go handle.
	obsCls := newClass("Mvvm::Observable", vm.cObject)
	mod.consts["Observable"] = obsCls
	vm.consts["Mvvm::Observable"] = obsCls
	for _, name := range mvvm.Methods((*mvvm.Observable)(nil)) {
		mname := name
		obsCls.define(mname, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.mvvmDispatch(self.(*MvvmObservable).h, mname, args)
		})
	}

	cmdCls := newClass("Mvvm::Command", vm.cObject)
	mod.consts["Command"] = cmdCls
	vm.consts["Mvvm::Command"] = cmdCls
	for _, name := range mvvm.Methods((*mvvm.Command)(nil)) {
		mname := name
		cmdCls.define(mname, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.mvvmDispatch(self.(*MvvmCommand).h, mname, args)
		})
	}

	listCls := newClass("Mvvm::ObservableList", vm.cObject)
	mod.consts["ObservableList"] = listCls
	vm.consts["Mvvm::ObservableList"] = listCls
	for _, name := range mvvm.Methods((*mvvm.ObservableList)(nil)) {
		mname := name
		listCls.define(mname, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.mvvmDispatch(self.(*MvvmList).h, mname, args)
		})
	}
}

// mvvmDispatch is the single Call shim every Mvvm method shares: it marshals the
// Ruby arguments to the adapter's Ruby-shaped Go values, invokes mvvm.Call on
// recv, and marshals the result back — raising Mvvm::Error on any adapter-reported
// failure (an out-of-range list index).
func (vm *VM) mvvmDispatch(recv any, method string, args []object.Value) object.Value {
	goArgs := make([]any, len(args))
	for i, a := range args {
		goArgs[i] = mvvmValueToAny(a)
	}
	res, err := mvvm.Call(recv, method, goArgs...)
	if err != nil {
		return raise("Mvvm::Error", "%s", err.Error())
	}
	return mvvmAnyToValue(res)
}

// mvvmValueToAny converts an rbgo object.Value into the Ruby-shaped Go value the
// adapter's Call expects: an observed value, a collection item or a callback
// identifier — nil, Bool, Integer and Float scalars, a String (a Symbol coerces
// to its name), an Array to a []any and a Hash to a map[string]any. The handles
// are results, never arguments. An unmappable value raises TypeError.
func mvvmValueToAny(v object.Value) any {
	switch x := v.(type) {
	case object.Nil:
		return nil
	case object.Bool:
		return bool(x)
	case object.Integer:
		return int(x)
	case object.Float:
		return float64(x)
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	case *object.Array:
		out := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = mvvmValueToAny(e)
		}
		return out
	case *object.Hash:
		m := make(map[string]any, len(x.Keys))
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			m[mvvmKeyString(k)] = mvvmValueToAny(val)
		}
		return m
	default:
		return raise("TypeError", "cannot pass a %s to Mvvm", v.Inspect())
	}
}

// mvvmKeyString renders a Ruby Hash key as a String: a String or Symbol yields
// its text, and any other key falls back to its to_s form so a Hash-valued
// observed value is always usable.
func mvvmKeyString(k object.Value) string {
	switch key := k.(type) {
	case *object.String:
		return key.Str()
	case object.Symbol:
		return string(key)
	default:
		return k.ToS()
	}
}

// mvvmAnyToValue converts a Ruby-shaped Go value returned by the adapter into an
// rbgo object.Value: nil to Nil, the Go scalars to their object.Value peers, a
// []any to an Array (a Slice or a drained event batch), a map[string]any to a
// Hash (one drained event), and the opaque *Observable / *Command /
// *ObservableList handles to their Ruby wrappers. A value the adapter never emits
// raises TypeError.
func mvvmAnyToValue(v any) object.Value {
	switch x := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(x)
	case int:
		return object.IntValue(int64(x))
	case int64:
		return object.IntValue(x)
	case float64:
		return object.Float(x)
	case string:
		return object.NewString(x)
	case []any:
		out := make([]object.Value, len(x))
		for i, e := range x {
			out[i] = mvvmAnyToValue(e)
		}
		return object.NewArrayFromSlice(out)
	case map[string]any:
		h := object.NewHash()
		for k, val := range x {
			h.Set(object.NewString(k), mvvmAnyToValue(val))
		}
		return h
	case *mvvm.Observable:
		return &MvvmObservable{h: x}
	case *mvvm.Command:
		return &MvvmCommand{h: x}
	case *mvvm.ObservableList:
		return &MvvmList{h: x}
	default:
		return raise("TypeError", "Mvvm returned an unmappable %T", v)
	}
}
