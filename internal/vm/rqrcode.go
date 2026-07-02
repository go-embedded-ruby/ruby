// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rqrcode "github.com/go-ruby-rqrcode/rqrcode"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// RQRCode is the Ruby wrapper around a *rqrcode.QRCode. RQRCode::QRCode.new(data,
// level:, size:, mode:) builds one, and its methods mirror the rqrcode gem:
// #modules / #to_a return the module matrix as an Array of Arrays of booleans,
// #checked?(row, col) reports a single dark module, and #as_svg / #as_ansi /
// #as_html / #to_s render it. The generator and renderers live entirely in the
// github.com/go-ruby-rqrcode/rqrcode library (see rqrcode_bind.go); this file is
// the thin wiring plus the RQRCode module namespace.
type RQRCode struct {
	q *rqrcode.QRCode
}

func (r *RQRCode) ToS() string     { return "#<RQRCode::QRCode>" }
func (r *RQRCode) Inspect() string { return "#<RQRCode::QRCode>" }
func (r *RQRCode) Truthy() bool    { return true }

// registerRQRCode installs the RQRCode module and its RQRCode::QRCode class
// (require "rqrcode"): RQRCode::QRCode.new(data, level:, size:, mode:) plus the
// instance renderers. An out-of-range or malformed request raises
// RQRCode::QRCodeArgumentError / RQRCode::QRCodeRunTimeError, the gem's error
// tree, so a caller rescues as the right Ruby class.
func (vm *VM) registerRQRCode() {
	mod := newClass("RQRCode", nil)
	mod.isModule = true
	vm.consts["RQRCode"] = mod
	vm.registerRQRCodeErrors(mod)

	cls := newClass("RQRCode::QRCode", vm.cObject)
	mod.consts["QRCode"] = cls
	vm.consts["RQRCode::QRCode"] = cls

	// RQRCode::QRCode.new(data, level:, size:, mode:, max_size:) builds a QR code.
	// The keyword options mirror the gem: :level (:l/:m/:q/:h), :size (version
	// 1..40), :mode (:number/:alphanumeric/:byte_8bit), :max_size.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			var opt object.Value
			if len(args) > 1 {
				opt = args[1]
			}
			return &RQRCode{q: rqrcodeNew(rqrcodeDataArg(args[0]), opt)}
		}}

	// #qrcode returns the receiver itself: the rqrcode gem exposes the core code
	// under RQRCode::QRCode#qrcode (an RQRCodeCore::QRCode), so callers write
	// `qr.qrcode.modules`; here the wrapper is the same object.
	cls.define("qrcode", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})

	// #modules / #to_a return the module matrix as an Array of Arrays of booleans.
	modules := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rqrcodeModules(self.(*RQRCode).q)
	}
	cls.define("modules", modules)
	cls.define("to_a", modules)

	// #module_count returns the side length in modules.
	cls.define("module_count", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self.(*RQRCode).q.ModuleCount)
	})

	// #version returns the QR version (1..40).
	cls.define("version", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self.(*RQRCode).q.Version)
	})

	// #checked?(row, col) reports whether the module at (row, col) is dark. An
	// out-of-range pair raises RQRCode::QRCodeRunTimeError, matching the gem.
	cls.define("checked?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		on, err := self.(*RQRCode).q.Checked(int(intArg(args[0])), int(intArg(args[1])))
		if err != nil {
			raise("RQRCode::QRCodeRunTimeError", "%s", rqrcodeErrMsg(err))
		}
		return object.Bool(on)
	})

	// #as_svg(options = {}) renders an SVG document.
	cls.define("as_svg", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(rqrcodeAsSVG(self.(*RQRCode).q, rqrcodeOptArg(args)))
	})

	// #as_ansi(options = {}) renders with ANSI background colors.
	cls.define("as_ansi", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(rqrcodeAsANSI(self.(*RQRCode).q, rqrcodeOptArg(args)))
	})

	// #as_html renders the code as an HTML table.
	cls.define("as_html", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*RQRCode).q.AsHTML())
	})

	// #to_s(options = {}) / #as_ansi renders the matrix as text (dark "x").
	toS := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(rqrcodeToString(self.(*RQRCode).q, rqrcodeOptArg(args)))
	}
	cls.define("to_s", toS)
}

// registerRQRCodeErrors installs the RQRCode error tree mirroring the gem
// (QRCodeArgumentError / QRCodeRunTimeError < StandardError). Each class is
// registered both as a nested constant of RQRCode (so Ruby
// `RQRCode::QRCodeArgumentError` resolves it) and under its qualified name in the
// top-level table (so a re-raised library error's exceptionObject lookup finds
// the same class), exactly as the TomlRB:: classes are.
func (vm *VM) registerRQRCodeErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	reg("QRCodeArgumentError", "RQRCode::QRCodeArgumentError", std)
	reg("QRCodeRunTimeError", "RQRCode::QRCodeRunTimeError", std)
}

// rqrcodeDataArg coerces the data argument to its string: a String yields its
// contents, and any other value its to_s.
func rqrcodeDataArg(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// rqrcodeOptArg returns the trailing options Hash of a renderer call, or nil when
// none was supplied.
func rqrcodeOptArg(args []object.Value) *object.Hash {
	if len(args) == 0 {
		return nil
	}
	h, _ := args[len(args)-1].(*object.Hash)
	return h
}
