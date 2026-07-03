// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	addressable "github.com/go-ruby-addressable/addressable"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// AddressableURI wraps a *addressable.URI as a Ruby Addressable::URI object. The
// RFC 3986 parsing, normalization and reference-resolution live in the
// github.com/go-ruby-addressable/addressable library; this shell only reports the
// Ruby class and delegates each method (see addressable_bind.go).
type AddressableURI struct{ u *addressable.URI }

func (u *AddressableURI) ToS() string     { return u.u.String() }
func (u *AddressableURI) Inspect() string { return "#<Addressable::URI:" + u.u.String() + ">" }
func (u *AddressableURI) Truthy() bool    { return true }

// AddressableTemplate wraps a *addressable.Template as a Ruby
// Addressable::Template object.
type AddressableTemplate struct{ t *addressable.Template }

func (t *AddressableTemplate) ToS() string { return t.t.Pattern() }
func (t *AddressableTemplate) Inspect() string {
	return "#<Addressable::Template:" + t.t.Pattern() + ">"
}
func (t *AddressableTemplate) Truthy() bool { return true }

// registerAddressable installs the Addressable module (require
// "addressable/uri"): Addressable::URI.parse, #normalize, #join, #query_values,
// and Addressable::Template#expand / #extract. The value classes report their own
// Ruby class via classOf so instance methods dispatch correctly.
func (vm *VM) registerAddressable() {
	mod := newClass("Addressable", nil)
	mod.isModule = true
	vm.consts["Addressable"] = object.Wrap(mod)

	uriCls := newClass("Addressable::URI", vm.cObject)
	mod.consts["URI"] = object.Wrap(uriCls)
	vm.consts["Addressable::URI"] = object.Wrap(uriCls)

	tmplCls := newClass("Addressable::Template", vm.cObject)
	mod.consts["Template"] = object.Wrap(tmplCls)
	vm.consts["Addressable::Template"] = object.Wrap(tmplCls)

	// Addressable::URI.parse(str) parses a URI string (a nil argument yields nil,
	// matching the gem).
	uriCls.smethods["parse"] = &Method{name: "parse", owner: uriCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			if object.IsNil(args[0]) {
				return object.NilVal()
			}
			return object.Wrap(&AddressableURI{u: addressable.Parse(strArg(args[0]))})
		}}

	vm.registerAddressableURI(uriCls)

	// Addressable::Template.new(pattern) compiles an RFC 6570 template.
	tmplCls.smethods["new"] = &Method{name: "new", owner: tmplCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			return object.Wrap(&AddressableTemplate{t: addressable.NewTemplate(strArg(args[0]))})
		}}
	td := func(name string, fn NativeFn) { tmplCls.define(name, fn) }
	td("pattern", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*AddressableTemplate](self).t.Pattern()))
	})
	td("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*AddressableTemplate](self).t.Pattern()))
	})
	td("expand", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Wrap(&AddressableURI{u: addressable.Parse(object.Kind[*AddressableTemplate](self).t.Expand(addressableVars(args[0])))})
	})
	td("extract", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		m := object.Kind[*AddressableTemplate](self).t.Extract(addressableURIStr(args[0]))
		if m == nil {
			return object.NilVal()
		}
		return anyMapToHash(m)
	})
}

// registerAddressableURI installs the Addressable::URI instance surface: the
// component readers, #normalize, #join and #query_values.
func (vm *VM) registerAddressableURI(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*AddressableURI](self).u.String()))
	})
	d("scheme", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strPtrToRuby(object.Kind[*AddressableURI](self).u.Scheme())
	})
	d("host", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strPtrToRuby(object.Kind[*AddressableURI](self).u.Host())
	})
	d("path", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*AddressableURI](self).u.Path()))
	})
	d("query", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strPtrToRuby(object.Kind[*AddressableURI](self).u.Query())
	})
	d("fragment", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strPtrToRuby(object.Kind[*AddressableURI](self).u.Fragment())
	})
	d("userinfo", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strPtrToRuby(object.Kind[*AddressableURI](self).u.Userinfo())
	})
	d("port", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if p := object.Kind[*AddressableURI](self).u.Port(); p != nil {
			return object.IntValue(int64(*p))
		}
		return object.NilVal()
	})
	d("normalize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&AddressableURI{u: object.Kind[*AddressableURI](self).u.Normalize()})
	})
	d("join", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Wrap(&AddressableURI{u: object.Kind[*AddressableURI](self).u.Join(addressableURIStr(args[0]))})
	})
	d("query_values", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		q := object.Kind[*AddressableURI](self).u.Query()
		if q == nil {
			return object.NilVal()
		}
		h := object.NewHash()
		for _, pair := range object.Kind[*AddressableURI](self).u.QueryValues() {
			h.Set(object.Wrap(object.NewString(pair[0])), object.Wrap(object.NewString(pair[1])))
		}
		return object.Wrap(h)
	})
}
