//go:build js && wasm

// The interactive JS bridge: a Ruby `JS` module (and `JS::Ref` handle objects)
// that let Ruby running in the browser reach the page's DOM and Canvas — draw,
// register event handlers, and drive an animation loop. This is the foundation
// for an in-browser, Ruby-written window manager / compositor.
//
// Status: this compiles for GOOS=js GOARCH=wasm and the native build is
// unaffected (jsbridge_native.go is a no-op). Browser behaviour is validated
// manually (no headless browser in CI), so treat it as a working prototype.

package vm

import (
	"math"
	"syscall/js"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// jsHandles holds the live js.Value behind each JS::Ref, keyed by an integer
// handle stored in the ref's @h ivar. (A JS value cannot itself be an
// object.Value, so Ruby holds an opaque handle.)
var (
	jsHandles = map[int64]js.Value{}
	jsNext    int64
	cJSRef    *RClass
)

// jsWrap turns a js.Value into a Ruby value: primitives convert directly, objects
// and functions become a JS::Ref carrying a fresh handle.
func jsWrap(v js.Value) object.Value {
	switch v.Type() {
	case js.TypeNull, js.TypeUndefined:
		return object.NilV
	case js.TypeBoolean:
		return object.Bool(v.Bool())
	case js.TypeNumber:
		f := v.Float()
		if f == math.Trunc(f) && !math.IsInf(f, 0) {
			return object.Integer(int64(f))
		}
		return object.Float(f)
	case js.TypeString:
		return object.NewString(v.String())
	}
	jsNext++
	jsHandles[jsNext] = v
	return &RObject{class: cJSRef, ivars: map[string]object.Value{"@h": object.Integer(jsNext)}}
}

// jsUnwrap turns a Ruby value into a js argument: a JS::Ref resolves to its
// js.Value, primitives convert, a Ruby block is not unwrapped here.
func jsUnwrap(v object.Value) any {
	switch x := v.(type) {
	case *RObject:
		if x.class == cJSRef {
			return jsHandles[int64(x.ivars["@h"].(object.Integer))]
		}
	case object.Integer:
		return int64(x)
	case object.Float:
		return float64(x)
	case *object.String:
		return x.Str()
	case object.Bool:
		return bool(x)
	}
	return js.Null()
}

func jsRefValue(self object.Value) js.Value {
	return jsHandles[int64(self.(*RObject).ivars["@h"].(object.Integer))]
}

func jsArgs(args []object.Value) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = jsUnwrap(a)
	}
	return out
}

func (vm *VM) registerJSBridge() {
	cJSRef = newClass("JS::Ref", vm.cObject)
	// JS::Ref — a handle to a JS object. get/set/call/[]/on operate on it.
	cJSRef.define("get", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return jsWrap(jsRefValue(self).Get(args[0].ToS()))
	})
	cJSRef.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return jsWrap(jsRefValue(self).Get(args[0].ToS()))
	})
	cJSRef.define("set", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		jsRefValue(self).Set(args[0].ToS(), jsUnwrap(args[1]))
		return args[1]
	})
	cJSRef.define("call", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return jsWrap(jsRefValue(self).Call(args[0].ToS(), jsArgs(args[1:])...))
	})
	cJSRef.define("on", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		target := jsRefValue(self)
		fn := js.FuncOf(func(_ js.Value, cb []js.Value) any {
			var evt object.Value = object.NilV
			if len(cb) > 0 {
				evt = jsWrap(cb[0])
			}
			vm.callBlock(blk, []object.Value{evt})
			return nil
		})
		target.Call("addEventListener", args[0].ToS(), fn)
		return self
	})

	mod := newClass("JS", nil)
	mod.isModule = true
	vm.consts["JS"] = mod
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	def("global", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return jsWrap(js.Global())
	})
	def("window", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return jsWrap(js.Global().Get("window"))
	})
	def("document", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return jsWrap(js.Global().Get("document"))
	})
	def("log", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		js.Global().Get("console").Call("log", jsArgs(args)...)
		return object.NilV
	})
	// JS.raf { |t| … } schedules the block on the next animation frame; reschedule
	// from inside the block for a render loop.
	def("raf", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		var fn js.Func
		fn = js.FuncOf(func(_ js.Value, cb []js.Value) any {
			fn.Release()
			var t object.Value = object.NilV
			if len(cb) > 0 {
				t = jsWrap(cb[0])
			}
			vm.callBlock(blk, []object.Value{t})
			return nil
		})
		js.Global().Call("requestAnimationFrame", fn)
		return object.NilV
	})
}
