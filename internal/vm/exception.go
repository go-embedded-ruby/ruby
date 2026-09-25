// This file completes the built-in Exception protocol toward MRI 3.4/4.0:
// Exception#exception / .exception, #inspect, #==, #cause (with the auto-set at
// raise time and the raise cause: keyword), and the structured accessors of the
// specific exception classes (NameError#name/#receiver, NoMethodError#name/#args,
// KeyError#key/#receiver, FrozenError#receiver, StopIteration#result,
// SystemExit#status is defined in builtins.go, LocalJumpError#exit_value/#reason,
// UncaughtThrowError#tag/#value and SystemCallError#errno). Thread::Backtrace and
// its ::Location value objects back Exception#backtrace_locations.
package vm

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// undefinedMethodReceiver formats the "for …" tail MRI 4.0 appends to an
// "undefined method 'x'" NoMethodError, describing the receiver the failed call
// targeted. MRI renders nil/true/false literally; a named class or module as
// "class Name" / "module Name" and an anonymous one as "class #<Class:0x…>" /
// "module #<Module:0x…>"; an object carrying a per-object singleton class through
// its own #inspect-shaped "#<Class:0x…>" identity string (built here, never by
// dispatching #inspect); and any other object as "an instance of <ClassName>"
// (or the anonymous class's "#<Class:0x…>" repr when the class is unnamed).
func (vm *VM) undefinedMethodReceiver(self object.Value) string {
	if object.IsNil(self) {
		return "nil"
	}
	if b, ok := self.(object.Bool); ok {
		if bool(b) {
			return "true"
		}
		return "false"
	}
	if c, ok := self.(*RClass); ok {
		kind := "class"
		if c.isModule {
			kind = "module"
		}
		return kind + " " + vm.classDisplayName(c)
	}
	if vm.objSingleton(self) != nil {
		return vm.objectIdentityRepr(self)
	}
	return "an instance of " + vm.classDisplayName(vm.classOf(self))
}

// classDisplayName returns the class/module's name through its own (overridable)
// #name method — matching MRI, which builds the "for class …" / "an instance of
// …" message from receiver.name — or the anonymous "#<Class:0x…>" /
// "#<Module:0x…>" identity when #name yields no String (an anonymous class).
func (vm *VM) classDisplayName(c *RClass) string {
	if n, ok := vm.send(c, "name", nil, nil).(*object.String); ok {
		return n.Str()
	}
	return vm.anonClassOrModuleRepr(c)
}

// anonClassOrModuleRepr renders an anonymous class or module as MRI's
// "#<Class:0x…>" / "#<Module:0x…>" identity string.
func (vm *VM) anonClassOrModuleRepr(c *RClass) string {
	kind := "Class"
	if c.isModule {
		kind = "Module"
	}
	return fmt.Sprintf("#<%s:0x%016x>", kind, uint64(vm.refID(c)))
}

// objectIdentityRepr renders an ordinary object as MRI's default
// "#<ClassName:0x…>" #inspect form, built directly (never dispatching a
// user-defined #inspect).
func (vm *VM) objectIdentityRepr(self object.Value) string {
	return fmt.Sprintf("#<%s:0x%016x>", vm.classOf(self).name, uint64(vm.refID(self)))
}

// causeIvar holds an exception's cause (another exception, or nil). MRI keeps the
// cause in a hidden field; rbgo stores it in a double-underscore ivar so casual
// #instance_variables introspection does not surface it.
const causeIvar = "@__cause__"

// registerExceptionMethods completes the Exception instance/class protocol on the
// given Exception class and installs the accessors of the specific exception
// classes plus Thread::Backtrace::Location. It runs during VM setup, after the
// hierarchy and the base message/backtrace methods are in place.
func (vm *VM) registerExceptionMethods(cException *RClass) {
	// Exception#exception: with no argument returns the receiver itself; with one
	// argument returns a copy of the receiver whose message is replaced (message
	// coerced via #to_s). This is what Kernel#raise calls to re-message an
	// exception object.
	cException.define("exception", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		switch len(args) {
		case 0:
			return self
		case 1:
			dup := dupValue(self)
			setIvar(dup, "@message", object.NewString(vm.exceptionMessageArg(args[0])))
			return dup
		default:
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0..1)", len(args))
			return object.NilV
		}
	})
	// Exception.exception is a class method equivalent to Exception.new — defined
	// on the metaclass so every subclass inherits it (RuntimeError.exception).
	cException.smethods["exception"] = &Method{name: "exception", owner: cException,
		native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			return vm.send(self, "new", args, blk)
		}}

	// Exception#inspect: "#<ClassName: message>" for a one-line message,
	// "#<ClassName:\"...\">" (the inspected string, no space) when the message
	// spans lines, and just "ClassName" when the message is empty — matching MRI.
	cException.define("inspect", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.exceptionInspect(self))
	})

	// Exception#==: true when the other object is of the same class and its
	// #message and #backtrace compare equal (MRI compares message and backtrace,
	// dispatching #message/#backtrace on the operand so a duck-typed object works).
	cException.define("==", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		other := args[0]
		if self == other {
			return object.True
		}
		o, ok := other.(*RObject)
		if !ok || o.class != vm.classOf(self) {
			return object.False
		}
		if vm.exceptionMessageText(self) != vm.exceptionMessageText(other) {
			return object.False
		}
		return object.Bool(valueEql(getIvar(self, backtraceIvar), getIvar(other, backtraceIvar)))
	})

	// Exception#cause: the exception that was being handled ($!) when this one was
	// raised, forming a chain; nil when there was none.
	cException.define("cause", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, causeIvar)
	})

	cNameError := vm.consts["NameError"].(*RClass)
	// NameError.new(msg = nil, name = nil, receiver:) — the name that could not be
	// resolved is the second positional and the receiver an optional keyword, both
	// stamped so #name/#receiver report what the caller passed (MRI's signature).
	cNameError.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		args = vm.stampReceiverKwarg(self, args)
		if len(args) > 0 && !object.IsNil(args[0]) {
			setIvar(self, "@message", object.NewString(vm.exceptionMessageArg(args[0])))
		}
		if len(args) > 1 {
			setIvar(self, "@name", args[1])
		}
		return object.NilV
	})
	// NameError#receiver: the object on which the missing name was looked up.
	// MRI raises an ArgumentError ("no receiver is available") when none was
	// recorded; rbgo records one at every method-dispatch NameError, so an unset
	// receiver only happens for a bare NameError.new — return nil there. (Const /
	// class-variable NameErrors do not yet record a receiver either, so raising
	// here would regress those cases; see the receiver_spec residuals.)
	cNameError.define("receiver", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@receiver")
	})

	cNoMethodError := vm.consts["NoMethodError"].(*RClass)
	// NoMethodError.new(msg = nil, name = nil, args = [], priv = false, receiver:)
	// — MRI's signature: the message, the missing method name, the call's own
	// arguments, a private-call flag (accepted and ignored here) and the receiver
	// keyword, stamped so #name/#args/#receiver report the failed call.
	cNoMethodError.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		args = vm.stampReceiverKwarg(self, args)
		if len(args) > 0 && !object.IsNil(args[0]) {
			setIvar(self, "@message", object.NewString(vm.exceptionMessageArg(args[0])))
		}
		if len(args) > 1 {
			setIvar(self, "@name", args[1])
		}
		if len(args) > 2 {
			setIvar(self, "@args", args[2])
		}
		return object.NilV
	})
	// NoMethodError#args: the arguments passed in the failed call (empty array by
	// default). #name is inherited from NameError (defined in builtins.go).
	cNoMethodError.define("args", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if a := getIvar(self, "@args"); a != object.NilV {
			return a
		}
		return object.NewArray()
	})

	cKeyError := vm.consts["KeyError"].(*RClass)
	// KeyError#key / #receiver: the missing key and the Hash it was fetched from.
	cKeyError.define("key", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@key")
	})
	cKeyError.define("receiver", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@receiver")
	})

	cFrozenError := vm.consts["FrozenError"].(*RClass)
	// FrozenError#receiver: the frozen object whose modification was attempted.
	cFrozenError.define("receiver", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@receiver")
	})

	cStopIteration := vm.consts["StopIteration"].(*RClass)
	// StopIteration#result: the value the finished iteration returned (the return
	// value of the each/loop body), nil when unset.
	cStopIteration.define("result", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@result")
	})

	cLocalJumpError := vm.consts["LocalJumpError"].(*RClass)
	// LocalJumpError#exit_value / #reason: the value carried by the jump (e.g. the
	// operand of an unexpected `return`/`break`) and the kind of jump (:return,
	// :break, :noreturn ...). Default reason is :noreturn.
	cLocalJumpError.define("exit_value", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@exit_value")
	})
	cLocalJumpError.define("reason", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if r := getIvar(self, "@reason"); r != object.NilV {
			return r
		}
		return object.Symbol("noreturn")
	})

	cUncaughtThrowError := vm.consts["UncaughtThrowError"].(*RClass)
	// UncaughtThrowError#tag / #value: the tag thrown with no matching catch and
	// the value thrown alongside it.
	cUncaughtThrowError.define("tag", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@tag")
	})
	cUncaughtThrowError.define("value", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@value")
	})

	vm.registerSystemCallError()

	vm.registerBacktraceLocation()
	vm.registerThreadBacktrace()
}

// exceptionMessageArg coerces a #exception / #initialize message argument to its
// string form: a String is taken as-is, anything else is sent #to_s.
func (vm *VM) exceptionMessageArg(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return vm.send(v, "to_s", nil, nil).ToS()
}

// exceptionInspect renders Exception#inspect (see the method comment above).
// The message is taken through rb_obj_as_string(exc) — MRI's exc_inspect
// (ruby/ruby v3_4_0 error.c:1838) dispatches #to_s rather than reading the
// stored message, so a subclass that overrides #to_s changes what #inspect
// reports; a #to_s that does not return a String falls back to the object's own
// identity representation, as rb_any_to_s does.
func (vm *VM) exceptionInspect(self object.Value) string {
	cls := vm.classOf(self).name
	msg := vm.objAsString(self)
	if msg == "" {
		return cls
	}
	if strings.ContainsRune(msg, '\n') {
		return "#<" + cls + ":" + strconv.Quote(msg) + ">"
	}
	return "#<" + cls + ": " + msg + ">"
}

// autoCause stamps exc's cause with the exception currently being handled ($!),
// the way MRI links a new exception to the one whose rescue it was raised inside.
// It is a no-op when there is no current exception, when exc *is* that exception
// (a re-raise), or when a cause is already recorded (an explicit cause: wins).
func (vm *VM) autoCause(exc object.Value) {
	if object.IsNil(vm.curExc) || vm.curExc == exc {
		return
	}
	if getIvar(exc, causeIvar) != object.NilV {
		return
	}
	setIvar(exc, causeIvar, vm.curExc)
}

// applyRaiseCause resolves the cause of an exception at a Kernel#raise: an
// explicit cause: keyword (which must be nil or an Exception) overrides, and a
// nil explicit cause deliberately suppresses the auto-cause; with no keyword the
// current exception is linked automatically.
func (vm *VM) applyRaiseCause(exc object.Value, causeGiven bool, causeVal object.Value) {
	if !causeGiven {
		vm.autoCause(exc)
		return
	}
	if !object.IsNil(causeVal) && !classIsA(vm.classOf(causeVal), vm.consts["Exception"].(*RClass)) {
		raise("TypeError", "exception object expected")
	}
	setIvar(exc, causeIvar, causeVal)
}

// stampReceiverKwarg peels a trailing `receiver:` keyword off a NameError /
// NoMethodError constructor argument list, stamping @receiver, and returns the
// remaining positional args. The trailing Hash is consumed only when it carries a
// :receiver key, so a genuine positional Hash argument is left untouched.
func (vm *VM) stampReceiverKwarg(self object.Value, args []object.Value) []object.Value {
	if len(args) == 0 {
		return args
	}
	h, ok := args[len(args)-1].(*object.Hash)
	if !ok {
		return args
	}
	if v, ok := h.Get(object.Symbol("receiver")); ok {
		setIvar(self, "@receiver", v)
		return args[:len(args)-1]
	}
	return args
}

// popCauseKwarg splits a trailing `cause:` keyword out of a Kernel#raise argument
// list. The keyword hash is recognised only when its sole key is :cause, so a
// genuine Hash message (raise SomeError, {...}) is left untouched.
func popCauseKwarg(args []object.Value) (rest []object.Value, given bool, cause object.Value) {
	if len(args) == 0 {
		return args, false, object.NilV
	}
	h, ok := args[len(args)-1].(*object.Hash)
	if !ok || len(h.Keys) != 1 {
		return args, false, object.NilV
	}
	k := h.Keys[0]
	if sym, isSym := k.(object.Symbol); !isSym || sym != object.Symbol("cause") {
		return args, false, object.NilV
	}
	v, _ := h.Get(k)
	return args[:len(args)-1], true, v
}

// raiseWithIvars builds an exception object of the named class carrying @message
// plus the given structured ivars (skipping nil values), stamps its backtrace and
// panics — the way an internal raise that must carry data (NoMethodError#name,
// KeyError#key, UncaughtThrowError#tag ...) reaches a rescue.
func (vm *VM) raiseWithIvars(class, msg string, ivars map[string]object.Value) {
	// class is always one of the built-in exception constants stamped by an
	// internal raise site, so the assertion holds.
	cls := vm.consts[class].(*RClass)
	iv := map[string]object.Value{"@message": object.NewString(msg)}
	order := []string{"@message"}
	for k, v := range ivars {
		if v != nil {
			iv[k] = v
			order = append(order, k)
		}
	}
	obj := &RObject{class: cls, ivars: iv, ivarOrder: order}
	panic(vm.excError(vm.captureBacktrace(obj)))
}

// registerBacktraceLocation installs Thread::Backtrace and its ::Location value
// class, whose instances back Exception#backtrace_locations. Each Location is
// parsed from a captured backtrace line ("path:lineno:in 'label'") and answers
// #path, #lineno, #label, #to_s and #inspect.
func (vm *VM) registerBacktraceLocation() {
	// Thread is registered before the Exception protocol, so it is always present.
	thread := vm.consts["Thread"].(*RClass)
	backtrace := newClass("Thread::Backtrace", vm.cObject)
	thread.consts["Backtrace"] = backtrace
	loc := newClass("Thread::Backtrace::Location", vm.cObject)
	backtrace.consts["Location"] = loc
	vm.consts["Thread::Backtrace::Location"] = loc
	vm.backtraceLocationClass = loc

	loc.define("path", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@path")
	})
	loc.define("lineno", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@lineno")
	})
	loc.define("label", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@label")
	})
	locToS := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@__str")
	}
	loc.define("to_s", locToS)
	loc.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(strconv.Quote(getIvar(self, "@__str").ToS()))
	})
	// #absolute_path aliases #path here (rbgo carries no distinct absolute path).
	loc.define("absolute_path", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@path")
	})
	// #base_label is the label with NO decoration at all. MRI reads it from a
	// different field than #label does — location.base_label beside
	// location.label (vm_backtrace.c v3_4_0:331) — and that field is the plain
	// name the ISeq was compiled under, so it carries neither the "block in "
	// that calculate_iseq_label adds for a block frame nor the "Owner#" that
	// rb_gen_method_name adds for a method one. #label says
	// "block (2 levels) in C#foo" where #base_label says "foo". Removing both
	// decorations reconstructs the same answer from the one label we carry.
	loc.define("base_label", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		_, base := splitBlockQualifier(getIvar(self, "@label").ToS())
		return object.NewString(stripOwnerPrefix(base))
	})
}

// splitBlockQualifier splits the "block in " / "block (N levels) in " decoration
// a block frame's label carries from the label of the scope the block was
// written in. A label without the decoration comes back with an empty qualifier.
func splitBlockQualifier(label string) (qual, base string) {
	if rest, ok := strings.CutPrefix(label, "block in "); ok {
		return "block in ", rest
	}
	if strings.HasPrefix(label, "block (") {
		if i := strings.Index(label, " levels) in "); i >= 0 {
			cut := i + len(" levels) in ")
			return label[:cut], label[cut:]
		}
	}
	return "", label
}

// stripOwnerPrefix removes the "Owner#" / "Owner." that genMethodName puts in
// front of a method label, yielding the bare name MRI keeps in
// location.base_label.
//
// It removes a prefix only when what precedes the separator is a CONSTANT PATH,
// which is the only shape rb_mod_name0 can produce: a run of "::"-joined
// segments each starting with an upper-case letter. That test is what keeps it
// from eating part of a label that merely contains the separator — a method name
// cannot hold a "#" or a ".", but "<main>" and the other bracketed labels reach
// here too, and an undecorated label must come back untouched.
func stripOwnerPrefix(label string) string {
	cut := strings.LastIndexAny(label, "#.")
	if cut <= 0 || cut == len(label)-1 {
		return label
	}
	if !isConstantPath(label[:cut]) {
		return label
	}
	return label[cut+1:]
}

// backtraceLocation builds a Thread::Backtrace::Location from one captured
// backtrace line of the form "path:lineno:in 'label'". Missing pieces degrade
// gracefully: the whole line becomes the path when it does not parse.
func (vm *VM) backtraceLocation(line string) object.Value {
	path, label := line, ""
	var lineno int64
	if i := strings.Index(line, ":in '"); i >= 0 {
		label = strings.TrimSuffix(line[i+len(":in '"):], "'")
		path = line[:i]
	}
	if j := strings.LastIndex(path, ":"); j >= 0 {
		if n, err := strconv.ParseInt(path[j+1:], 10, 64); err == nil {
			lineno = n
			path = path[:j]
		}
	}
	iv := map[string]object.Value{
		"@path":   object.NewString(path),
		"@lineno": object.IntValue(lineno),
		"@label":  object.NewString(label),
		"@__str":  object.NewString(line),
	}
	return &RObject{class: vm.backtraceLocationClass,
		ivars:     iv,
		ivarOrder: []string{"@path", "@lineno", "@label", "@__str"}}
}

// registerThreadBacktrace installs Thread#backtrace and Thread#backtrace_locations.
// They report the frames of a thread rather than of the caller, so unlike
// Kernel#caller they count from the frame that CALLED them: the first entry
// describes the #backtrace call itself, and dropping it gives exactly
// caller(0). A dead thread reports nil. Reference: ruby/ruby v3_4_0 vm_backtrace.c
// rb_thread_backtrace_m / rb_thread_backtrace_locations_m → thread_backtrace_to_ary.
func (vm *VM) registerThreadBacktrace() {
	thread := vm.consts["Thread"].(*RClass)
	thread.define("backtrace", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		frames, ok := vm.threadBacktraceFrames(self, args, "backtrace")
		if !ok {
			return object.NilV
		}
		return object.NewArrayFromSlice(frames)
	})
	thread.define("backtrace_locations", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		frames, ok := vm.threadBacktraceFrames(self, args, "backtrace_locations")
		if !ok {
			return object.NilV
		}
		locs := make([]object.Value, len(frames))
		for i, f := range frames {
			locs[i] = vm.backtraceLocation(f.ToS())
		}
		return object.NewArrayFromSlice(locs)
	})
}

// threadBacktraceFrames renders the receiver thread's backtrace, sliced by the
// same argument forms Kernel#caller accepts (a start level, a start and a
// length, or a Range), and reports whether anything is left to return: false
// means nil — a dead thread, or a start past the top of the stack.
//
// The frame list starts with a synthetic entry for the call being served, which
// is what makes `t.backtrace_locations(1..-1)` equal `caller_locations(0..-1)`
// and the default start 0 rather than Kernel#caller's 1. Only the running
// thread's own stack can be walked here, so another live thread reports the
// empty backtrace MRI allows for a thread that has not started executing.
func (vm *VM) threadBacktraceFrames(self object.Value, args []object.Value, label string) ([]object.Value, bool) {
	t, ok := self.(*RThread)
	if !ok || t.isDone() {
		return nil, false
	}
	if t != vm.currentThread {
		// rbgo keeps ONE frame stack, shared by every thread under the GVL, so the
		// frames of a thread that is not the running one are simply not recorded
		// anywhere. Refusing is the honest answer: returning an empty Array would
		// assert that the thread is executing nothing, which ruby/spec's
		// fixtures/code/concurrent.rb reads as a fact and spins on forever.
		raise("NotImplementedError",
			"Thread#%s of another thread is not supported: rbgo records one frame stack per VM, not per thread", label)
	}
	here, line := "", 0
	if n := len(vm.frameNames); n > 0 {
		here, line = vm.frameFileLabel(n-1), vm.frameLine(n-1)
	}
	// Thread#backtrace counts its own call as the innermost frame, and that frame
	// is the caller's: the line is where #backtrace was WRITTEN, which is the
	// innermost recorded frame's current pc. ruby/spec pins exactly that —
	// core/thread/backtrace_locations_spec.rb matches the first location against
	// the line of the `backtrace_locations` call itself.
	full := []object.Value{object.NewString(formatBacktraceEntry(here, line, label))}
	full = append(full, vm.backtraceFrames(0)...)
	return sliceBacktraceFrames(vm, full, args)
}

// sliceBacktraceFrames applies Thread#backtrace's start/length/range arguments to
// a rendered frame list. It is Kernel#caller's slicing with a default start of 0
// (a thread backtrace counts its own call), and reports false where MRI returns
// nil — a start beyond the end of the stack.
func sliceBacktraceFrames(vm *VM, full []object.Value, args []object.Value) ([]object.Value, bool) {
	arr := object.NewArrayFromSlice(full)
	if len(args) >= 1 {
		if _, isRange := args[0].(*object.Range); isRange {
			res := vm.send(arr, "[]", []object.Value{args[0]}, nil)
			if object.IsNil(res) {
				return nil, false
			}
			return res.(*object.Array).Elems, true
		}
	}
	start := int64(0)
	if len(args) >= 1 {
		start = vm.toIntCoerce(args[0])
	}
	if start < 0 {
		raise("ArgumentError", "negative level (%d)", start)
	}
	length := int64(len(full)) + 1
	if len(args) >= 2 && !object.IsNil(args[1]) {
		length = vm.toIntCoerce(args[1])
		if length < 0 {
			raise("ArgumentError", "negative size (%d)", length)
		}
	}
	res := vm.send(arr, "[]", []object.Value{object.IntValue(start), object.IntValue(length)}, nil)
	if object.IsNil(res) {
		return nil, false
	}
	return res.(*object.Array).Elems, true
}

// errnoIvar holds a SystemCallError's error number. MRI stores it under the
// hidden id_errno (no @), so it does not show up in #instance_variables; rbgo has
// no hidden-ivar slot, so it follows the @__name__ convention used for the
// backtrace and cause.
const errnoIvar = "@__errno__"

// registerSystemCallError installs SystemCallError's own protocol: #initialize
// (which both builds the message and decides which Errno::Exxx class the object
// ends up being), #errno and the .=== that makes `rescue Errno::EINVAL` match by
// error number rather than by class. Sources: ruby/ruby v3_4_0 error.c —
// syserr_initialize (error.c:3104), syserr_errno (error.c:3153) and syserr_eqq
// (error.c:3168).
func (vm *VM) registerSystemCallError() {
	cls := vm.consts["SystemCallError"].(*RClass)

	cls.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		vm.syserrInitialize(self, args)
		return object.NilV
	})

	// SystemCallError#errno is MRI's rb_attr_get(self, id_errno): the value
	// #initialize stored, which is the argument AS GIVEN (SystemCallError.new("x",
	// 2.9).errno is 2.9, even though the class lookup truncated it to 2). It is
	// nil for a generic SystemCallError built with no error number. An Errno::Exxx
	// raised internally never ran #initialize, so its class's own Errno constant
	// is the fallback.
	cls.define("errno", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if e := getIvar(self, errnoIvar); e != object.NilV {
			return e
		}
		return vm.classErrno(vm.classOf(self))
	})

	// SystemCallError.=== matches by ERROR NUMBER, not by class: a generic
	// SystemCallError matches any SystemCallError, and an Errno::Exxx matches
	// anything whose #errno equals its own Errno constant. That is what lets a
	// `rescue Errno::EINVAL` catch a SystemCallError.new("foo", EINVAL) that was
	// never given EINVAL's class.
	cls.smethods["==="] = &Method{name: "===", owner: cls, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		return object.Bool(vm.syserrEqq(self.(*RClass), args[0]))
	}}
}

// classErrno reads the Errno constant a class carries (Errno::ENOENT::Errno),
// walking up the ancestry — so a user subclass of Errno::ENOENT inherits it — and
// stopping before Object, whose constant table is the top level and holds the
// unrelated Errno *module*. It returns nil when no ancestor carries one.
func (vm *VM) classErrno(c *RClass) object.Value {
	for ; c != nil && c != vm.cObject; c = c.super {
		if e, ok := c.consts["Errno"]; ok {
			return e
		}
	}
	return object.NilV
}

// syserrEqq is MRI's syserr_eqq (ruby/ruby v3_4_0 error.c:3168): a non-
// SystemCallError that does not even answer #errno never matches; a generic
// SystemCallError receiver matches every SystemCallError; otherwise the
// argument's #errno is compared with the receiver's own Errno constant.
func (vm *VM) syserrEqq(recv *RClass, exc object.Value) bool {
	generic := vm.consts["SystemCallError"].(*RClass)
	if !classIsA(vm.classOf(exc), generic) {
		if !vm.respondsTo(exc, "errno") {
			return false
		}
	} else if recv == generic {
		return true
	}
	num := getIvar(exc, errnoIvar)
	if num == object.NilV {
		num = vm.send(exc, "errno", nil, nil)
	}
	e := vm.classErrno(recv)
	if a, ok := num.(object.Integer); ok {
		b, ok := e.(object.Integer)
		return ok && a == b
	}
	return vm.send(num, "==", []object.Value{e}, nil).Truthy()
}

// syserrInitialize is MRI's syserr_initialize (ruby/ruby v3_4_0 error.c:3104).
// Two shapes share one method:
//
//   - On SystemCallError itself: (msg, errno = nil, func = nil), except that a
//     lone Integer argument IS the errno. When the number has a registered class
//     the object BECOMES an instance of it — MRI rewrites the receiver's class in
//     place (RBASIC_SET_CLASS), which is why SystemCallError.new(Errno::EINVAL::
//     Errno).instance_of?(Errno::EINVAL) holds.
//   - On a subclass (Errno::EINVAL, or a user subclass of one): (msg = nil,
//     func = nil), with the error number taken from the class's Errno constant.
//
// The message is then strerror(errno) — "unknown error" when there is no number —
// with " @ <func>" and " - <msg>" appended when those were given, so
// Errno::EINVAL.new("custom", "loc").message is
// "Invalid argument @ loc - custom".
func (vm *VM) syserrInitialize(self object.Value, args []object.Value) {
	var mesg, errVal, fn object.Value = object.NilV, object.NilV, object.NilV
	if vm.classOf(self) == vm.consts["SystemCallError"].(*RClass) {
		if len(args) < 1 || len(args) > 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..3)", len(args))
		}
		mesg, errVal, fn = args[0], argAt(args, 1), argAt(args, 2)
		// A single Integer argument is the errno, not the message. MRI tests
		// FIXNUM_P, so a Float or a String stays the message (and a Float then
		// fails StringValue below, as it does in MRI).
		if i, ok := args[0].(object.Integer); ok && len(args) == 1 {
			mesg, errVal = object.NilV, i
		}
		if !object.IsNil(errVal) {
			vm.becomeErrnoClass(self, coerceInt(vm, errVal))
		}
	} else {
		if len(args) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0..2)", len(args))
		}
		mesg, fn = argAt(args, 0), argAt(args, 1)
		errVal = vm.classErrno(vm.classOf(self))
	}

	msg := "unknown error"
	if !object.IsNil(errVal) {
		msg = errnoStrerror(coerceInt(vm, errVal))
	}
	if !object.IsNil(mesg) {
		// StringValue(mesg) first: a Symbol message is a TypeError even though the
		// location is appended before it in the result.
		text := vm.coerceFormatString(mesg)
		if !object.IsNil(fn) {
			msg += " @ " + vm.send(fn, "to_s", nil, nil).ToS()
		}
		msg += " - " + text
	}
	setIvar(self, "@message", object.NewString(msg))
	setIvar(self, errnoIvar, errVal)
}

// becomeErrnoClass rewrites self's class to the Errno::Exxx registered for errno
// number n, the way MRI's syserr_initialize does with RBASIC_SET_CLASS. A number
// no class claims leaves the object a generic SystemCallError, and a receiver
// that is not a plain object is MRI's "invalid instance type" TypeError.
func (vm *VM) becomeErrnoClass(self object.Value, n int64) {
	c := vm.errnoClass(n)
	if c == nil {
		return
	}
	o, ok := self.(*RObject)
	if !ok {
		raise("TypeError", "invalid instance type")
	}
	o.class = c
}
