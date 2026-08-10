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
	"strconv"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

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
	// NameError#receiver: the object on which the missing name was looked up.
	// MRI raises an ArgumentError ("no receiver is available") when none was
	// recorded; rbgo records one at every method-dispatch NameError, so an unset
	// receiver only happens for a bare NameError.new — return nil there.
	cNameError.define("receiver", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@receiver")
	})

	cNoMethodError := vm.consts["NoMethodError"].(*RClass)
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

	// SystemCallError#errno: the platform errno of the Errno::* subclass — its
	// class-level Errno constant (Errno::ENOENT::Errno). nil for a bare
	// SystemCallError with no errno.
	vm.consts["SystemCallError"].(*RClass).define("errno", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		// Walk the class ancestry up to (but not including) Object, whose constant
		// table is the top level and holds the unrelated Errno *module*. Each
		// Errno::Exxx carries its own integer Errno constant.
		for c := vm.classOf(self); c != nil && c != vm.cObject; c = c.super {
			if e, ok := c.consts["Errno"]; ok {
				return e
			}
		}
		return object.NilV
	})

	vm.registerBacktraceLocation()
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
func (vm *VM) exceptionInspect(self object.Value) string {
	cls := vm.classOf(self).name
	msg := vm.exceptionMessageText(self)
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
	// #base_label is the label without the "block in "/"<...> in " qualifier; the
	// captured label carries no qualifier here, so it equals #label.
	loc.define("base_label", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@label")
	})
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
