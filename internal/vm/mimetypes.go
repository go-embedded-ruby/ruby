// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// MIMEType wraps a mimetypes.Type as a Ruby MIME::Type object. The IANA/registry
// data model (content_type / media_type / sub_type / extensions / binary? …)
// lives in the github.com/go-ruby-mime-types/mime-types library; this shell only
// reports the Ruby class and delegates each reader (see mimetypes_bind.go).
type MIMEType struct{ t mimeTypeVal }

func (t *MIMEType) ToS() string     { return t.t.ContentType() }
func (t *MIMEType) Inspect() string { return "#<MIME::Type: " + t.t.ContentType() + ">" }
func (t *MIMEType) Truthy() bool    { return true }

// registerMIMETypes installs the MIME module and its MIME::Types registry facade
// (require "mime/types"): MIME::Types[str] (lookup by content-type),
// MIME::Types.type_for(filename) / .of(filename) (lookup by extension) and the
// MIME::Type value class. The registry and the data itself live in the
// go-ruby-mime-types library; this module is the thin wiring that maps a Ruby
// query String to a registry call and each result to a MIME::Type (see
// mimetypes_bind.go).
func (vm *VM) registerMIMETypes() {
	mimeMod := newClass("MIME", nil)
	mimeMod.isModule = true
	vm.consts["MIME"] = mimeMod

	typesMod := newClass("MIME::Types", nil)
	typesMod.isModule = true
	mimeMod.consts["Types"] = typesMod
	vm.consts["MIME::Types"] = typesMod

	typeCls := newClass("MIME::Type", vm.cObject)
	mimeMod.consts["Type"] = typeCls
	vm.consts["MIME::Type"] = typeCls
	vm.registerMIMEType(typeCls)

	def := func(name string, fn NativeFn) {
		typesMod.smethods[name] = &Method{name: name, owner: typesMod, native: fn}
	}

	// MIME::Types[type_id] looks a content-type string up, returning the
	// priority-sorted Array of matching MIME::Type objects (the gem's [] method).
	def("[]", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return mimeTypeArray(mimeDefault().Get(strArg(args[0])))
	})

	// MIME::Types.type_for(filename) / .of(filename) look up by filename
	// extension, returning the priority-sorted Array of matching MIME::Type.
	typeFor := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return mimeTypeArray(mimeDefault().TypeFor(strArg(args[0])))
	}
	def("type_for", typeFor)
	def("of", typeFor)

	// MIME::Types.count returns the size of the registry (the gem's Enumerable
	// count / registry length).
	def("count", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(mimeDefault().Len()))
	})
}

// registerMIMEType installs the MIME::Type instance surface: the identity readers
// (content_type / media_type / sub_type / simplified), the extension views and
// the binary? / ascii? / registered? / obsolete? predicates.
func (vm *VM) registerMIMEType(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) mimeTypeVal { return object.Kind[*MIMEType](v).t }

	str := func(name string, get func(mimeTypeVal) string) {
		d(name, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NewString(get(self(v)))
		})
	}
	str("content_type", mimeTypeVal.ContentType)
	str("to_s", mimeTypeVal.ContentType)
	str("media_type", mimeTypeVal.MediaType)
	str("sub_type", mimeTypeVal.SubType)
	str("simplified", mimeTypeVal.Simplified)
	str("friendly", mimeTypeVal.Friendly)
	str("encoding", mimeTypeVal.Encoding)

	// extensions returns the Array of registered filename extensions.
	d("extensions", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return strSliceToArray(self(v).Extensions())
	})
	// preferred_extension returns the first/preferred extension, or nil when the
	// type registers none (the gem returns nil).
	d("preferred_extension", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if ext := self(v).PreferredExtension(); ext != "" {
			return object.NewString(ext)
		}
		return object.NilV
	})

	boolean := func(name string, get func(mimeTypeVal) bool) {
		d(name, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Bool(get(self(v)))
		})
	}
	boolean("binary?", mimeTypeVal.Binary)
	boolean("ascii?", mimeTypeVal.ASCII)
	boolean("registered?", mimeTypeVal.Registered)
	boolean("obsolete?", mimeTypeVal.Obsolete)
	boolean("complete?", mimeTypeVal.Complete)
	boolean("signature?", mimeTypeVal.Signature)
	boolean("provisional?", mimeTypeVal.Provisional)
}
