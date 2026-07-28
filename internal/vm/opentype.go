// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	opentype "github.com/go-ruby-opentype/opentype"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the `require "opentype"` binding: it wires the
// interpreter-independent github.com/go-ruby-opentype/opentype adapter (the
// Ruby-facing core of the go-opentype text stack — font parsing, sized faces,
// complex-script shaping and the Unicode Bidirectional Algorithm) into rbgo's
// object graph. The adapter exposes a single dynamic entry point,
// Call(receiver, method, args...) (any, error), plus Methods(receiver) listing
// the snake_case names it accepts on each handle (*Module, *Font, *Face) — the
// exact shape an rbgo binding drives from method_missing. Rather than route
// every unknown name through method_missing (which, for a module receiver,
// would have to live on the global Module class), this binding enumerates the
// adapter's Methods once at startup and installs each as a real Ruby method
// whose body is the same thin Call shim, so respond_to? and the method list are
// correct for free. Arguments and results marshal between rbgo object.Values and
// the adapter's Ruby-shaped Go values (Hash=map[string]any, Array=[]any,
// scalars, and the opaque *Font/*Face handles wrapped as Ruby objects).

// OpentypeFont is the Ruby wrapper around the adapter's *opentype.Font handle, a
// parsed TrueType/OpenType font. It reports the Opentype::Font class so its
// query methods (num_glyphs / glyph_index / axes / named_instances / face)
// dispatch through the shared Call shim.
type OpentypeFont struct{ h *opentype.Font }

func (f *OpentypeFont) ToS() string     { return "#<Opentype::Font>" }
func (f *OpentypeFont) Inspect() string { return f.ToS() }
func (f *OpentypeFont) Truthy() bool    { return true }

// OpentypeFace is the Ruby wrapper around the adapter's *opentype.Face handle, a
// font sized at a pixel height. It reports the Opentype::Face class so its
// measurement, metrics, glyph and variation methods dispatch through Call.
type OpentypeFace struct{ h *opentype.Face }

func (f *OpentypeFace) ToS() string     { return "#<Opentype::Face>" }
func (f *OpentypeFace) Inspect() string { return f.ToS() }
func (f *OpentypeFace) Truthy() bool    { return true }

// registerOpentype installs the Opentype module (require "opentype") and its two
// handle classes, Opentype::Font and Opentype::Face. Every method the adapter
// exposes on a receiver becomes a real Ruby method backed by the Call shim: the
// module methods (open_font / parse / most_legible / default_font / load /
// families / visual_order / resolve_levels / shape) are singleton methods on the
// module; the Font and Face methods are instance methods on their classes. A bad
// font blob or an adapter-level failure raises Opentype::Error.
func (vm *VM) registerOpentype() {
	mod := newClass("Opentype", nil)
	mod.isModule = true
	vm.consts["Opentype"] = mod

	std := vm.consts["StandardError"].(*RClass)
	errCls := newClass("Opentype::Error", std)
	mod.consts["Error"] = errCls
	vm.consts["Opentype::Error"] = errCls

	// Opentype.<name> singleton methods dispatch against the package-level
	// adapter Module receiver.
	otMod := opentype.NewModule()
	for _, name := range opentype.Methods(otMod) {
		mname := name
		mod.smethods[mname] = &Method{name: mname, owner: mod,
			native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return vm.otDispatch(otMod, mname, args)
			}}
	}

	// Opentype::Font instance methods dispatch against the wrapped *opentype.Font.
	fontCls := newClass("Opentype::Font", vm.cObject)
	mod.consts["Font"] = fontCls
	vm.consts["Opentype::Font"] = fontCls
	for _, name := range opentype.Methods((*opentype.Font)(nil)) {
		mname := name
		fontCls.define(mname, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.otDispatch(self.(*OpentypeFont).h, mname, args)
		})
	}

	// Opentype::Face instance methods dispatch against the wrapped *opentype.Face.
	faceCls := newClass("Opentype::Face", vm.cObject)
	mod.consts["Face"] = faceCls
	vm.consts["Opentype::Face"] = faceCls
	for _, name := range opentype.Methods((*opentype.Face)(nil)) {
		mname := name
		faceCls.define(mname, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.otDispatch(self.(*OpentypeFace).h, mname, args)
		})
	}
}

// otDispatch is the single Call shim every Opentype method shares: it marshals
// the Ruby arguments to the adapter's Ruby-shaped Go values, invokes
// opentype.Call on recv, and marshals the result back — raising Opentype::Error
// on any adapter-reported failure (a bad font blob, an out-of-range argument).
func (vm *VM) otDispatch(recv any, method string, args []object.Value) object.Value {
	goArgs := make([]any, len(args))
	for i, a := range args {
		goArgs[i] = otValueToAny(a)
	}
	res, err := opentype.Call(recv, method, goArgs...)
	if err != nil {
		return raise("Opentype::Error", "%s", err.Error())
	}
	return otAnyToValue(res)
}

// otValueToAny converts an rbgo object.Value into the Ruby-shaped Go value the
// adapter's Call expects: nil, Bool, Integer and Float scalars, a String (a
// Symbol coerces to its name), an Array to a []any, a Hash to a map[string]any,
// and an Opentype::Font / Opentype::Face wrapper back to its opaque handle. An
// unmappable value raises TypeError.
func otValueToAny(v object.Value) any {
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
			out[i] = otValueToAny(e)
		}
		return out
	case *object.Hash:
		m := make(map[string]any, len(x.Keys))
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			m[otKeyString(k)] = otValueToAny(val)
		}
		return m
	case *OpentypeFont:
		return x.h
	case *OpentypeFace:
		return x.h
	default:
		return raise("TypeError", "cannot pass a %s to Opentype", v.Inspect())
	}
}

// otKeyString renders a Ruby Hash key (an option name) as a String: a String or
// Symbol yields its text, and any other key falls back to its to_s form so an
// options Hash is always usable.
func otKeyString(k object.Value) string {
	switch key := k.(type) {
	case *object.String:
		return key.Str()
	case object.Symbol:
		return string(key)
	default:
		return k.ToS()
	}
}

// otAnyToValue converts a Ruby-shaped Go value returned by the adapter into an
// rbgo object.Value: nil to Nil, the Go scalars to their object.Value peers, a
// []byte (font bytes, glyph coverage) to a binary String, a []any to an Array, a
// map[string]any to a Hash, and the adapter's opaque *Font / *Face handles to
// their Ruby wrappers. A value the adapter never emits raises TypeError.
func otAnyToValue(v any) object.Value {
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
		// A nil slice is the adapter's "not available" (e.g. load of a family whose
		// bytes are not linked in), which reads as Ruby nil; real bytes become a
		// binary String.
		if x == nil {
			return object.NilV
		}
		return object.NewStringBytesEnc(x, "ASCII-8BIT")
	case []any:
		out := make([]object.Value, len(x))
		for i, e := range x {
			out[i] = otAnyToValue(e)
		}
		return object.NewArrayFromSlice(out)
	case map[string]any:
		h := object.NewHash()
		for k, val := range x {
			h.Set(object.NewString(k), otAnyToValue(val))
		}
		return h
	case *opentype.Font:
		return &OpentypeFont{h: x}
	case *opentype.Face:
		return &OpentypeFace{h: x}
	default:
		return raise("TypeError", "Opentype returned an unmappable %T", v)
	}
}
