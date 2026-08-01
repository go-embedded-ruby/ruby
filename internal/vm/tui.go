// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	tui "github.com/go-ruby-widgets/tui"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the `require "tui"` binding: it wires the interpreter-independent
// github.com/go-ruby-widgets/tui adapter (the Ruby-facing core of the go-widgets
// terminal-cell toolkit — labels, buttons, entries, check buttons, list boxes,
// progress bars, containers, splits and notebooks, laid out and rendered to a
// self-contained ANSI frame plus a decoded cell grid) into rbgo's object graph.
// The adapter exposes a single dynamic entry point, Call(receiver, method,
// args...) (any, error), plus Methods(receiver) listing the snake_case names it
// accepts on each receiver kind (its *Module and its opaque *Widget handle). As
// with the opentype binding, this enumerates Methods once and installs each name
// as a real Ruby method: the *Module names are singleton methods on the Tui
// module, the *Widget names are instance methods on Tui::Widget. Arguments and
// results marshal between rbgo object.Values and the adapter's Ruby-shaped Go
// values (Hash=map[string]any, Array=[]any, scalars, and the opaque *Widget
// handle wrapped as a Tui::Widget).

// TuiWidget is the Ruby wrapper around the adapter's *tui.Widget handle — one
// terminal widget or layout container. It reports the Tui::Widget class so its
// mutators, composition verbs and callback wiring dispatch through the shared
// Call shim, and it round-trips as a method argument (a child pane) as well as a
// result.
type TuiWidget struct{ h *tui.Widget }

func (w *TuiWidget) ToS() string     { return "#<Tui::Widget>" }
func (w *TuiWidget) Inspect() string { return w.ToS() }
func (w *TuiWidget) Truthy() bool    { return true }

// registerTui installs the Tui module (require "tui") and its Tui::Widget handle
// class. The module singleton methods are the constructors (label / button /
// entry / check_button / list_box / progress_bar / container / h_split / v_split
// / notebook) plus layout / render / render_cells / decode_cells / bounds /
// set_size / dispatch; the Tui::Widget instance methods are the mutators,
// composition verbs (set_body / set_header / set_left / add_tab …) and callback
// wiring (on_click / on_toggle …). A nil handle or a method used on the wrong
// widget kind raises Tui::Error.
func (vm *VM) registerTui() {
	mod := newClass("Tui", nil)
	mod.isModule = true
	vm.consts["Tui"] = mod

	std := vm.consts["StandardError"].(*RClass)
	errCls := newClass("Tui::Error", std)
	mod.consts["Error"] = errCls
	vm.consts["Tui::Error"] = errCls

	// Tui.<name> singleton methods dispatch against one shared adapter Module,
	// which owns the per-dispatch callback log.
	tm := tui.NewModule()
	for _, name := range tui.Methods(tm) {
		mname := name
		mod.smethods[mname] = &Method{name: mname, owner: mod,
			native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return vm.tuiDispatch(tm, mname, args)
			}}
	}

	// Tui::Widget instance methods dispatch against the wrapped *tui.Widget.
	wCls := newClass("Tui::Widget", vm.cObject)
	mod.consts["Widget"] = wCls
	vm.consts["Tui::Widget"] = wCls
	for _, name := range tui.Methods((*tui.Widget)(nil)) {
		mname := name
		wCls.define(mname, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.tuiDispatch(self.(*TuiWidget).h, mname, args)
		})
	}
}

// tuiDispatch is the single Call shim every Tui method shares: it marshals the
// Ruby arguments to the adapter's Ruby-shaped Go values, invokes tui.Call on
// recv, and marshals the result back — raising Tui::Error on any adapter-reported
// failure (a nil widget, a method used on the wrong widget kind).
func (vm *VM) tuiDispatch(recv any, method string, args []object.Value) object.Value {
	goArgs := make([]any, len(args))
	for i, a := range args {
		goArgs[i] = tuiValueToAny(a)
	}
	res, err := tui.Call(recv, method, goArgs...)
	if err != nil {
		return raise("Tui::Error", "%s", err.Error())
	}
	return tuiAnyToValue(res)
}

// tuiValueToAny converts an rbgo object.Value into the Ruby-shaped Go value the
// adapter's Call expects: nil, Bool, Integer and Float scalars, a String (a
// Symbol coerces to its name), an Array to a []any, a Hash to a map[string]any,
// and a Tui::Widget wrapper back to its opaque *tui.Widget handle (a child pane).
// An unmappable value raises TypeError.
func tuiValueToAny(v object.Value) any {
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
			out[i] = tuiValueToAny(e)
		}
		return out
	case *object.Hash:
		m := make(map[string]any, len(x.Keys))
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			m[tuiKeyString(k)] = tuiValueToAny(val)
		}
		return m
	case *TuiWidget:
		return x.h
	default:
		return raise("TypeError", "cannot pass a %s to Tui", v.Inspect())
	}
}

// tuiKeyString renders a Ruby Hash key (an event field name) as a String: a
// String or Symbol yields its text, and any other key falls back to its to_s
// form so an event Hash is always usable.
func tuiKeyString(k object.Value) string {
	switch key := k.(type) {
	case *object.String:
		return key.Str()
	case object.Symbol:
		return string(key)
	default:
		return k.ToS()
	}
}

// tuiAnyToValue converts a Ruby-shaped Go value returned by the adapter into an
// rbgo object.Value: nil to Nil, the Go scalars to their object.Value peers (the
// ANSI frame is a String, a progress fraction a Float), a []any to an Array, a
// map[string]any to a Hash (bounds, a decoded cell grid, a dispatch result), and
// the opaque *tui.Widget handle to a Tui::Widget wrapper. A value the adapter
// never emits raises TypeError.
func tuiAnyToValue(v any) object.Value {
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
			out[i] = tuiAnyToValue(e)
		}
		return object.NewArrayFromSlice(out)
	case map[string]any:
		h := object.NewHash()
		for k, val := range x {
			h.Set(object.NewString(k), tuiAnyToValue(val))
		}
		return h
	case *tui.Widget:
		return &TuiWidget{h: x}
	default:
		return raise("TypeError", "Tui returned an unmappable %T", v)
	}
}
