// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerAASM installs the AASM ("Acts As State Machine") surface — require
// "aasm" — backed by the pure-Go engine of github.com/go-ruby-aasm/aasm:
//
//   - the AASM mixin: `include AASM` grafts the class-level `aasm do … end` DSL
//     (state / event / transitions with initial/final, guards, and the before /
//     after / success / error / enter / exit callbacks) plus the instance API
//     onto the including class;
//   - for each event `e`, the generated `e` (fire, in-memory), `e!` (fire!,
//     persisting) and `may_e?` (may_fire?) instance methods, and for each state
//     `s` the `s?` predicate;
//   - `obj.aasm` / `Klass.aasm` — an AASM::InstanceBase reader exposing
//     current_state, states, events, permitted_events, fire/fire!/may_fire?;
//   - the AASM::Error tree (Error < StandardError; InvalidTransition /
//     UndefinedState < Error) the whiny transitions raise into.
//
// The deterministic state-machine core (transition selection, the faithful
// callback ordering, whiny transitions) lives in the library; this file is the
// class and method wiring, and aasm_bind.go holds the Go-side machine specs, the
// per-object engine build and the state / persist / guard / callback seams. Every
// guard and callback runs INLINE on the VM goroutine under the GVL; a Ruby raise
// inside one routes to the event's `error` callback (else propagates) exactly as
// the gem does.
func (vm *VM) registerAASM() {
	mod := newClass("AASM", nil)
	mod.isModule = true
	vm.consts["AASM"] = mod

	vm.registerAASMErrors(mod)

	base := newClass("AASM::InstanceBase", vm.cObject)
	mod.consts["InstanceBase"] = base
	vm.consts["AASM::InstanceBase"] = base
	vm.registerAASMInstanceBase(base)

	// included(base): graft the class-level DSL and the instance API onto the
	// class that includes AASM (the include machinery hands the hook the includer).
	mod.smethods["included"] = &Method{name: "included", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.aasmInstall(args[0].(*RClass))
		return object.NilV
	}}
}

// registerAASMErrors installs the AASM error tree: Error < StandardError, and
// InvalidTransition / UndefinedState / NoTransitionsDefinedError < Error, mirroring
// the gem's exceptions so a whiny transition is rescuable as AASM::InvalidTransition.
func (vm *VM) registerAASMErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	errC := newClass("AASM::Error", std)
	mod.consts["Error"] = errC
	vm.consts["AASM::Error"] = errC
	for _, n := range []string{"InvalidTransition", "UndefinedState", "NoTransitionsDefinedError"} {
		c := newClass("AASM::"+n, errC)
		mod.consts[n] = c
		vm.consts["AASM::"+n] = c
	}
}

// aasmInstall wires the class-level `aasm` DSL/introspection method and the
// instance-level `aasm` reader onto a class that includes AASM.
func (vm *VM) aasmInstall(cls *RClass) {
	// Klass.aasm(name = nil, **opts, &block): with a block, define (or extend) a
	// state machine; without one, return the class-level introspection reader.
	cls.smethods["aasm"] = &Method{name: "aasm", owner: cls, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		c := self.(*RClass)
		if blk == nil {
			return vm.aasmWrapper(c, aasmMachineName(args), nil)
		}
		vm.aasmDefine(c, args, blk)
		return object.NilV
	}}
	// obj.aasm(name = nil): the instance introspection / fire reader.
	cls.define("aasm", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.aasmWrapper(vm.classOf(self), aasmMachineName(args), self)
	})
}

// aasmMachineName reads the optional leading Symbol/String machine name from an
// aasm(...) call, defaulting to "" (the gem's default unnamed machine). A leading
// Hash (keyword options only) leaves the name empty.
func aasmMachineName(args []object.Value) string {
	for _, a := range args {
		switch t := a.(type) {
		case object.Symbol:
			return string(t)
		case *object.String:
			return t.Str()
		}
	}
	return ""
}

// aasmDefine records a machine from an `aasm(name, **opts) do … end` block and
// generates the event / state / column instance methods for it.
func (vm *VM) aasmDefine(cls *RClass, args []object.Value, blk *Proc) {
	name := aasmMachineName(args)
	spec := &aasmMachineSpec{name: name, whiny: true}
	if name == "" {
		spec.column = "aasm_state"
	} else {
		spec.column = "aasm_" + name
	}
	if opts, ok := lastHash(args); ok {
		if v := hgetv(opts, "column"); !object.IsNil(v) {
			spec.column = arStr(v)
		}
		if v, ok := hget(opts, "whiny_transitions"); ok {
			spec.whiny = v.Truthy()
		}
	}
	vm.aasmEvalBlock(spec, blk)

	if vm.aasmSpecs == nil {
		vm.aasmSpecs = map[*RClass][]*aasmMachineSpec{}
	}
	vm.aasmSpecs[cls] = append(vm.aasmSpecs[cls], spec)
	vm.aasmDefineMethods(cls, spec)
}

// aasmEvalBlock runs the `aasm do … end` body against a throwaway builder whose
// singleton carries the `state` / `event` DSL methods, recording into spec.
func (vm *VM) aasmEvalBlock(spec *aasmMachineSpec, blk *Proc) {
	builder := &RObject{class: vm.cObject, ivars: map[string]object.Value{}}
	sc, _ := vm.ensureSingleton(builder)
	sc.define("state", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		aasmAddState(spec, args)
		return object.NilV
	})
	sc.define("event", func(vm *VM, _ object.Value, args []object.Value, b *Proc) object.Value {
		vm.aasmAddEvent(spec, args, b)
		return object.NilV
	})
	vm.callBlockSelf(blk, builder, nil)
}

// aasmAddState records a `state :name, initial:/final:/enter:/exit:` declaration.
func aasmAddState(spec *aasmMachineSpec, args []object.Value) {
	st := aasmStateSpec{name: arStr(args[0])}
	if opts, ok := lastHash(args); ok {
		if v, ok := hget(opts, "initial"); ok && v.Truthy() {
			st.initial = true
		}
		if v, ok := hget(opts, "final"); ok && v.Truthy() {
			st.final = true
		}
		st.enter = aasmCbSpecs(hgetv(opts, "enter"))
		st.exit = aasmCbSpecs(hgetv(opts, "exit"))
	}
	spec.states = append(spec.states, st)
}

// aasmAddEvent records an `event :name, **opts do … end` declaration, evaluating
// the event body (transitions / guard / before / after / success / error /
// after_commit) when a block is given.
func (vm *VM) aasmAddEvent(spec *aasmMachineSpec, args []object.Value, blk *Proc) {
	ev := aasmEventSpec{name: arStr(args[0])}
	if opts, ok := lastHash(args); ok {
		ev.guards = append(ev.guards, aasmCbSpecs(hgetv(opts, "guard"))...)
		ev.guards = append(ev.guards, aasmCbSpecs(hgetv(opts, "guards"))...)
		ev.before = aasmCbSpecs(hgetv(opts, "before"))
		ev.after = aasmCbSpecs(hgetv(opts, "after"))
		ev.success = aasmCbSpecs(hgetv(opts, "success"))
		ev.afterCommit = aasmCbSpecs(hgetv(opts, "after_commit"))
		ev.errorCbs = aasmCbSpecs(hgetv(opts, "error"))
	}
	if blk != nil {
		vm.aasmEvalEventBlock(&ev, blk)
	}
	spec.events = append(spec.events, ev)
}

// aasmEvalEventBlock runs an event body against a builder whose singleton carries
// the `transitions` / `guard` / `before` / `after` / `success` / `error` /
// `after_commit` DSL, recording into ev.
func (vm *VM) aasmEvalEventBlock(ev *aasmEventSpec, blk *Proc) {
	builder := &RObject{class: vm.cObject, ivars: map[string]object.Value{}}
	sc, _ := vm.ensureSingleton(builder)
	sc.define("transitions", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		aasmAddTransition(ev, args)
		return object.NilV
	})
	cb := func(set *[]aasmCbSpec) NativeFn {
		return func(vm *VM, _ object.Value, args []object.Value, b *Proc) object.Value {
			*set = append(*set, aasmArgOrBlock(args, b)...)
			return object.NilV
		}
	}
	sc.define("guard", cb(&ev.guards))
	sc.define("before", cb(&ev.before))
	sc.define("after", cb(&ev.after))
	sc.define("success", cb(&ev.success))
	sc.define("error", cb(&ev.errorCbs))
	sc.define("after_commit", cb(&ev.afterCommit))
	vm.callBlockSelf(blk, builder, nil)
}

// aasmAddTransition records a `transitions from:, to:, guard:, unless:, before:,
// after:, success:` declaration on an event.
func aasmAddTransition(ev *aasmEventSpec, args []object.Value) {
	tr := aasmTransSpec{}
	if opts, ok := lastHash(args); ok {
		if v := hgetv(opts, "from"); !object.IsNil(v) {
			tr.from = aasmNames(v)
		}
		if v := hgetv(opts, "to"); !object.IsNil(v) {
			tr.to = arStr(v)
		}
		tr.guards = aasmCbSpecs(hgetv(opts, "guard"))
		tr.guards = append(tr.guards, aasmCbSpecs(hgetv(opts, "guards"))...)
		tr.unless = aasmCbSpecs(hgetv(opts, "unless"))
		tr.before = aasmCbSpecs(hgetv(opts, "before"))
		tr.after = aasmCbSpecs(hgetv(opts, "after"))
		tr.success = aasmCbSpecs(hgetv(opts, "success"))
	}
	ev.transitions = append(ev.transitions, tr)
}

// aasmArgOrBlock reads an event-level callback declaration given either a block
// (`after { … }`) or method-name arguments (`after :log, :notify`).
func aasmArgOrBlock(args []object.Value, blk *Proc) []aasmCbSpec {
	if blk != nil {
		return []aasmCbSpec{{blk: blk}}
	}
	var out []aasmCbSpec
	for _, a := range args {
		out = append(out, aasmCbSpecs(a)...)
	}
	return out
}

// aasmDefineMethods generates the fire / predicate / column-accessor instance
// methods for one machine on cls.
func (vm *VM) aasmDefineMethods(cls *RClass, spec *aasmMachineSpec) {
	for i := range spec.states {
		st := spec.states[i].name
		cls.define(st+"?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Bool(vm.aasmBuild(spec, self).Is(st))
		})
	}
	for i := range spec.events {
		ev := spec.events[i].name
		cls.define(ev, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.aasmFire(spec, self, ev, args, false)
		})
		cls.define(ev+"!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.aasmFire(spec, self, ev, args, true)
		})
		cls.define("may_"+ev+"?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			ok, err := vm.aasmBuild(spec, self).MayFire(ev, aasmToAny(args)...)
			if err != nil {
				vm.aasmRaise(err)
			}
			return object.Bool(ok)
		})
	}
	// A plain reader/writer for the state column when the class has none, so
	// `obj.aasm_state` reports the current state (defaulting to the initial state).
	if lookupOwnOrIncluded(cls, spec.column) == nil {
		col := spec.column
		cls.define(col, func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			if v := getIvar(self, "@"+col); v != nil && !object.IsNil(v) {
				return v
			}
			if init := spec.initialState(); init != "" {
				return object.Symbol(init)
			}
			return object.NilV
		})
		cls.define(col+"=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			setIvar(self, "@"+col, args[0])
			return args[0]
		})
	}
}

// aasmFire runs an event against obj, raising the mapped AASM exception on a
// whiny failure and re-raising a Ruby exception that escaped a callback.
func (vm *VM) aasmFire(spec *aasmMachineSpec, obj object.Value, name string, args []object.Value, bang bool) object.Value {
	inst := vm.aasmBuild(spec, obj)
	goArgs := aasmToAny(args)
	var (
		ok  bool
		err error
	)
	if bang {
		ok, err = inst.FireBang(name, goArgs...)
	} else {
		ok, err = inst.Fire(name, goArgs...)
	}
	if err != nil {
		vm.aasmRaise(err)
	}
	return object.Bool(ok)
}

// aasmToAny boxes fire arguments as []any for the engine (which hands them back
// to guards/callbacks unchanged).
func aasmToAny(args []object.Value) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = a
	}
	return out
}

// registerAASMInstanceBase installs the AASM::InstanceBase reader returned by
// `obj.aasm` / `Klass.aasm`: current_state (and its writer), states, events,
// permitted_events, initial_state, and the fire / fire! / may_fire? trio.
func (vm *VM) registerAASMInstanceBase(base *RClass) {
	specOf := func(self object.Value) *aasmMachineSpec {
		cls := getIvar(self, "@__class").(*RClass)
		name := aasmStateStr(getIvar(self, "@__name"))
		return vm.aasmSpecFor(cls, name)
	}
	objOf := func(self object.Value) (object.Value, bool) {
		o := getIvar(self, "@__obj")
		if o == nil || object.IsNil(o) {
			return nil, false
		}
		return o, true
	}

	base.define("current_state", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		spec := specOf(self)
		if obj, ok := objOf(self); ok {
			return object.Symbol(vm.aasmBuild(spec, obj).CurrentState())
		}
		if init := spec.initialState(); init != "" {
			return object.Symbol(init)
		}
		return object.NilV
	})
	base.define("current_state=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		spec := specOf(self)
		if obj, ok := objOf(self); ok {
			_ = vm.aasmSeams(spec, obj).SetState(arStr(args[0]))
		}
		return args[0]
	})
	base.define("states", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return aasmSymArray(aasmStateNames(specOf(self)))
	})
	base.define("events", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return aasmSymArray(aasmEventNames(specOf(self)))
	})
	base.define("initial_state", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if init := specOf(self).initialState(); init != "" {
			return object.Symbol(init)
		}
		return object.NilV
	})
	base.define("permitted_events", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		obj, ok := objOf(self)
		if !ok {
			return object.NewArrayFromSlice(nil)
		}
		return aasmSymArray(vm.aasmBuild(specOf(self), obj).PermittedEvents(aasmToAny(args)...))
	})
	base.define("may_fire?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		obj, ok := objOf(self)
		if !ok {
			return object.Bool(false)
		}
		res, err := vm.aasmBuild(specOf(self), obj).MayFire(arStr(args[0]), aasmToAny(args[1:])...)
		if err != nil {
			vm.aasmRaise(err)
		}
		return object.Bool(res)
	})
	base.define("fire", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		obj, _ := objOf(self)
		return vm.aasmFire(specOf(self), obj, arStr(args[0]), args[1:], false)
	})
	base.define("fire!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		obj, _ := objOf(self)
		return vm.aasmFire(specOf(self), obj, arStr(args[0]), args[1:], true)
	})
}

// aasmStateNames / aasmEventNames list a machine's state / event names in
// declaration order.
func aasmStateNames(spec *aasmMachineSpec) []string {
	out := make([]string, len(spec.states))
	for i := range spec.states {
		out[i] = spec.states[i].name
	}
	return out
}

func aasmEventNames(spec *aasmMachineSpec) []string {
	out := make([]string, len(spec.events))
	for i := range spec.events {
		out[i] = spec.events[i].name
	}
	return out
}

// aasmWrapper builds the AASM::InstanceBase reader for cls's machine name, bound
// to obj when one is given (an instance reader) or unbound (a class reader).
func (vm *VM) aasmWrapper(cls *RClass, name string, obj object.Value) object.Value {
	w := &RObject{class: vm.consts["AASM::InstanceBase"].(*RClass), ivars: map[string]object.Value{
		"@__class": cls,
		"@__name":  object.NewString(name),
	}}
	if obj != nil {
		w.ivars["@__obj"] = obj
	}
	return w
}

// hget looks up a keyword option by Symbol then String name.
func hget(h *object.Hash, key string) (object.Value, bool) {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}

// hgetv is hget returning nil (as NilV) when the key is absent.
func hgetv(h *object.Hash, key string) object.Value {
	if v, ok := hget(h, key); ok {
		return v
	}
	return object.NilV
}
