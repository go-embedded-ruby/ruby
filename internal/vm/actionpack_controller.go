// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	"github.com/go-ruby-actionpack/actionpack/controller"
	"github.com/go-ruby-actionpack/actionpack/dispatch"
	rack "github.com/go-ruby-rack/rack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the ActionController::Base controller: the class-level DSL
// (before_action/after_action/around_action, rescue_from, view_context), the
// instance surface (render/redirect_to/head/params/request/response), and the
// PostsController.dispatch(action, env) entry point. A controller instance is a
// plain RObject of the user's subclass; its live *controller.Base is reached
// through the @__ac handle, so a bare `render` inside `def index` resolves to
// the inherited native method that writes into that request's response. Every
// seam — the action body (RunAction), the filter/rescue bodies, the view render
// — runs inline on the VM goroutine under the GVL.

// acControllerDef is the per-ActionController::Base-subclass declaration the
// class DSL accumulates: the before/after/around filter specs, the rescue_from
// specs and the view-render context. Each subclass owns one; dispatch materialises
// it (and its ancestors') into a live *controller.Class bound to the instance.
type acControllerDef struct {
	befores     []acFilterSpec
	afters      []acFilterSpec
	arounds     []acFilterSpec
	rescuers    []acRescueSpec
	viewContext object.Value // the ActionView-style render context (responds to #render), or nil
}

// acFilterSpec is one before/after/around filter: a captured Ruby block or a
// symbol naming an instance method, with the only/except/if/unless conditions.
type acFilterSpec struct {
	blk        *Proc
	sym        string
	only       []string
	except     []string
	ifCond     acCond
	unlessCond acCond
}

// acCond is an if:/unless: condition — a Ruby block (lambda) or a symbol method
// name; set reports whether the condition was declared.
type acCond struct {
	blk *Proc
	sym string
	set bool
}

// acRescueSpec is one rescue_from declaration: the exception classes it matches,
// and the handler (a block or a with: method name).
type acRescueSpec struct {
	classes []*RClass
	blk     *Proc
	sym     string
}

// acRubyErr carries a Ruby exception raised by a controller body out to the
// library's rescue/dispatch layer and back into a Ruby rescue handler.
type acRubyErr struct{ e RubyError }

func (e *acRubyErr) Error() string { return e.e.Error() }

// acControllerDefFor returns the def for a controller subclass, creating it on
// first use (keyed by the class object, so each subclass keeps its own filters).
func (vm *VM) acControllerDefFor(cls *RClass) *acControllerDef {
	if vm.acControllerDefs == nil {
		vm.acControllerDefs = map[*RClass]*acControllerDef{}
	}
	d, ok := vm.acControllerDefs[cls]
	if !ok {
		d = &acControllerDef{}
		vm.acControllerDefs[cls] = d
	}
	return d
}

// acControllerChain returns cls's defs, outermost-ancestor first, so a subclass
// inherits its parents' filters/rescuers (parents run first, matching Rails).
func (vm *VM) acControllerChain(cls *RClass) []*acControllerDef {
	var classes []*RClass
	for c := cls; c != nil && c != vm.cACControllerBase.super; c = c.super {
		classes = append(classes, c)
	}
	defs := make([]*acControllerDef, 0, len(classes))
	for i := len(classes) - 1; i >= 0; i-- {
		if d, ok := vm.acControllerDefs[classes[i]]; ok {
			defs = append(defs, d)
		}
	}
	return defs
}

// registerACController installs the ActionController::Base class DSL and the
// instance/dispatch surface (inherited by every subclass).
func (vm *VM) registerACController(base *RClass) {
	// --- class-level DSL ----------------------------------------------------
	filterDSL := func(kind int) NativeFn {
		return func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			d := vm.acControllerDefFor(self.(*RClass))
			specs := acFilterSpecs(args, blk)
			switch kind {
			case 0:
				d.befores = append(d.befores, specs...)
			case 1:
				d.afters = append(d.afters, specs...)
			default:
				d.arounds = append(d.arounds, specs...)
			}
			return object.NilV
		}
	}
	base.smethods["before_action"] = &Method{name: "before_action", owner: base, native: filterDSL(0)}
	base.smethods["after_action"] = &Method{name: "after_action", owner: base, native: filterDSL(1)}
	base.smethods["around_action"] = &Method{name: "around_action", owner: base, native: filterDSL(2)}

	// rescue_from Klass, ... [with: :meth] { |e| … }
	base.smethods["rescue_from"] = &Method{name: "rescue_from", owner: base, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		spec := acRescueSpec{blk: blk}
		for _, a := range args {
			if c, ok := a.(*RClass); ok {
				spec.classes = append(spec.classes, c)
			}
		}
		if h := lastHashOrNil(args); h != nil {
			if v, ok := h.Get(object.Symbol("with")); ok {
				spec.sym = apStr(v)
			}
		}
		if len(spec.classes) == 0 {
			raise("ArgumentError", "rescue_from requires at least one exception class")
		}
		d := vm.acControllerDefFor(self.(*RClass))
		d.rescuers = append(d.rescuers, spec)
		return object.NilV
	}}

	// view_context(obj) — set the ActionView-style render context (any object
	// responding to #render). The Renderer seam sends it a `render` message.
	base.smethods["view_context"] = &Method{name: "view_context", owner: base, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		vm.acControllerDefFor(self.(*RClass)).viewContext = apArg(args, 0)
		return object.NilV
	}}

	// PostsController.dispatch(action, env = nil) -> [status, headers, body].
	base.smethods["dispatch"] = &Method{name: "dispatch", owner: base, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.acDispatch(self.(*RClass), apStr(apArg(args, 0)), apArg(args, 1))
	}}

	// --- instance surface ---------------------------------------------------
	base.define("render", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if err := acBaseOf(self).Render(acRenderOptions(args)); err != nil {
			raise("AbstractController::DoubleRenderError", "%s", err.Error())
		}
		return object.NilV
	})
	base.define("redirect_to", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		var status []int
		if h := lastHashOrNil(args); h != nil {
			if v, ok := h.Get(object.Symbol("status")); ok {
				status = append(status, apInt(v))
			}
		}
		if err := acBaseOf(self).RedirectTo(rackStr(args[0]), status...); err != nil {
			raise("AbstractController::DoubleRenderError", "%s", err.Error())
		}
		return object.NilV
	})
	base.define("head", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if err := acBaseOf(self).Head(apInt(apArg(args, 0))); err != nil {
			raise("AbstractController::DoubleRenderError", "%s", err.Error())
		}
		return object.NilV
	})
	base.define("params", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ACParams{p: acBaseOf(self).Params(), cls: vm.cACParameters}
	})
	base.define("request", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ACRequest{r: acBaseOf(self).Request(), cls: vm.cACRequest}
	})
	base.define("response", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ACResponse{r: acBaseOf(self).Response(), cls: vm.cACResponse}
	})
	base.define("action_name", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(acBaseOf(self).Action())
	})
	base.define("performed?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(acBaseOf(self).Performed())
	})
}

// acBaseOf returns the live *controller.Base backing a controller instance (from
// its @__ac handle, set by dispatch before the action runs).
func acBaseOf(self object.Value) *controller.Base {
	return getIvar(self, "@__ac").(acHandle).v.(*controller.Base)
}

// acDispatch materialises the controller class for one request, runs the action
// through the library pipeline and returns the SPEC [status, headers, body]
// Rack triple. An unrescued error is re-raised into Ruby.
func (vm *VM) acDispatch(cls *RClass, action string, envArg object.Value) object.Value {
	inst := &RObject{class: cls, ivars: map[string]object.Value{}}
	req := dispatch.NewRequest(acEnv(envArg))
	req.SetPathParameters(map[string]any{"controller": cls.name, "action": action})
	params, err := req.Parameters()
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	cl := vm.acBuildController(inst, cls)
	b := cl.New(req, dispatch.NewResponse(), params)
	setIvar(inst, "@__ac", acHandle{b})
	if perr := b.Process(action); perr != nil {
		panic(vm.acRubyErrorOf(perr))
	}
	r := b.Response()
	return object.NewArray(object.IntValue(int64(r.Status())), rackHeadersToHash(r.Headers()), rackBodyArray(r.Body()))
}

// acEnv reads dispatch's optional env argument into a rack.Env: a nil argument
// yields an empty env, any Hash is converted as the Rack bindings do.
func acEnv(v object.Value) rack.Env {
	if object.IsNil(v) {
		return rack.Env{}
	}
	return rackEnv(v)
}

// acBuildController assembles a *controller.Class from cls's def chain, every
// seam bound to inst: before/after/around filters, rescue_from handlers, the
// RunAction body (the Ruby action method) and the Renderer (the view context).
func (vm *VM) acBuildController(inst object.Value, cls *RClass) *controller.Class {
	cl := controller.NewClass(cls.name)
	var view object.Value
	for _, d := range vm.acControllerChain(cls) {
		for _, s := range d.befores {
			spec := s
			cl.BeforeAction(func(_ *controller.Base) error { return vm.acInvoke(inst, spec.blk, spec.sym) }, vm.acFilterOpts(inst, spec)...)
		}
		for _, s := range d.afters {
			spec := s
			cl.AfterAction(func(_ *controller.Base) error { return vm.acInvoke(inst, spec.blk, spec.sym) }, vm.acFilterOpts(inst, spec)...)
		}
		for _, s := range d.arounds {
			spec := s
			cl.AroundAction(func(_ *controller.Base, yield func() error) error { return vm.acAround(inst, spec, yield) }, vm.acFilterOpts(inst, spec)...)
		}
		for _, r := range d.rescuers {
			spec := r
			cl.RescueFrom(vm.acRescueMatch(spec), vm.acRescueHandle(inst, spec))
		}
		if !object.IsNil(d.viewContext) {
			view = d.viewContext
		}
	}
	cl.RunAction(func(c *controller.Base, action string) error {
		if vm.findMethod(inst, action) == nil {
			return &controller.ActionNotFound{Controller: c.Class().Name, Action: action}
		}
		return vm.acInvoke(inst, nil, action)
	})
	if !object.IsNil(view) {
		cl.SetRenderer(vm.acRenderer(inst, view))
	}
	return cl
}

// acInvoke runs a controller body against inst — a captured block or a named
// instance method — converting a Ruby raise into a Go error the pipeline routes
// through rescue_from.
func (vm *VM) acInvoke(inst object.Value, blk *Proc, sym string) (err error) {
	defer acRecover(&err)
	if blk != nil {
		vm.callBlockSelf(blk, inst, nil)
	} else {
		vm.send(inst, sym, nil, nil)
	}
	return nil
}

// acRecover converts a recovered Ruby exception into an *acRubyErr stored in
// *err; a non-Ruby panic is re-raised untouched.
func acRecover(err *error) {
	if r := recover(); r != nil {
		if re, ok := r.(RubyError); ok {
			*err = &acRubyErr{e: re}
			return
		}
		panic(r)
	}
}

// acAround runs an around filter method, passing it a native yield block: when
// the Ruby method yields, the rest of the chain (and the action) runs, and any
// error it produces is re-raised at the yield point so the method may rescue it.
func (vm *VM) acAround(inst object.Value, spec acFilterSpec, yield func() error) (err error) {
	defer acRecover(&err)
	yp := &Proc{native: func(_ *VM, _ []object.Value) object.Value {
		if e := yield(); e != nil {
			panic(vm.acRubyErrorOf(e))
		}
		return object.NilV
	}}
	vm.send(inst, spec.sym, nil, yp)
	return nil
}

// acFilterOpts maps a filter spec's only/except/if/unless conditions into the
// library's []controller.FilterOption.
func (vm *VM) acFilterOpts(inst object.Value, spec acFilterSpec) []controller.FilterOption {
	var opts []controller.FilterOption
	if len(spec.only) > 0 {
		opts = append(opts, controller.Only(spec.only...))
	}
	if len(spec.except) > 0 {
		opts = append(opts, controller.Except(spec.except...))
	}
	if spec.ifCond.set {
		opts = append(opts, controller.If(vm.acCondFunc(inst, spec.ifCond)))
	}
	if spec.unlessCond.set {
		opts = append(opts, controller.Unless(vm.acCondFunc(inst, spec.unlessCond)))
	}
	return opts
}

// acCondFunc adapts an if:/unless: condition into a func(*Base) bool: a block
// runs against inst, a symbol sends the named predicate method.
func (vm *VM) acCondFunc(inst object.Value, cond acCond) func(*controller.Base) bool {
	return func(_ *controller.Base) bool {
		if cond.blk != nil {
			return vm.callBlockSelf(cond.blk, inst, nil).Truthy()
		}
		return vm.send(inst, cond.sym, nil, nil).Truthy()
	}
}

// acRescueMatch adapts a rescue_from spec into the library's error matcher: the
// raised exception's Ruby class is compared against the declared classes.
func (vm *VM) acRescueMatch(spec acRescueSpec) func(error) bool {
	return func(err error) bool {
		ec := vm.classOf(vm.acErrValue(err))
		for _, c := range spec.classes {
			if classIsA(ec, c) {
				return true
			}
		}
		return false
	}
}

// acRescueHandle adapts a rescue_from spec into the library's handler: it runs
// the block or sends the with: method against inst with the exception object.
func (vm *VM) acRescueHandle(inst object.Value, spec acRescueSpec) func(*controller.Base, error) error {
	return func(_ *controller.Base, err error) error {
		obj := vm.acErrValue(err)
		if spec.blk != nil {
			vm.callBlockSelf(spec.blk, inst, []object.Value{obj})
		} else {
			vm.send(inst, spec.sym, []object.Value{obj}, nil)
		}
		return nil
	}
}

// acRenderer adapts a view context into the library's Renderer seam: it sends a
// `render` message with the render options as a Ruby Hash and returns the body.
func (vm *VM) acRenderer(inst object.Value, view object.Value) controller.Renderer {
	return func(_ *controller.Base, opts controller.RenderOptions) (string, error) {
		r := vm.send(view, "render", []object.Value{acRenderOptsHash(opts)}, nil)
		if s, ok := r.(*object.String); ok {
			return s.Str(), nil
		}
		return r.ToS(), nil
	}
}

// acErrValue maps a pipeline error back into the Ruby exception object a rescue
// handler receives.
func (vm *VM) acErrValue(err error) object.Value {
	return vm.exceptionObject(vm.acRubyErrorOf(err))
}

// acRubyErrorOf converts a pipeline error into the RubyError to raise: a wrapped
// Ruby exception carries its original object; the only other error the pipeline
// yields is an unmapped action, reported as AbstractController::ActionNotFound.
func (vm *VM) acRubyErrorOf(err error) RubyError {
	var re *acRubyErr
	if errors.As(err, &re) {
		return re.e
	}
	return RubyError{Class: "AbstractController::ActionNotFound", Message: err.Error()}
}

// acFilterSpecs turns a before/after/around DSL call's arguments (symbol method
// names + a trailing options Hash) and optional block into filter specs: one per
// symbol plus one for the block, all sharing the only/except/if/unless options.
func acFilterSpecs(args []object.Value, blk *Proc) []acFilterSpec {
	var only, except []string
	var ifCond, unlessCond acCond
	if h := lastHashOrNil(args); h != nil {
		for _, k := range h.Keys {
			val, _ := h.Get(k)
			switch apStr(k) {
			case "only":
				only = apStrList(val)
			case "except":
				except = apStrList(val)
			case "if":
				ifCond = acCondFrom(val)
			case "unless":
				unlessCond = acCondFrom(val)
			}
		}
	}
	mk := func(blk *Proc, sym string) acFilterSpec {
		return acFilterSpec{blk: blk, sym: sym, only: only, except: except, ifCond: ifCond, unlessCond: unlessCond}
	}
	var specs []acFilterSpec
	for _, a := range args {
		if _, ok := a.(*object.Hash); ok {
			continue
		}
		specs = append(specs, mk(nil, apStr(a)))
	}
	if blk != nil {
		specs = append(specs, mk(blk, ""))
	}
	return specs
}

// acCondFrom reads an if:/unless: value into an acCond: a Proc (lambda) is a
// block condition, anything else a symbol predicate-method name.
func acCondFrom(v object.Value) acCond {
	if p, ok := v.(*Proc); ok {
		return acCond{blk: p, set: true}
	}
	return acCond{sym: apStr(v), set: true}
}

// acRenderOptions builds a controller.RenderOptions from a render call: a leading
// String is the template/action name; the options Hash supplies plain/json/
// template/action/status/content_type/layout.
func acRenderOptions(args []object.Value) controller.RenderOptions {
	var o controller.RenderOptions
	if len(args) > 0 {
		if s, ok := args[0].(*object.String); ok {
			o.Template = s.Str()
		}
	}
	if h := lastHashOrNil(args); h != nil {
		for _, k := range h.Keys {
			val, _ := h.Get(k)
			switch apStr(k) {
			case "plain":
				o.Plain = rackStr(val)
			case "json":
				o.JSON = rackToGo(val)
			case "template":
				o.Template = apStr(val)
			case "action":
				o.Action = apStr(val)
			case "status":
				o.Status = apInt(val)
			case "content_type":
				o.ContentType = rackStr(val)
			case "layout":
				o.Layout = apStr(val)
			}
		}
	}
	return o
}

// acRenderOptsHash maps a controller.RenderOptions into the Ruby Hash a view
// context's #render receives (symbol keys), so the ActionView seam sees the
// full render request.
func acRenderOptsHash(opts controller.RenderOptions) *object.Hash {
	h := object.NewHash()
	h.Set(object.Symbol("template"), object.NewString(opts.Template))
	h.Set(object.Symbol("action"), object.NewString(opts.Action))
	h.Set(object.Symbol("plain"), object.NewString(opts.Plain))
	h.Set(object.Symbol("json"), rackFromGo(opts.JSON))
	h.Set(object.Symbol("status"), object.IntValue(int64(opts.Status)))
	h.Set(object.Symbol("content_type"), object.NewString(opts.ContentType))
	h.Set(object.Symbol("layout"), object.NewString(opts.Layout))
	return h
}
