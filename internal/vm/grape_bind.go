// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	grape "github.com/go-ruby-grape/grape"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the Grape::Router verb DSL + #match, the Grape::Validator
// `params do … end` DSL + #validate, and the Grape::Formatter surface, over the
// go-ruby-grape library. Route matching, params coercion and response formatting
// are deterministic Go; binding an endpoint block to a live Ruby object and
// parsing the Rack env is the host seam, so a route handler is any Ruby value the
// router hands back verbatim on a match.

// registerGrapeRouter installs Grape::Router and its Route / Match value objects.
func (vm *VM) registerGrapeRouter(mod *RClass) {
	cls := newClass("Grape::Router", vm.cObject)
	mod.consts["Router"] = object.Wrap(cls)
	vm.consts["Grape::Router"] = object.Wrap(cls)

	route := newClass("Grape::Router::Route", vm.cObject)
	cls.consts["Route"] = object.Wrap(route)
	vm.consts["Grape::Router::Route"] = object.Wrap(route)
	match := newClass("Grape::Router::Match", vm.cObject)
	cls.consts["Match"] = object.Wrap(match)
	vm.consts["Grape::Router::Match"] = object.Wrap(match)

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&GrapeRouter{rt: grape.NewRouter()})
	}}

	self := func(v object.Value) *GrapeRouter { return object.Kind[*GrapeRouter](v) }
	// add wires one verb: #get/#post/… declare a route whose handler is any Ruby
	// value (kept alongside the library route so #match hands the same object back).
	add := func(method string) NativeFn {
		return func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			pattern := grapeStr(args[0])
			var handler object.Value = object.NilVal()
			if len(args) > 1 {
				handler = args[1]
			} else if blk != nil {
				handler = object.Wrap(blk)
			}
			r := grape.NewRoute(method, pattern, handler)
			self(v).rt.Add(r)
			return object.Wrap(&GrapeRoute{rt: r, handler: handler})
		}
	}
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"} {
		cls.define(grapeVerbName(m), add(m))
	}

	// #match(method, path) resolves the request to a Match.
	cls.define("match", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		m := self(v).rt.Match(grapeStr(args[0]), grapeStr(args[1]))
		gm := &GrapeMatch{m: m}
		if m.Route != nil {
			h, _ := m.Route.Handler.(object.Value)
			gm.handler = h
			gm.route = object.Wrap(&GrapeRoute{rt: m.Route, handler: h})
		}
		return object.Wrap(gm)
	})

	vm.registerGrapeRoute(route)
	vm.registerGrapeMatch(match)
}

// registerGrapeRoute installs the Route instance surface: #http_method /
// #pattern / #handler.
func (vm *VM) registerGrapeRoute(cls *RClass) {
	self := func(v object.Value) *GrapeRoute { return object.Kind[*GrapeRoute](v) }
	cls.define("http_method", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).rt.Method))
	})
	cls.define("pattern", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).rt.Pattern))
	})
	cls.define("handler", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if h := self(v).handler; !object.IsNil(h) {
			return h
		}
		return object.NilVal()
	})
}

// registerGrapeMatch installs the Match instance surface: #status (a Symbol),
// #ok? / #not_found? / #method_not_allowed?, #route, #params, #allowed.
func (vm *VM) registerGrapeMatch(cls *RClass) {
	self := func(v object.Value) *GrapeMatch { return object.Kind[*GrapeMatch](v) }
	cls.define("status", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.SymVal(string(object.Symbol(grapeStatusName(self(v).m.Status))))
	})
	cls.define("ok?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).m.Status == grape.StatusOK)))
	})
	cls.define("not_found?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).m.Status == grape.StatusNotFound)))
	})
	cls.define("method_not_allowed?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).m.Status == grape.StatusMethodNotAllowed)))
	})
	cls.define("route", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if r := self(v).route; !object.IsNil(r) {
			return r
		}
		return object.NilVal()
	})
	cls.define("handler", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if h := self(v).handler; !object.IsNil(h) {
			return h
		}
		return object.NilVal()
	})
	// #params returns the captured path params as a String=>String Hash.
	cls.define("params", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		for k, val := range self(v).m.Params {
			h.Set(object.Wrap(object.NewString(k)), object.Wrap(object.NewString(val)))
		}
		return object.Wrap(h)
	})
	// #allowed lists the methods a path accepts (the 405 Allow header set).
	cls.define("allowed", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		allowed := self(v).m.Allowed
		arr := object.NewArrayFromSlice(make([]object.Value, len(allowed)))
		for i, a := range allowed {
			arr.Elems[i] = object.Wrap(object.NewString(a))
		}
		return object.Wrap(arr)
	})
}

// registerGrapeValidator installs Grape::Validator.new { params DSL } and its
// #validate instance method.
func (vm *VM) registerGrapeValidator(mod *RClass) {
	cls := newClass("Grape::Validator", vm.cObject)
	mod.consts["Validator"] = object.Wrap(cls)
	vm.consts["Grape::Validator"] = object.Wrap(cls)

	scope := newClass("Grape::Validations::ParamsScope", vm.cObject)
	vm.consts["Grape::Validations::ParamsScope"] = object.Wrap(scope)

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		set := &grape.ParamSet{}
		if blk != nil {
			vm.callBlockSelf(blk, object.Wrap(&GrapeParamsBuilder{set: set}), nil)
		}
		return object.Wrap(&GrapeValidator{set: set, v: grape.NewParamsValidator(set)})
	}}

	cls.define("validate", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		raw := grapeRawHash(args[0])
		gv := object.Kind[*GrapeValidator](v)
		coerced, verr := gv.v.Validate(raw)
		if verr != nil && !verr.Empty() {
			raise("Grape::Exceptions::ValidationErrors", "%s", verr.Error())
		}
		return object.Wrap(grapeCoercedToHash(vm, gv.set, coerced))
	})

	vm.registerGrapeParamsBuilder()
}

// registerGrapeParamsBuilder installs the `params do … end` DSL receiver:
// #requires and #optional append a Param built from the name plus its options
// Hash (type / values / regexp / default / length).
func (vm *VM) registerGrapeParamsBuilder() {
	dsl := newClass("Grape::Validations::ParamsScope::DSL", vm.cObject)
	vm.consts["Grape::Validations::ParamsScope::DSL"] = object.Wrap(dsl)

	add := func(required bool) NativeFn {
		return func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			b := object.Kind[*GrapeParamsBuilder](self)
			p := grapeBuildParam(grapeStr(args[0]), required, grapeOptions(args))
			b.set.Params = append(b.set.Params, p)
			return object.NilVal()
		}
	}
	dsl.define("requires", add(true))
	dsl.define("optional", add(false))
}

// registerGrapeFormatter installs Grape::Formatter and its #format / #json /
// #txt / #xml methods.
func (vm *VM) registerGrapeFormatter(mod *RClass) {
	cls := newClass("Grape::Formatter", vm.cObject)
	mod.consts["Formatter"] = object.Wrap(cls)
	vm.consts["Grape::Formatter"] = object.Wrap(cls)

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&GrapeFormatter{})
	}}

	f := grape.Formatter{}
	// #format(fmt, value) -> [body, mime].
	cls.define("format", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		body, mime, err := f.Format(grapeStr(args[0]), grapeToGo(args[1]))
		grapeCheckFormatErr(err)
		return object.Wrap(object.NewArray(object.Wrap(object.NewString(body)), object.Wrap(object.NewString(mime))))
	})
	cls.define("json", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		body, err := f.JSON(grapeToGo(grapeArg(args)))
		grapeCheckFormatErr(err)
		return object.Wrap(object.NewString(body))
	})
	cls.define("xml", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		body, err := f.XML(grapeToGo(grapeArg(args)))
		grapeCheckFormatErr(err)
		return object.Wrap(object.NewString(body))
	})
	cls.define("txt", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(f.Txt(grapeToGo(grapeArg(args)))))
	})
}
