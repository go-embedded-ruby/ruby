// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-ruby-actionpack/actionpack/routing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires ActionDispatch::Routing::RouteSet.new, its `draw` DSL and the
// recognize / path / url_for reverse-routing helpers, plus the Mapper DSL self
// the draw block runs against (verb matchers, resources/resource, root,
// namespace/scope, member/collection, constraints). The route compiler,
// recognizer and generator are the library; running the DSL block is the seam.

// registerACRouteSet installs ActionDispatch::Routing::RouteSet and its Mapper
// DSL surface.
func (vm *VM) registerACRouteSet(routeSet, mapper *RClass) {
	routeSet.smethods["new"] = &Method{name: "new", owner: routeSet, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ACRouteSet{rs: routing.NewRouteSet(), cls: routeSet}
	}}

	// draw do … end — evaluate the routing DSL against a Mapper wrapper.
	routeSet.define("draw", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "RouteSet#draw requires a block")
		}
		rs := self.(*ACRouteSet)
		err := rs.rs.Draw(func(m *routing.Mapper) {
			vm.callBlockSelf(blk, &ACMapper{m: m, cls: vm.cACMapper}, nil)
		})
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return object.NilV
	})

	// recognize(method, path) — resolve a request to {controller, action, params},
	// or nil when nothing matches.
	routeSet.define("recognize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		rec, ok := self.(*ACRouteSet).rs.Recognize(apStr(apArg(args, 0)), apStr(apArg(args, 1)))
		if !ok {
			return object.NilV
		}
		return rackFromGo(rec.Params)
	})

	// path(name, **params) — the named-route path helper (post_path(id: 1)).
	routeSet.define("path", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self.(*ACRouteSet).rs.Path(apStr(apArg(args, 0)), acAnyMap(lastHashOrNil(args)))
		if err != nil {
			raise("ActionController::UrlGenerationError", "%s", err.Error())
		}
		return object.NewString(s)
	})

	// path_args(name, *args) — positional path helper (post_path(1)); a trailing
	// Hash supplies extra options.
	routeSet.define("path_args", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		s, err := self.(*ACRouteSet).rs.PathArgs(apStr(args[0]), acAnyArgs(args[1:])...)
		if err != nil {
			raise("ActionController::UrlGenerationError", "%s", err.Error())
		}
		return object.NewString(s)
	})

	// url_for(controller:, action:, **params) — reverse routing from an options
	// Hash.
	routeSet.define("url_for", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self.(*ACRouteSet).rs.UrlFor(acAnyMap(lastHashOrNil(args)))
		if err != nil {
			raise("ActionController::UrlGenerationError", "%s", err.Error())
		}
		return object.NewString(s)
	})

	// routes — the routes in definition order, each as a small Hash.
	routeSet.define("routes", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		routes := self.(*ACRouteSet).rs.Routes()
		out := make([]object.Value, len(routes))
		for i, r := range routes {
			h := object.NewHash()
			h.Set(object.NewString("name"), object.NewString(r.Name))
			h.Set(object.NewString("verb"), object.NewString(r.Verb))
			h.Set(object.NewString("spec"), object.NewString(r.Spec))
			h.Set(object.NewString("controller"), object.NewString(r.Controller))
			h.Set(object.NewString("action"), object.NewString(r.Action))
			out[i] = h
		}
		return object.NewArrayFromSlice(out)
	})

	vm.registerACMapper(mapper)
}

// registerACMapper installs the routes-DSL surface a `draw` block runs against.
func (vm *VM) registerACMapper(cls *RClass) {
	self := func(v object.Value) *routing.Mapper { return v.(*ACMapper).m }

	// Verb matchers: get/post/put/patch/delete "path", to:, as:, via:, …
	for name, fn := range map[string]func(*routing.Mapper, string, ...routing.Option){
		"get":    (*routing.Mapper).Get,
		"post":   (*routing.Mapper).Post,
		"put":    (*routing.Mapper).Put,
		"patch":  (*routing.Mapper).Patch,
		"delete": (*routing.Mapper).Delete,
		"match":  (*routing.Mapper).Match,
	} {
		verb := fn
		cls.define(name, func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			verb(self(v), acPath(args), acRouteOptions(args)...)
			return object.NilV
		})
	}

	// root to: "home#index" (or root "home#index").
	cls.define("root", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).Root(acTarget(args))
		return object.NilV
	})

	// resources :posts [do … end] — a plural RESTful resource.
	cls.define("resources", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		self(v).Resources(apStr(apArg(args, 0)), vm.acSubMapper(blk), acResOptions(args)...)
		return object.NilV
	})

	// resource :profile [do … end] — a singular RESTful resource.
	cls.define("resource", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		self(v).Resource(apStr(apArg(args, 0)), vm.acSubMapper(blk), acResOptions(args)...)
		return object.NilV
	})

	// namespace :admin do … end — a path/module/name-prefixed block.
	cls.define("namespace", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		self(v).Namespace(apStr(apArg(args, 0)), vm.acReqBlock(blk, "namespace"))
		return object.NilV
	})

	// scope path:/module:/as: do … end — a scoped block.
	cls.define("scope", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		self(v).Scope(acScopeOpts(args), vm.acReqBlock(blk, "scope"))
		return object.NilV
	})

	// member / collection do … end — resource-level route groups.
	cls.define("member", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		self(v).Member(vm.acReqBlock(blk, "member"))
		return object.NilV
	})
	cls.define("collection", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		self(v).Collection(vm.acReqBlock(blk, "collection"))
		return object.NilV
	})

	// constraints(id: /\d+/) do … end — added segment constraints.
	cls.define("constraints", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		self(v).ConstraintsBlock(apStrMap(apArg(args, 0)), vm.acReqBlock(blk, "constraints"))
		return object.NilV
	})
}

// acSubMapper adapts an optional resources/resource block into the library's
// func(*Mapper): nil when no block was given (the resource generates its default
// routes), else a closure running the block against a sub-Mapper wrapper.
func (vm *VM) acSubMapper(blk *Proc) func(*routing.Mapper) {
	if blk == nil {
		return nil
	}
	return func(sub *routing.Mapper) {
		vm.callBlockSelf(blk, &ACMapper{m: sub, cls: vm.cACMapper}, nil)
	}
}

// acReqBlock adapts a required DSL block into the library's func(*Mapper),
// raising when the block is absent (namespace/scope/member/collection/
// constraints all take a mandatory block).
func (vm *VM) acReqBlock(blk *Proc, name string) func(*routing.Mapper) {
	if blk == nil {
		raise("ArgumentError", "%s requires a block", name)
	}
	return func(sub *routing.Mapper) {
		vm.callBlockSelf(blk, &ACMapper{m: sub, cls: vm.cACMapper}, nil)
	}
}

// acPath returns the route path (the first positional, non-Hash argument), or
// "/" when only keyword options were given.
func acPath(args []object.Value) string {
	if len(args) > 0 {
		if _, ok := args[0].(*object.Hash); !ok {
			return apStr(args[0])
		}
	}
	return "/"
}

// acTarget returns a route's dispatch target: the first positional String, else
// the `to:` option ("controller#action").
func acTarget(args []object.Value) string {
	if len(args) > 0 {
		if _, ok := args[0].(*object.Hash); !ok {
			return apStr(args[0])
		}
	}
	if h := lastHashOrNil(args); h != nil {
		if v, ok := h.Get(object.Symbol("to")); ok {
			return apStr(v)
		}
	}
	return ""
}

// acRouteOptions reads a route declaration's keyword options into the library's
// []routing.Option (to:/as:/via:/action:/controller:/constraints:/defaults:/on:).
func acRouteOptions(args []object.Value) []routing.Option {
	h := lastHashOrNil(args)
	if h == nil {
		return nil
	}
	var out []routing.Option
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		switch apStr(k) {
		case "to":
			out = append(out, routing.To(apStr(val)))
		case "as":
			out = append(out, routing.As(apStr(val)))
		case "via":
			out = append(out, routing.Via(apStrList(val)...))
		case "action":
			out = append(out, routing.Action(apStr(val)))
		case "controller":
			out = append(out, routing.Controller(apStr(val)))
		case "constraints":
			out = append(out, routing.Constraints(apStrMap(val)))
		case "defaults":
			out = append(out, routing.Defaults(apStrMap(val)))
		case "on":
			out = append(out, routing.On(apStr(val)))
		}
	}
	return out
}

// acResOptions reads a resources/resource declaration's keyword options into the
// library's []routing.ResOption (only:/except:/path:/controller:/param:).
func acResOptions(args []object.Value) []routing.ResOption {
	h := lastHashOrNil(args)
	if h == nil {
		return nil
	}
	var out []routing.ResOption
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		switch apStr(k) {
		case "only":
			out = append(out, routing.Only(apStrList(val)...))
		case "except":
			out = append(out, routing.Except(apStrList(val)...))
		case "path":
			out = append(out, routing.PathName(apStr(val)))
		case "controller":
			out = append(out, routing.ResController(apStr(val)))
		case "param":
			out = append(out, routing.ResParam(apStr(val)))
		}
	}
	return out
}

// acScopeOpts reads a scope declaration's keyword options into routing.ScopeOpts.
func acScopeOpts(args []object.Value) routing.ScopeOpts {
	var opts routing.ScopeOpts
	h := lastHashOrNil(args)
	if h == nil {
		return opts
	}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		switch apStr(k) {
		case "path":
			opts.Path = apStr(val)
		case "module":
			opts.Module = apStr(val)
		case "as":
			opts.As = apStr(val)
		case "constraints":
			opts.Constraints = apStrMap(val)
		case "defaults":
			opts.Defaults = apStrMap(val)
		}
	}
	return opts
}

// acAnyMap reads a Ruby Hash into the map[string]any the reverse-routing helpers
// consume (each value mapped through rackToGo); a nil Hash yields an empty map.
func acAnyMap(h *object.Hash) map[string]any {
	out := map[string]any{}
	if h == nil {
		return out
	}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		out[apStr(k)] = rackToGo(val)
	}
	return out
}

// acAnyArgs maps positional path_args into the []any the helper consumes: a
// trailing Hash becomes a map[string]any option bag, every other argument its
// rackToGo scalar.
func acAnyArgs(args []object.Value) []any {
	out := make([]any, 0, len(args))
	for i, a := range args {
		if h, ok := a.(*object.Hash); ok && i == len(args)-1 {
			out = append(out, acAnyMap(h))
			continue
		}
		out = append(out, rackToGo(a))
	}
	return out
}

// lastHashOrNil returns the trailing keyword-options Hash of an argument list,
// or nil when the last argument is not a Hash.
func lastHashOrNil(args []object.Value) *object.Hash {
	if h, ok := lastHash(args); ok {
		return h
	}
	return nil
}
