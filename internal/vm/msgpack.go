// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerMsgpack installs the MessagePack module (require "msgpack"):
// MessagePack.pack / .dump, MessagePack.unpack / .load and Object#to_msgpack. The
// packer and unpacker live in the github.com/go-ruby-msgpack/msgpack library;
// this module is the thin wiring that maps rbgo's object graph to and from the
// library's value model (see msgpack_bind.go). The error tree
// (MessagePack::Error and its Pack/Unpack subclasses) is registered so a
// re-raised library error rescues as the right Ruby class.
func (vm *VM) registerMsgpack() {
	mod := newClass("MessagePack", nil)
	mod.isModule = true
	vm.consts["MessagePack"] = mod
	// The `msgpack` gem exposes the module under both names.
	vm.consts["Msgpack"] = mod
	vm.registerMsgpackErrors(mod)

	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// MessagePack.pack(obj) / .dump(obj) render a value to a binary (ASCII-8BIT)
	// String, matching the gem's wire output.
	pack := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return &object.String{B: msgpackPack(args[0]), Enc: "ASCII-8BIT"}
	}
	def("pack", pack)
	def("dump", pack)

	// MessagePack.unpack(str) / .load(str) parse MessagePack bytes back to a tree
	// of Ruby values. The String's raw bytes are read regardless of encoding.
	unpack := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return msgpackUnpack(vm, msgpackBytesArg(args[0]))
	}
	def("unpack", unpack)
	def("load", unpack)

	// Object#to_msgpack (the gem installs this on Object) returns
	// MessagePack.pack(self).
	vm.cObject.define("to_msgpack", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.String{B: msgpackPack(self), Enc: "ASCII-8BIT"}
	})
}

// registerMsgpackErrors installs the MessagePack::Error exception tree mirroring
// the gem (Error < StandardError; PackError / UnpackError / MalformedFormatError
// < Error). Each class is registered both as a nested constant of MessagePack
// (so Ruby `MessagePack::PackError` resolves it) and under its qualified name in
// the top-level table (so a re-raised library error's exceptionObject lookup
// finds the very same class), exactly as JSON:: classes are.
func (vm *VM) registerMsgpackErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	err := reg("Error", "MessagePack::Error", std)
	reg("PackError", "MessagePack::PackError", err)
	unpackErr := reg("UnpackError", "MessagePack::UnpackError", err)
	reg("MalformedFormatError", "MessagePack::MalformedFormatError", unpackErr)
}

// msgpackBytesArg coerces MessagePack.unpack's argument to raw bytes: a String
// yields its backing bytes verbatim (a binary payload is not re-encoded), and any
// other value its to_s bytes, so a non-String argument does not crash the parser.
func msgpackBytesArg(v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.B
	}
	return []byte(v.ToS())
}
