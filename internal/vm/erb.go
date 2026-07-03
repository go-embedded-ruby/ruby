// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
	erb "github.com/go-ruby-erb/erb"
)

// registerERB installs the native backbone of the ERB standard library: the ERB
// class with a private native compiler hook and the ERB::Util module of escaping
// helpers. The template scanner/compiler lives entirely in the pure-Go
// github.com/go-ruby-erb/erb library (mirroring MRI's ERB::Compiler byte for
// byte); the interpreter-bound pieces — initialize / result / result_with_hash —
// stay in Ruby (prelude.rb) because they need a Binding and eval. ERB::Util's
// pure functions (html_escape / url_encode and their h / u aliases) bind
// straight through to the library so the escaping output also comes from one
// authoritative source.
//
// The class and module are created here so the native hook __compile can be a
// private instance method; the prelude reopens ERB to add the public API.
func (vm *VM) registerERB() {
	cERB := newClass("ERB", vm.cObject)
	vm.consts["ERB"] = cERB

	// __compile(template, trim_mode, eoutvar) -> src. The single bridge into the
	// library: it returns the Ruby source the compiled template evaluates to
	// (already carrying the "#coding:UTF-8\n" magic prefix). A nil trim_mode is
	// the no-trim default; eoutvar names the output buffer. The library never
	// fails on template syntax, but a malformed-option error is surfaced as an
	// ArgumentError so the contract stays total on the Ruby side.
	cERB.methods["__compile"] = &Method{name: "__compile", owner: cERB, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		template := strArg(args[0])
		trimMode := optStrArg(args[1])
		eoutvar := strArg(args[2])
		src, _, err := erbCompile(template, erb.Options{TrimMode: trimMode, EOutVar: eoutvar})
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return object.NewString(src)
	}}

	// ERB::Util — html_escape / url_encode (with the one-letter h / u aliases)
	// bound to the library's pure functions, callable as module functions
	// (ERB::Util.h(...)). MRI coerces non-strings with to_s first; the conversion
	// happens here via toS so the library sees a String.
	util := newClass("Util", nil)
	util.isModule = true
	cERB.consts["Util"] = util
	udef := func(name string, fn NativeFn) { util.smethods[name] = &Method{name: name, owner: util, native: fn} }

	htmlEscape := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(erb.HTMLEscape(erbToS(vm, args[0])))
	}
	urlEncode := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(erb.URLEncode(erbToS(vm, args[0])))
	}
	udef("html_escape", htmlEscape)
	udef("h", htmlEscape)
	udef("url_encode", urlEncode)
	udef("u", urlEncode)
}

// erbCompile is the seam over the go-ruby-erb compiler. The library never fails
// on a well-formed template (Compile's err is reserved for genuinely malformed
// options, unreachable through this binding), so the error branch is exercised
// only by swapping this var in a fault-injection test.
var erbCompile = erb.Compile

// optStrArg returns the String value of v, or "" when v is nil — ERB's trim_mode
// keyword defaults to nil (no trimming), which the library spells as the empty
// trim string.
func optStrArg(v object.Value) string {
	if _, isNil := v.(object.Nil); isNil {
		return ""
	}
	return strArg(v)
}

// erbToS coerces an ERB::Util argument to its String form the way MRI does,
// invoking #to_s on non-strings (so nil -> "", 123 -> "123"). A String is used
// verbatim.
func erbToS(vm *VM, v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return string(s.Bytes())
	}
	return vm.send(v, "to_s", nil, nil).(*object.String).Str()
}
