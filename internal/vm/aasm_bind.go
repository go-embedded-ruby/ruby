// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	aasm "github.com/go-ruby-aasm/aasm"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file holds the AASM binding internals: the Go-side state-machine
// specifications recorded from the `aasm do … end` DSL, the per-Ruby-object
// build that turns a spec into a bound *aasm.Instance (with the state / persist
// seams and the guard / callback closures wired back into rbgo under the GVL),
// the Ruby <-> engine value conversions and the transition-error mapping onto
// the AASM Ruby exception tree. aasm.go holds the class and method wiring.

// aasmCbSpec is one guard / callback declaration recorded from the DSL: either a
// method name (`:ok?`) or a block (`{ … }`). Resolved to an aasm.Guard /
// aasm.Callback bound to a specific Ruby object at build time.
type aasmCbSpec struct {
	sym string // method name, or "" when a block is used
	blk *Proc  // block, or nil when a method name is used
}

// aasmStateSpec is a `state :name, initial:/final:/enter:/exit:` declaration.
type aasmStateSpec struct {
	name        string
	initial     bool
	final       bool
	enter, exit []aasmCbSpec
}

// aasmTransSpec is one `transitions from:, to:, guard:, …` declaration.
type aasmTransSpec struct {
	from                   []string
	to                     string
	guards, unless         []aasmCbSpec
	before, after, success []aasmCbSpec
}

// aasmEventSpec is an `event :name do … end` declaration. Event-level guards and
// callbacks apply to every one of the event's transitions.
type aasmEventSpec struct {
	name                                string
	transitions                         []aasmTransSpec
	guards                              []aasmCbSpec
	before, after, success, afterCommit []aasmCbSpec
	errorCbs                            []aasmCbSpec
}

// aasmMachineSpec is one whole `aasm do … end` block: the pure-Go analogue of a
// gem state machine, replayed to build a bound engine instance per Ruby object.
type aasmMachineSpec struct {
	name   string
	column string
	whiny  bool
	states []aasmStateSpec
	events []aasmEventSpec
}

// stateSpec returns the named state declaration, or nil.
func (m *aasmMachineSpec) stateSpec(name string) *aasmStateSpec {
	for i := range m.states {
		if m.states[i].name == name {
			return &m.states[i]
		}
	}
	return nil
}

// eventSpec returns the named event declaration, or nil.
func (m *aasmMachineSpec) eventSpec(name string) *aasmEventSpec {
	for i := range m.events {
		if m.events[i].name == name {
			return &m.events[i]
		}
	}
	return nil
}

// initialState reports the state flagged initial, or "" if none is.
func (m *aasmMachineSpec) initialState() string {
	for i := range m.states {
		if m.states[i].initial {
			return m.states[i].name
		}
	}
	return ""
}

// aasmSpecFor finds the machine named name defined on cls or any ancestor, so a
// subclass inherits its parents' machines. Returns nil if none matches.
func (vm *VM) aasmSpecFor(cls *RClass, name string) *aasmMachineSpec {
	for _, anc := range vm.ancestors(cls) {
		for _, m := range vm.aasmSpecs[anc] {
			if m.name == name {
				return m
			}
		}
	}
	return nil
}

// aasmRubyErr wraps a Ruby exception (a RubyError panic) that escaped a guard or
// callback, so the engine can route it through the event error callbacks and the
// original exception is re-raised faithfully if it survives.
type aasmRubyErr struct{ re RubyError }

func (e aasmRubyErr) Error() string { return e.re.Error() }

// aasmCbSpecs reads a DSL value (Symbol / String method name, Proc, or Array of
// those) into a list of callback specs. A nil / absent value yields nil.
func aasmCbSpecs(v object.Value) []aasmCbSpec {
	if v == nil || object.IsNil(v) {
		return nil
	}
	switch t := v.(type) {
	case object.Symbol:
		return []aasmCbSpec{{sym: string(t)}}
	case *object.String:
		return []aasmCbSpec{{sym: t.Str()}}
	case *Proc:
		return []aasmCbSpec{{blk: t}}
	case *object.Array:
		var out []aasmCbSpec
		for _, el := range t.Elems {
			out = append(out, aasmCbSpecs(el)...)
		}
		return out
	}
	return nil
}

// aasmNames reads a DSL value that is a single Symbol/String or an Array of them
// into a list of names (for a transition's `from:`).
func aasmNames(v object.Value) []string {
	if a, ok := v.(*object.Array); ok {
		out := make([]string, 0, len(a.Elems))
		for _, el := range a.Elems {
			out = append(out, arStr(el))
		}
		return out
	}
	return []string{arStr(v)}
}

// aasmArgsToRuby converts engine callback args (already Ruby object.Values, boxed
// as any by the engine) back to a Ruby slice.
func aasmArgsToRuby(args []any) []object.Value {
	out := make([]object.Value, 0, len(args))
	for _, a := range args {
		if v, ok := a.(object.Value); ok {
			out = append(out, v)
		}
	}
	return out
}

// aasmErrToRuby turns a Go error surfaced to an event error callback into the
// Ruby exception object to hand the callback: the original Ruby exception when
// one was raised, else a fresh AASM::Error.
func (vm *VM) aasmErrToRuby(err error) object.Value {
	var re aasmRubyErr
	if errors.As(err, &re) {
		if re.re.Obj != nil {
			return re.re.Obj
		}
		return vm.buildException(re.re.Class, re.re.Message)
	}
	return vm.buildException("AASM::Error", err.Error())
}

// buildException constructs a Ruby exception object of the named class (falling
// back to RuntimeError when unknown) with the given message.
func (vm *VM) buildException(class, msg string) object.Value {
	cls, ok := vm.consts[class].(*RClass)
	if !ok {
		cls = vm.consts["RuntimeError"].(*RClass)
	}
	return vm.send(cls, "new", []object.Value{object.NewString(msg)}, nil)
}

// aasmGuard binds a guard spec to obj: it runs the Ruby predicate under the GVL
// and reports its truthiness. A Ruby exception is captured as an aasmRubyErr.
func (vm *VM) aasmGuard(spec aasmCbSpec, obj object.Value, name string) aasm.Guard {
	return func(args []any) (ok bool, err error) {
		res, err := vm.aasmInvoke(spec, obj, name, aasmArgsToRuby(args))
		if err != nil {
			return false, err
		}
		return res.Truthy(), nil
	}
}

// aasmCallback binds a callback spec to obj: it runs the Ruby method / block
// under the GVL. A Ruby exception is captured as an aasmRubyErr.
func (vm *VM) aasmCallback(spec aasmCbSpec, obj object.Value, name string) aasm.Callback {
	return func(args []any) (any, error) {
		res, err := vm.aasmInvoke(spec, obj, name, aasmArgsToRuby(args))
		if err != nil {
			return nil, err
		}
		return res, nil
	}
}

// aasmErrorCallback binds an event `error` spec to obj: it runs the Ruby handler
// with the raised exception as the first argument. Returning normally swallows
// the error (the gem's behaviour); a raise inside the handler propagates.
func (vm *VM) aasmErrorCallback(spec aasmCbSpec, obj object.Value, name string) aasm.ErrorCallback {
	return func(inErr error, args []any) error {
		rbArgs := append([]object.Value{vm.aasmErrToRuby(inErr)}, aasmArgsToRuby(args)...)
		_, err := vm.aasmInvoke(spec, obj, name, rbArgs)
		return err
	}
}

// aasmInvoke calls a guard/callback spec against obj, recovering a Ruby raise
// into an aasmRubyErr so the engine can route it; a non-Ruby Go panic propagates.
func (vm *VM) aasmInvoke(spec aasmCbSpec, obj object.Value, name string, args []object.Value) (res object.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok {
				res, err = object.NilV, aasmRubyErr{re: re}
				return
			}
			panic(r)
		}
	}()
	if spec.blk != nil {
		return vm.callBlockSelf(spec.blk, obj, args), nil
	}
	return vm.send(obj, spec.sym, args, nil), nil
}

// aasmSeams builds the state / persist seams for obj: GetState / SetState read
// and write the machine's state column (a reader/writer method when the object
// has one, else the backing ivar), and Persist sends save / save! when defined.
func (vm *VM) aasmSeams(spec *aasmMachineSpec, obj object.Value) aasm.Seams {
	col := spec.column
	ivar := "@" + col
	return aasm.Seams{
		GetState: func() string {
			if vm.respondsTo(obj, col) {
				return aasmStateStr(vm.send(obj, col, nil, nil))
			}
			return aasmStateStr(getIvar(obj, ivar))
		},
		SetState: func(s string) error {
			if vm.respondsTo(obj, col+"=") {
				vm.send(obj, col+"=", []object.Value{object.Symbol(s)}, nil)
				return nil
			}
			setIvar(obj, ivar, object.Symbol(s))
			return nil
		},
		Persist: func() error {
			if vm.respondsTo(obj, "save!") {
				vm.send(obj, "save!", nil, nil)
			} else if vm.respondsTo(obj, "save") {
				vm.send(obj, "save", nil, nil)
			}
			return nil
		},
	}
}

// aasmStateStr renders a state value (Symbol / String / nil) as a plain string;
// nil / blank yields "", which the engine reads as the initial state.
func aasmStateStr(v object.Value) string {
	if v == nil || object.IsNil(v) {
		return ""
	}
	switch t := v.(type) {
	case object.Symbol:
		return string(t)
	case *object.String:
		return t.Str()
	}
	return v.ToS()
}

// aasmBuild replays a machine spec into a fresh aasm.Machine bound to obj, with
// every guard and callback wired back to obj's Ruby methods / blocks.
func (vm *VM) aasmBuild(spec *aasmMachineSpec, obj object.Value) *aasm.Instance {
	m := aasm.New(spec.name).WhinyTransitions(spec.whiny)
	for i := range spec.states {
		s := &spec.states[i]
		var opts []aasm.StateOption
		if s.initial {
			opts = append(opts, aasm.Initial())
		}
		if s.final {
			opts = append(opts, aasm.Final())
		}
		for _, cb := range s.enter {
			opts = append(opts, aasm.Enter(vm.aasmCallback(cb, obj, s.name)))
		}
		for _, cb := range s.exit {
			opts = append(opts, aasm.Exit(vm.aasmCallback(cb, obj, s.name)))
		}
		m.State(s.name, opts...)
	}
	for i := range spec.events {
		e := &spec.events[i]
		var opts []aasm.EventOption
		for _, t := range e.transitions {
			tr := aasm.Transition{From: t.from, To: t.to}
			for _, g := range e.guards {
				tr.Guards = append(tr.Guards, vm.aasmGuard(g, obj, e.name))
			}
			for _, g := range t.guards {
				tr.Guards = append(tr.Guards, vm.aasmGuard(g, obj, e.name))
			}
			for _, g := range t.unless {
				tr.Unless = append(tr.Unless, vm.aasmGuard(g, obj, e.name))
			}
			for _, cb := range t.before {
				tr.Before = append(tr.Before, vm.aasmCallback(cb, obj, e.name))
			}
			for _, cb := range t.after {
				tr.After = append(tr.After, vm.aasmCallback(cb, obj, e.name))
			}
			for _, cb := range t.success {
				tr.Success = append(tr.Success, vm.aasmCallback(cb, obj, e.name))
			}
			opts = append(opts, aasm.Transitions(tr))
		}
		for _, cb := range e.before {
			opts = append(opts, aasm.Before(vm.aasmCallback(cb, obj, e.name)))
		}
		for _, cb := range e.after {
			opts = append(opts, aasm.After(vm.aasmCallback(cb, obj, e.name)))
		}
		for _, cb := range e.success {
			opts = append(opts, aasm.Success(vm.aasmCallback(cb, obj, e.name)))
		}
		for _, cb := range e.afterCommit {
			opts = append(opts, aasm.AfterCommit(vm.aasmCallback(cb, obj, e.name)))
		}
		for _, cb := range e.errorCbs {
			opts = append(opts, aasm.Error(vm.aasmErrorCallback(cb, obj, e.name)))
		}
		m.Event(e.name, opts...)
	}
	return m.Bind(vm.aasmSeams(spec, obj))
}

// aasmRaise maps an engine error onto the AASM Ruby exception tree, re-raising a
// Ruby exception that escaped a callback unchanged.
func (vm *VM) aasmRaise(err error) {
	var re aasmRubyErr
	if errors.As(err, &re) {
		panic(re.re)
	}
	if errors.Is(err, aasm.ErrInvalidTransition) {
		raise("AASM::InvalidTransition", "%s", err.Error())
	}
	if errors.Is(err, aasm.ErrUndefinedEvent) {
		raise("AASM::UndefinedState", "%s", err.Error())
	}
	raise("AASM::Error", "%s", err.Error())
}

// aasmSymArray renders a list of names as a Ruby Array of Symbols.
func aasmSymArray(names []string) object.Value {
	el := make([]object.Value, len(names))
	for i, n := range names {
		el[i] = object.Symbol(n)
	}
	return object.NewArrayFromSlice(el)
}
