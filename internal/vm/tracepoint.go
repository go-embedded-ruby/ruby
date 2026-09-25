// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

// TracePoint: the object, its event set, and the hook list the VM fires into.
//
// The shape here is MRI's, read from vm_trace.c and iseq.c at v3_4_0 rather
// than reconstructed from the documentation:
//
//   - the event set is the bit field of include/ruby/internal/event.h
//     (RUBY_EVENT_LINE 0x0001 … RUBY_EVENT_RESCUE 0x4000), and
//     symbol2event_flag (vm_trace.c:804) is the name→bit table, including its
//     "joke" aggregates :a_call and :a_return;
//   - a TracePoint is created disabled and carries a Proc; tracepoint_new_s
//     (vm_trace.c:1490) defaults the event set to RUBY_EVENT_TRACEPOINT_ALL
//     when no event is named and raises ArgumentError "must be called with a
//     block" when there is no block — and it validates the events BEFORE it
//     looks for the block, which is why `TracePoint.new(:test)` reports the
//     unknown event rather than the missing block;
//   - enable/disable are tracepoint_enable_m / tracepoint_disable_m
//     (vm_trace.c:1375 / :1424): each answers the PREVIOUS tracing state, and
//     the block form scopes the change and restores that previous state on the
//     way out (MRI's rb_ensure with rb_tracepoint_enable/disable as the
//     ensure);
//   - the accessors are rb_tracearg_* (vm_trace.c:862-1080), all of which read
//     ec->trace_arg and raise RuntimeError "access from outside" when it is
//     NULL (get_trace_arg, vm_trace.c:846). That same pointer is the REENTRY
//     GUARD: rb_exec_event_hooks (vm_trace.c:434) fires nothing while it is
//     set, which is what keeps a handler's own execution from tracing itself;
//   - TracePoint.allow_reentry (vm_trace.c:1607) clears it for the duration of
//     a block, and restores it in an ensure;
//   - target:/target_line: are rb_tracepoint_enable_for_target
//     (vm_trace.c:1234): the target is resolved to an ISeq, hooks are attached
//     to it and its children recursively, and attaching NOTHING is the
//     ArgumentError "can not enable any hooks".
//
// What rbgo fires and what it does not is recorded at supportedTraceEvents.
package vm

import (
	"fmt"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// traceEvents is MRI's rb_event_flag_t, restricted to the TracePoint half of
// the bit space (include/ruby/internal/event.h, v3_4_0). The values are MRI's
// literal bits, not a re-numbering, so a set read from one side reads the same
// on the other.
type traceEvents uint32

const (
	evLine           traceEvents = 0x0001
	evClass          traceEvents = 0x0002
	evEnd            traceEvents = 0x0004
	evCall           traceEvents = 0x0008
	evReturn         traceEvents = 0x0010
	evCCall          traceEvents = 0x0020
	evCReturn        traceEvents = 0x0040
	evRaise          traceEvents = 0x0080
	evBCall          traceEvents = 0x0100
	evBReturn        traceEvents = 0x0200
	evThreadBegin    traceEvents = 0x0400
	evThreadEnd      traceEvents = 0x0800
	evFiberSwitch    traceEvents = 0x1000
	evScriptCompiled traceEvents = 0x2000
	evRescue         traceEvents = 0x4000

	// evTracePointAll is RUBY_EVENT_TRACEPOINT_ALL: the set TracePoint.new gets
	// when it is given no event name at all (tracepoint_new_s, vm_trace.c:1490).
	evTracePointAll traceEvents = 0xffff
)

// supportedTraceEvents is the subset rbgo actually FIRES. A TracePoint may name
// any MRI event — an unknown name is still an ArgumentError, and an event
// outside this set is accepted and simply never delivered, exactly as a
// TracePoint for an event the program never reaches behaves.
//
// Left out, and why (each needs a hook in a file this change does not own, or a
// VM facility that does not exist yet):
//
//   - :c_call / :c_return — a native body is invoked from callNative
//     (object_model.go), and only from there; the OpSend sites in this file see
//     a resolved *Method on the fast path but not on the send / method_missing
//     / operator fallbacks, so hooking them here would fire for some native
//     calls and not others, which is worse than not firing.
//   - :raise — every Ruby-level raise funnels through excError/captureBacktrace
//     (builtins.go). The only raise-shaped point reachable from here is the
//     rescue dispatch, which is where :rescue fires; announcing :raise from
//     there would report the rescuing frame's line as the raise site.
//   - :script_compiled — eval compiles in eval.go / binding_eval_open.go, and
//     rbgo forms no "(eval at FILE:LINE)" path for the compiled unit.
//   - :thread_begin / :thread_end / :fiber_switch — thread.go / fiber.go.
const supportedTraceEvents = evLine | evClass | evEnd | evCall | evReturn |
	evBCall | evBReturn | evRescue

// traceEventNames is symbol2event_flag's table (vm_trace.c:804) read forwards.
// The order is MRI's, and the two aggregates at the end are MRI's too — the
// source calls them a joke, but `TracePoint.new(:a_call)` really does register
// call|b_call|c_call.
var traceEventNames = []struct {
	name string
	bits traceEvents
}{
	{"line", evLine},
	{"class", evClass},
	{"end", evEnd},
	{"call", evCall},
	{"return", evReturn},
	{"c_call", evCCall},
	{"c_return", evCReturn},
	{"raise", evRaise},
	{"b_call", evBCall},
	{"b_return", evBReturn},
	{"thread_begin", evThreadBegin},
	{"thread_end", evThreadEnd},
	{"fiber_switch", evFiberSwitch},
	{"script_compiled", evScriptCompiled},
	{"rescue", evRescue},
	{"a_call", evCall | evBCall | evCCall},
	{"a_return", evReturn | evBReturn | evCReturn},
}

// eventName is get_event_id (vm_trace.c:667) read backwards: the Symbol name of
// a SINGLE event bit. An aggregate has no name of its own (MRI returns 0), and
// no trace_arg ever carries one, because an event is delivered one bit at a time.
func eventName(e traceEvents) string {
	for _, ev := range traceEventNames {
		if ev.bits == e {
			return ev.name
		}
	}
	return "unknown"
}

// tracePoint is MRI's rb_tp_t (vm_trace.c): the event mask, the handler Proc,
// whether it is currently registered, and the three filters a hook may carry
// (thread, local ISeq set, line).
//
// It doubles as the Go box hung off the Ruby object's @__tp ivar, so it
// satisfies object.Value; it is never itself a receiver, so vm.classOf needs no
// case for it.
type tracePoint struct {
	events  traceEvents
	blk     *Proc
	self    object.Value // the Ruby TracePoint the handler is called with
	tracing bool

	// targetTh is MRI's tp->target_th: when non-nil the hook fires only on that
	// thread. enable's `target_thread:` defaults to the CURRENT thread for the
	// block form with no target (tracepoint_enable_m, vm_trace.c:1379) and to no
	// filter otherwise.
	targetTh *RThread

	// localSeqs is MRI's local_target_set: non-nil exactly when the TracePoint
	// was enabled with `target:`, and holding the target ISeq plus every ISeq
	// nested inside it (rb_iseq_add_local_tracepoint_recursively, iseq.c:3912).
	// A local TracePoint fires only for an event whose ISeq is in the set.
	localSeqs map[*bytecode.ISeq]bool

	// targetLine is MRI's hook->filter.target_line: 0 for no filter, otherwise
	// only events on that source line are delivered (exec_hooks_body,
	// vm_trace.c:341).
	targetLine int
}

func (t *tracePoint) ToS() string     { return "#<TracePoint>" }
func (t *tracePoint) Inspect() string { return "#<TracePoint>" }
func (t *tracePoint) Truthy() bool    { return true }

// tpStateIvar is the hidden ivar the Go state hangs off. The name is the
// package's established shape for a Go-backed core object (see @__lexer,
// @__engine, @__tmpl elsewhere in this package).
const tpStateIvar = "@__tp"

// traceArg is MRI's rb_trace_arg_t: everything an event carries, built once at
// the fire site and read by the accessors while the handler runs. ec->trace_arg
// points at it; here vm.traceArg does.
//
// The frame pieces (env/definee/locals) are kept rather than a finished Binding
// because building one pins the frame's Env out of the reuse pool
// (markEnvCaptured), and most events are never asked for a binding.
type traceArg struct {
	event    traceEvents
	self     object.Value
	path     string
	line     int
	methodID string // "" renders as nil
	calleeID string
	klass    object.Value // defined_class; NilV when the event has none

	iseq   *bytecode.ISeq // the code the event came from; also the local-hook key
	isProc bool           // a non-lambda block frame: #parameters reports :opt

	// data is #return_value or #raised_exception, for the events that carry one.
	data object.Value

	env     *Env
	definee *RClass
	locals  []string
	file    string
}

// registerTracePoint installs the TracePoint class. It is called from New after
// the prelude, beside the other late registrations, so Object already exists.
func (vm *VM) registerTracePoint() {
	cls := newClass("TracePoint", vm.cObject)
	vm.consts["TracePoint"] = cls
	vm.cTracePoint = cls

	sdef := func(name string, fn NativeFn) {
		cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
	}

	// TracePoint.new(*events) { |tp| … } — tracepoint_new_s (vm_trace.c:1490).
	sdef("new", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.newTracePoint(tpClassOf(vm, self), args, blk)
	})
	// TracePoint.trace(*events) { |tp| … } — tracepoint_trace_s (vm_trace.c:1513):
	// new, then enable, then hand back the (already tracing) object.
	sdef("trace", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		tpv := vm.newTracePoint(tpClassOf(vm, self), args, blk)
		vm.tracepointEnable(tpOf(tpv))
		return tpv
	})
	// TracePoint.allow_reentry { … } — tracepoint_allow_reentry (vm_trace.c:1607).
	sdef("allow_reentry", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		return vm.traceAllowReentry(blk)
	})

	cls.define("enabled?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(tpOf(self).tracing)
	})
	cls.define("enable", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.tracePointEnableM(tpOf(self), args, blk)
	})
	cls.define("disable", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		return vm.tracePointDisableM(tpOf(self), blk)
	})
	cls.define("inspect", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.tracePointInspect(tpOf(self)))
	})

	// The trace-argument accessors. Each is rb_tracearg_* in vm_trace.c, and each
	// goes through currentTraceArg, which is get_trace_arg (vm_trace.c:846).
	cls.define("event", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.SymVal(eventName(vm.currentTraceArg().event))
	})
	cls.define("lineno", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(vm.currentTraceArg().line))
	})
	cls.define("path", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.currentTraceArg().path)
	})
	cls.define("self", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.currentTraceArg().self
	})
	cls.define("method_id", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return symOrNil(vm.currentTraceArg().methodID)
	})
	cls.define("callee_id", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return symOrNil(vm.currentTraceArg().calleeID)
	})
	cls.define("defined_class", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		a := vm.currentTraceArg()
		if a.klass == nil {
			return object.NilV
		}
		return a.klass
	})
	cls.define("return_value", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		a := vm.currentTraceArg()
		// rb_tracearg_return_value (vm_trace.c:1006): only the three return events
		// carry one.
		if a.event&(evReturn|evCReturn|evBReturn) == 0 {
			raise("RuntimeError", "not supported by this event")
		}
		return a.data
	})
	cls.define("raised_exception", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		a := vm.currentTraceArg()
		// rb_tracearg_raised_exception (vm_trace.c:1021).
		if a.event&(evRaise|evRescue) == 0 {
			raise("RuntimeError", "not supported by this event")
		}
		return a.data
	})
	cls.define("binding", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.traceArgBinding(vm.currentTraceArg())
	})
	cls.define("parameters", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return traceArgParameters(vm.currentTraceArg())
	})
}

// tpClassOf is the class a TracePoint.new lands on: the receiver when it is a
// class (so a subclass makes instances of itself, as tracepoint_new_s passes
// `self` straight to tracepoint_new), else TracePoint.
func tpClassOf(vm *VM, self object.Value) *RClass {
	if c, ok := self.(*RClass); ok {
		return c
	}
	return vm.cTracePoint
}

// symOrNil renders an optional ID: a Symbol, or nil for the absent one — what
// rb_tracearg_method_id / _callee_id do with a zero ID (vm_trace.c:960/:967).
func symOrNil(name string) object.Value {
	if name == "" {
		return object.NilV
	}
	return object.SymVal(name)
}

// tpOf unwraps the Go state from a Ruby TracePoint.
func tpOf(v object.Value) *tracePoint {
	if o, ok := v.(*RObject); ok {
		if tp, ok := getIvar(o, tpStateIvar).(*tracePoint); ok {
			return tp
		}
	}
	raise("TypeError", "wrong argument type %s (expected TracePoint)", classNameOf(v))
	return nil
}

// newTracePoint is tracepoint_new_s (vm_trace.c:1490).
func (vm *VM) newTracePoint(cls *RClass, args []object.Value, blk *Proc) object.Value {
	var events traceEvents
	if len(args) == 0 {
		events = evTracePointAll
	} else {
		for _, a := range args {
			events |= vm.symbolToEventFlag(a)
		}
	}
	// The block check comes AFTER the event loop, as in MRI: an unknown event is
	// reported even when no block was given.
	if blk == nil {
		raise("ArgumentError", "must be called with a block")
	}
	tp := &tracePoint{events: events, blk: blk}
	obj := &RObject{class: cls, ivars: map[string]object.Value{tpStateIvar: tp}}
	tp.self = obj
	return obj
}

// symbolToEventFlag is symbol2event_flag (vm_trace.c:804): coerce to a Symbol
// (rb_to_symbol_type — a Symbol passes, anything else must answer #to_sym WITH
// a Symbol), then look the name up, raising ArgumentError for an unknown one.
func (vm *VM) symbolToEventFlag(v object.Value) traceEvents {
	name := vm.toSymbolName(v)
	for _, ev := range traceEventNames {
		if ev.name == name {
			return ev.bits
		}
	}
	raise("ArgumentError", "unknown event: %s", name)
	return 0
}

// toSymbolName is rb_to_symbol_type: the Symbol's name, converting through
// #to_sym when the value is not already one. A String converts too (String has
// #to_sym); anything whose #to_sym answers a non-Symbol is a TypeError, which
// is the second half of new_spec's "not a string/symbol" example.
func (vm *VM) toSymbolName(v object.Value) string {
	if s, ok := v.(object.Symbol); ok {
		return string(s)
	}
	if vm.respondsToDynamic(v, "to_sym") {
		if s, ok := vm.send(v, "to_sym", nil, nil).(object.Symbol); ok {
			return string(s)
		}
	}
	raise("TypeError", "no implicit conversion of %s into Symbol", classNameOf(v))
	return ""
}

// currentTraceArg is get_trace_arg (vm_trace.c:846): the event being delivered,
// or RuntimeError "access from outside" when no handler is running.
func (vm *VM) currentTraceArg() *traceArg {
	if vm.traceArg == nil {
		raise("RuntimeError", "access from outside")
	}
	return vm.traceArg
}

// traceArgBinding is rb_tracearg_binding (vm_trace.c:981). A native-frame event
// has no Ruby frame to bind, and neither does an event fired without one.
func (vm *VM) traceArgBinding(a *traceArg) object.Value {
	if a.env == nil {
		return object.NilV
	}
	markEnvCaptured(a.env)
	return &Binding{env: a.env, self: a.self, definee: a.definee, file: a.file,
		names: append([]string(nil), a.locals...)}
}

// traceArgParameters is rb_tracearg_parameters (vm_trace.c:916): the call and
// return events (and their block forms) report the running ISeq's parameters,
// with a non-lambda block reporting its positionals as :opt; every other event
// raises.
//
// MRI's C-call arm (rb_unnamed_parameters over the method's arity) is NOT
// transcribed: rbgo raises no :c_call, so that answer could never be checked
// against anything. Add it with the event, not before.
func traceArgParameters(a *traceArg) object.Value {
	switch a.event {
	case evCall, evReturn, evBCall, evBReturn:
		if a.iseq == nil {
			return object.NilV
		}
		return procParameters(a.iseq, !a.isProc)
	}
	raise("RuntimeError", "not supported by this event")
	return object.NilV
}

// tracePointInspect is tracepoint_inspect (vm_trace.c:1521). Outside a handler
// it says only whether the TracePoint is on; inside one it describes the event.
//
// MRI distinguishes four shapes; the two here are the ones rbgo can reach. Its
// C-call shape is the same as :call/:return's, and its thread shape belongs to
// :thread_begin/:thread_end — neither of which rbgo raises, so neither is
// transcribed. Add each with its event.
func (vm *VM) tracePointInspect(tp *tracePoint) string {
	a := vm.traceArg
	if a == nil {
		if tp.tracing {
			return "#<TracePoint:enabled>"
		}
		return "#<TracePoint:disabled>"
	}
	switch a.event {
	case evLine:
		// A line inside a method names it; a line at top level or in a block that
		// belongs to no method has no method_id and falls through to the default.
		if a.methodID != "" {
			return fmt.Sprintf("#<TracePoint:line %s:%d in '%s'>", a.path, a.line, a.methodID)
		}
	case evCall, evReturn:
		return fmt.Sprintf("#<TracePoint:%s '%s' %s:%d>", eventName(a.event), a.methodID, a.path, a.line)
	}
	return fmt.Sprintf("#<TracePoint:%s %s:%d>", eventName(a.event), a.path, a.line)
}

// tracePointEnableM is tracepoint_enable_m (vm_trace.c:1375).
func (vm *VM) tracePointEnableM(tp *tracePoint, args []object.Value, blk *Proc) object.Value {
	previous := tp.tracing
	target, targetLine, targetThread, threadGiven := vm.tracePointEnableKeywords(args)

	// target_thread: defaults to the CURRENT thread, but only for the block form
	// with neither target: nor target_line:; every other shape defaults to no
	// filter at all.
	if !threadGiven {
		if blk != nil && target == nil && targetLine == nil {
			targetThread = vm.currentThread
		} else {
			targetThread = nil
		}
	}
	if targetThread != nil && !object.IsNil(targetThread) {
		th, ok := targetThread.(*RThread)
		if !ok {
			raise("TypeError", "wrong argument type %s (expected Thread)", classNameOf(targetThread))
		}
		if tp.targetTh != nil {
			raise("ArgumentError", "can not override target_thread filter")
		}
		tp.targetTh = th
	} else {
		tp.targetTh = nil
	}

	if target == nil || object.IsNil(target) {
		if targetLine != nil && !object.IsNil(targetLine) {
			raise("ArgumentError", "only target_line is specified")
		}
		vm.tracepointEnable(tp)
	} else {
		vm.tracepointEnableForTarget(tp, target, targetLine)
	}

	if blk == nil {
		return object.Bool(previous)
	}
	return vm.traceScopedBlock(tp, previous, blk)
}

// tracePointDisableM is tracepoint_disable_m (vm_trace.c:1424).
func (vm *VM) tracePointDisableM(tp *tracePoint, blk *Proc) object.Value {
	previous := tp.tracing
	if blk == nil {
		vm.tracepointDisable(tp)
		return object.Bool(previous)
	}
	if tp.localSeqs != nil {
		raise("ArgumentError", "can't disable a targeting TracePoint in a block")
	}
	vm.tracepointDisable(tp)
	return vm.traceScopedBlock(tp, previous, blk)
}

// traceScopedBlock runs the block form of enable/disable: yield with NO
// arguments (both specs assert the block receives none) and restore the
// PREVIOUS tracing state on the way out, however the block leaves — MRI's
// rb_ensure(rb_yield, …, previous_tracing ? enable : disable, tpval).
func (vm *VM) traceScopedBlock(tp *tracePoint, previous bool, blk *Proc) object.Value {
	defer func() {
		if previous {
			// Re-enabling a TracePoint that the block left targeting would raise, so
			// the restore drops any local target first, as MRI's rb_tracepoint_enable
			// cannot be reached with local_target_set still set.
			tp.localSeqs, tp.targetLine = nil, 0
			vm.tracepointEnable(tp)
		} else {
			vm.tracepointDisable(tp)
		}
	}()
	return vm.callBlock(blk, nil)
}

// tracePointEnableKeywords picks enable's three keywords out of a trailing Hash.
// threadGiven distinguishes `target_thread: nil` (no filter) from an absent
// keyword (MRI's sym_default sentinel), which choose differently.
func (vm *VM) tracePointEnableKeywords(args []object.Value) (target, targetLine, targetThread object.Value, threadGiven bool) {
	h := trailingKwHash(args)
	if h == nil {
		return nil, nil, nil, false
	}
	if v, ok := h.Get(object.SymVal("target")); ok {
		target = v
	}
	if v, ok := h.Get(object.SymVal("target_line")); ok {
		targetLine = v
	}
	if v, ok := h.Get(object.SymVal("target_thread")); ok {
		targetThread, threadGiven = v, true
	}
	return target, targetLine, targetThread, threadGiven
}

// tracepointEnable is rb_tracepoint_enable (vm_trace.c:1193): registering a
// TracePoint that is already targeting is the nest-enable ArgumentError, and
// re-enabling an already-enabled one is a no-op.
func (vm *VM) tracepointEnable(tp *tracePoint) {
	if tp.localSeqs != nil {
		raise("ArgumentError", "can't nest-enable a targeting TracePoint")
	}
	if tp.tracing {
		return
	}
	tp.tracing = true
	vm.tracePoints = append(vm.tracePoints, tp)
	vm.recomputeTraceEvents()
}

// tracepointDisable is rb_tracepoint_disable: unregister, and drop any local
// target with it (MRI's disable_local_event_iseq_i sweep clears
// local_target_set, so a disabled TracePoint can be enabled either way again).
func (vm *VM) tracepointDisable(tp *tracePoint) {
	tp.localSeqs, tp.targetLine = nil, 0
	if !tp.tracing {
		return
	}
	tp.tracing = false
	for i, other := range vm.tracePoints {
		if other == tp {
			vm.tracePoints = append(vm.tracePoints[:i:i], vm.tracePoints[i+1:]...)
			break
		}
	}
	vm.recomputeTraceEvents()
}

// recomputeTraceEvents refreshes the single word the interpreter loop tests.
// It is the whole of rbgo's answer to MRI's ruby_vm_event_enabled_global_flags:
// zero means no hook wants anything, and every hot-path gate is a test of it.
func (vm *VM) recomputeTraceEvents() {
	var all traceEvents
	for _, tp := range vm.tracePoints {
		all |= tp.events
	}
	vm.traceEvents = all & supportedTraceEvents
}

// tracepointEnableForTarget is rb_tracepoint_enable_for_target (vm_trace.c:1234).
func (vm *VM) tracepointEnableForTarget(tp *tracePoint, target, targetLine object.Value) {
	iseq, isMethod := vm.iseqOfTarget(target)
	if tp.tracing {
		raise("ArgumentError", "can't nest-enable a targeting TracePoint")
	}
	line := 0
	if targetLine != nil && !object.IsNil(targetLine) {
		if tp.events&evLine == 0 {
			raise("ArgumentError", "target_line is specified, but line event is not specified")
		}
		line = int(vm.toIntCoerce(targetLine))
	}

	set := map[*bytecode.ISeq]bool{}
	collectISeqs(iseq, set)
	n := 0
	for s := range set {
		// The root of a Method/UnboundMethod target is the only ISeq that can raise
		// a :call/:return; every nested one is a block body.
		producible := evBCall | evBReturn | evLine
		if s == iseq && isMethod {
			producible = evCall | evReturn | evLine
		}
		switch {
		case line != 0:
			// With target_line: only :line events count, and only on a line this
			// ISeq actually has.
			if s.HasLine(line) {
				n++
			}
		case tp.events&producible != 0:
			n++
		}
	}
	if n == 0 {
		raise("ArgumentError", "can not enable any hooks")
	}

	tp.localSeqs = set
	tp.targetLine = line
	tp.tracing = true
	vm.tracePoints = append(vm.tracePoints, tp)
	vm.recomputeTraceEvents()
}

// iseqOfTarget is iseq_of (vm_trace.c:1217), which MRI writes as
// `RubyVM::InstructionSequence.of(target)`: a Method, an UnboundMethod or a
// Proc yields its code, and anything else is the "specified target is not
// supported" ArgumentError. isMethod reports the first two, whose code can
// raise :call/:return rather than :b_call/:b_return.
func (vm *VM) iseqOfTarget(target object.Value) (iseq *bytecode.ISeq, isMethod bool) {
	// methodISeq (method.go) answers the code behind a method record — its own,
	// or its block's for a define_method body, and nil for a native, which is
	// what `InstructionSequence.of` answers for a C method.
	switch t := target.(type) {
	case *BoundMethod:
		if t.m != nil {
			iseq, isMethod = methodISeq(t.m), true
		}
	case *UnboundMethod:
		if t.m != nil {
			iseq, isMethod = methodISeq(t.m), true
		}
	case *Proc:
		iseq = t.iseq
	}
	if iseq == nil {
		raise("ArgumentError", "specified target is not supported")
	}
	return iseq, isMethod
}

// collectISeqs gathers an ISeq and everything nested inside it. This is
// rb_iseq_add_local_tracepoint_recursively (iseq.c:3912), which walks the child
// ISeqs so a `target:` covers the blocks written inside the target — the reason
// `trace.enable(target: obj.method(:foo))` sees the :b_call of a block three
// levels deep in foo but not one in a method foo merely calls.
func collectISeqs(s *bytecode.ISeq, into map[*bytecode.ISeq]bool) {
	if s == nil || into[s] {
		return
	}
	into[s] = true
	for _, c := range s.Children {
		collectISeqs(c, into)
	}
}

// traceAllowReentry is tracepoint_allow_reentry (vm_trace.c:1607): clear the
// reentry guard for the block, and put it back afterwards whatever happens.
func (vm *VM) traceAllowReentry(blk *Proc) object.Value {
	a := vm.traceArg
	if a == nil {
		raise("RuntimeError", "No need to allow reentrance.")
	}
	if blk == nil {
		raise("ArgumentError", "must be called with a block")
	}
	vm.traceArg = nil
	defer func() { vm.traceArg = a }()
	return vm.callBlock(blk, nil)
}

// fireTrace delivers one event to every hook that wants it. It is
// rb_exec_event_hooks + exec_hooks_body (vm_trace.c:412/:341) with MRI's three
// filters and — crucially — MRI's reentry guard: while vm.traceArg is set no
// further event is delivered, so a handler's own line/call/return events do not
// re-enter it.
//
// ORDER is observable, and it is not registration order. MRI keeps two lists
// and walks them in turn: vm_trace_hook (vm_insnhelper.c v3_4_0:7054) runs the
// GLOBAL hooks first and the ISeq's LOCAL hooks — the ones a `target:` attached
// — second. Within each list hook_list_connect (vm_trace.c) PREPENDS
// (`hook->next = list->hooks; list->hooks = hook`), so the most recently
// enabled hook is called first. ruby/spec pins all three combinations that
// distinguishes (enable_spec.rb:377/:399/:421): a targeting TracePoint enabled
// inside a global one reports [:outer, :inner], two targeting ones report
// [:inner, :outer], and a global one inside a targeting one reports
// [:inner, :outer] again — which no single ordering of one list can produce.
//
// The list is copied before iterating because a handler may enable or disable a
// TracePoint, which rewrites vm.tracePoints underneath the loop; MRI gets the
// same protection from its deleted-flag-plus-sweep scheme (clean_hooks_check).
func (vm *VM) fireTrace(a *traceArg) {
	if vm.traceArg != nil || len(vm.tracePoints) == 0 {
		return
	}
	list := append([]*tracePoint(nil), vm.tracePoints...)
	vm.traceArg = a
	defer func() { vm.traceArg = nil }()
	// Pass 0: the global hooks. Pass 1: the local (targeting) ones. Both
	// backwards, which is MRI's prepend read forwards.
	for pass := 0; pass < 2; pass++ {
		wantLocal := pass == 1
		for i := len(list) - 1; i >= 0; i-- {
			tp := list[i]
			if (tp.localSeqs != nil) != wantLocal {
				continue
			}
			if !tp.tracing || tp.events&a.event == 0 {
				continue
			}
			if tp.targetTh != nil && tp.targetTh != vm.currentThread {
				continue
			}
			if wantLocal && (a.iseq == nil || !tp.localSeqs[a.iseq]) {
				continue
			}
			if tp.targetLine != 0 && tp.targetLine != a.line {
				continue
			}
			vm.callBlock(tp.blk, []object.Value{tp.self})
		}
	}
}

// frameTracePath is the path a frame's events report. An ISeq loaded from a
// file names it; the top-level program's ISeq carries no file, and MRI still
// reports the script there (rb_iseq_path over the main iseq), which is what
// frameFileLabel answers.
func (vm *VM) frameTracePath(iseq *bytecode.ISeq, frame int) string {
	if iseq != nil && iseq.File != "" {
		return iseq.File
	}
	// frameFileLabel indexes the frame stacks unguarded, and exec can legitimately
	// outrun them (see the bounds note at the pc publication in exec): a frame no
	// longer covered reports the script, which is what frameFileLabel itself falls
	// back to for a frame with no file.
	if frame < 0 || frame >= len(vm.frameFiles) {
		frame = len(vm.frameFiles) - 1
	}
	if frame < 0 {
		return vm.scriptLabel()
	}
	return vm.frameFileLabel(frame)
}

// scriptLabel is frameFileLabel's answer for a frame with no file of its own,
// reached directly when there is no frame at all to ask.
func (vm *VM) scriptLabel() string {
	switch vm.scriptName {
	case "":
		return "(rbgo)"
	case "-e":
		return "-e"
	default:
		return vm.scriptName
	}
}

// traceLineEvent fires :line. MRI reports the CURRENT source line here (the
// LINE event is not in get_path_and_lineno's first_lineno set, vm_trace.c:694),
// which is the line the instruction about to run belongs to.
func (vm *VM) traceLineEvent(f *traceFrame, pc int) {
	vm.fireTrace(&traceArg{
		event:    evLine,
		self:     f.self,
		path:     vm.frameTracePath(f.iseq, f.frame),
		line:     f.iseq.LineAt(pc),
		methodID: f.methodID,
		calleeID: f.calleeID,
		klass:    f.klassValue(),
		iseq:     f.iseq,
		isProc:   f.isProc,
		env:      f.env,
		definee:  f.definee,
		locals:   f.locals,
		file:     f.file,
	})
}

// traceFrame is the slice of an exec frame the event sites need. It is built
// once per traced frame so the three fire sites (entry, line, exit) do not each
// re-derive it, and it is built ONLY when tracing is on.
type traceFrame struct {
	iseq     *bytecode.ISeq
	frame    int
	self     object.Value
	methodID string
	calleeID string
	definee  *RClass
	env      *Env
	locals   []string
	file     string
	isProc   bool

	// entry/exit are the pair of events this frame raises when it starts and
	// finishes: call/return, b_call/b_return or class/end. Zero for a frame that
	// raises neither (top level, eval).
	entry traceEvents
	exit  traceEvents
}

// klassValue renders defined_class: the owner of a method frame, nil elsewhere.
func (f *traceFrame) klassValue() object.Value {
	if f.definee == nil || f.entry != evCall {
		return nil
	}
	return f.definee
}

// newTraceFrame decides what a frame is — method, block, class body, or none of
// those — and packages what its events report.
//
// The mapping is MRI's frame typing: a method activation raises call/return, a
// block activation (lambda or not) raises b_call/b_return, and a class or
// module body raises class/end. A top-level or eval frame raises neither.
func newTraceFrame(iseq *bytecode.ISeq, frame int, self object.Value, fm frameMethod,
	definee *RClass, env *Env, file string, selfBlock *Proc, methodName string, classBody bool,
) *traceFrame {
	f := &traceFrame{
		iseq: iseq, frame: frame, self: self,
		methodID: fm.orig, calleeID: fm.callee,
		definee: definee, env: env, locals: iseq.Locals, file: file,
	}
	switch {
	case selfBlock != nil:
		f.entry, f.exit = evBCall, evBReturn
		f.isProc = !selfBlock.isLambda
	case classBody:
		f.entry, f.exit = evClass, evEnd
		// A class body is not a method: it reports no method_id / callee_id, as
		// rb_vm_control_frame_id_and_class finds no method entry for a CLASS frame.
		f.methodID, f.calleeID = "", ""
	case methodName != "":
		f.entry, f.exit = evCall, evReturn
	}
	return f
}

// traceEntryEvent fires :call / :b_call / :class. MRI reports the ISeq's
// FIRST line for all three, not the current one (get_path_and_lineno,
// vm_trace.c:694, singles out CLASS|CALL|B_CALL) — so a :call event names the
// `def` line however far into the body the pc has moved.
func (vm *VM) traceEntryEvent(f *traceFrame) {
	vm.fireTrace(&traceArg{
		event:    f.entry,
		self:     f.self,
		path:     vm.frameTracePath(f.iseq, f.frame),
		line:     f.iseq.FirstLine,
		methodID: f.methodID,
		calleeID: f.calleeID,
		klass:    f.klassValue(),
		iseq:     f.iseq,
		isProc:   f.isProc,
		env:      f.env,
		definee:  f.definee,
		locals:   f.locals,
		file:     f.file,
	})
}

// traceExitEvent fires :return / :b_return / :end, carrying the frame's value
// as #return_value for the two return forms.
func (vm *VM) traceExitEvent(f *traceFrame, result object.Value) {
	vm.fireTrace(&traceArg{
		event:    f.exit,
		self:     f.self,
		path:     vm.frameTracePath(f.iseq, f.frame),
		line:     vm.frameLine(f.frame),
		methodID: f.methodID,
		calleeID: f.calleeID,
		klass:    f.klassValue(),
		iseq:     f.iseq,
		isProc:   f.isProc,
		data:     result,
		env:      f.env,
		definee:  f.definee,
		locals:   f.locals,
		file:     f.file,
	})
}

// traceRescueEvent fires :rescue as a frame's rescue clause takes over. MRI
// raises it from vm_insnhelper's exception path with the exception as #data
// (rb_tracearg_raised_exception accepts RAISE and RESCUE alike).
func (vm *VM) traceRescueEvent(f *traceFrame, exc object.Value) {
	vm.fireTrace(&traceArg{
		event:    evRescue,
		self:     f.self,
		path:     vm.frameTracePath(f.iseq, f.frame),
		line:     vm.frameLine(f.frame),
		methodID: f.methodID,
		calleeID: f.calleeID,
		iseq:     f.iseq,
		isProc:   f.isProc,
		data:     exc,
		env:      f.env,
		definee:  f.definee,
		locals:   f.locals,
		file:     f.file,
	})
}
