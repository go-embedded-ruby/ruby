// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	widgets "github.com/go-ruby-widgets/widgets"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the `require "widgets"` binding: it wires the
// interpreter-independent github.com/go-ruby-widgets/widgets adapter (the
// Ruby-facing core of the go-widgets pixel-blitting UI toolkit — flex/fixed box,
// grid, border and card layouts, the full widget set, software rendering to an
// RGBA buffer and event dispatch) into rbgo's object graph. Unlike the stateless
// data adapters (opentype, sass, …) the widgets adapter owns a live widget tree:
// every constructed widget or container is stored under an integer handle the
// Ruby side references, so a Widgets method takes and returns plain Integers,
// Hashes, Arrays and scalars — there are no opaque handle classes. The adapter
// exposes a single dynamic entry point, Call(receiver, method, args...) (any,
// error), plus Methods(receiver) listing the snake_case names it accepts on the
// module receiver. This binding enumerates Methods once at startup and installs
// each as a real singleton method on the Widgets module whose body is the same
// thin Call shim, so respond_to? and the method list are correct for free.
// Arguments and results marshal between rbgo object.Values and the adapter's
// Ruby-shaped Go values (Hash=map[string]any, Array=[]any, scalars, the RGBA
// pixel buffer as a binary String).

// registerWidgets installs the Widgets module (require "widgets") over a single,
// process-wide adapter Module that owns the widget-handle table. Every method
// the adapter exposes on that receiver — the constructors (button / label /
// entry / text_view / check_button / drop_down / list_box / menu / menu_bar /
// container / h_box / v_box / grid / frame / dock / border), the mutators
// (set_text / set_checked / select / set_style / set_spacing / set_theme …), the
// composition verbs (add_widget / add / add_fixed / add_flex / attach / dock_at
// / set_region / add_menu / set_active / set_layout), and layout / query / render
// / dispatch (layout / set_bounds / bounds / render / dispatch) — becomes a real
// singleton method backed by the Call shim. An unknown handle, an out-of-range
// argument or an unknown layout/region/style raises Widgets::Error.
func (vm *VM) registerWidgets() {
	mod := newClass("Widgets", nil)
	mod.isModule = true
	vm.consts["Widgets"] = mod

	std := vm.consts["StandardError"].(*RClass)
	errCls := newClass("Widgets::Error", std)
	mod.consts["Error"] = errCls
	vm.consts["Widgets::Error"] = errCls

	// Widgets.<name> singleton methods dispatch against one shared adapter Module,
	// which holds the live widget tree behind the integer handles Ruby passes back.
	wm := widgets.NewModule()
	for _, name := range widgets.Methods(wm) {
		mname := name
		mod.smethods[mname] = &Method{name: mname, owner: mod,
			native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return vm.wdgDispatch(wm, mname, args)
			}}
	}
}

// wdgDispatch is the single Call shim every Widgets method shares: it marshals
// the Ruby arguments to the adapter's Ruby-shaped Go values, invokes
// widgets.Call on recv, and marshals the result back — raising Widgets::Error on
// any adapter-reported failure (an unknown handle, a bad layout name, a
// non-positive render size).
func (vm *VM) wdgDispatch(recv any, method string, args []object.Value) object.Value {
	goArgs := make([]any, len(args))
	for i, a := range args {
		goArgs[i] = wdgValueToAny(a)
	}
	res, err := widgets.Call(recv, method, goArgs...)
	if err != nil {
		return raise("Widgets::Error", "%s", err.Error())
	}
	return wdgAnyToValue(res)
}

// wdgValueToAny converts an rbgo object.Value into the Ruby-shaped Go value the
// adapter's Call expects: nil, Bool, Integer and Float scalars, a String (a
// Symbol coerces to its name), an Array to a []any and a Hash to a
// map[string]any. An unmappable value raises TypeError.
func wdgValueToAny(v object.Value) any {
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
			out[i] = wdgValueToAny(e)
		}
		return out
	case *object.Hash:
		m := make(map[string]any, len(x.Keys))
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			m[wdgKeyString(k)] = wdgValueToAny(val)
		}
		return m
	default:
		return raise("TypeError", "cannot pass a %s to Widgets", v.Inspect())
	}
}

// wdgKeyString renders a Ruby Hash key (an option name) as a String: a String or
// Symbol yields its text, and any other key falls back to its to_s form so an
// options Hash is always usable.
func wdgKeyString(k object.Value) string {
	switch key := k.(type) {
	case *object.String:
		return key.Str()
	case object.Symbol:
		return string(key)
	default:
		return k.ToS()
	}
}

// wdgAnyToValue converts a Ruby-shaped Go value returned by the adapter into an
// rbgo object.Value: nil to Nil, the Go scalars to their object.Value peers, a
// []byte (the RGBA render buffer) to a binary String, a []any to an Array and a
// map[string]any to a Hash. A value the adapter never emits raises TypeError.
func wdgAnyToValue(v any) object.Value {
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
	case []byte:
		return object.NewStringBytesEnc(x, "ASCII-8BIT")
	case []any:
		out := make([]object.Value, len(x))
		for i, e := range x {
			out[i] = wdgAnyToValue(e)
		}
		return object.NewArrayFromSlice(out)
	case map[string]any:
		h := object.NewHash()
		for k, val := range x {
			h.Set(object.NewString(k), wdgAnyToValue(val))
		}
		return h
	default:
		return raise("TypeError", "Widgets returned an unmappable %T", v)
	}
}
