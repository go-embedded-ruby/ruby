// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"reflect"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// methodDefKey returns a comparable identity for a method's underlying
// definition, so that two Method/UnboundMethod objects sharing a body compare
// equal even when their Method records are distinct copies. An alias (`alias`)
// and a define_method transplanting another method's body both copy the record
// while keeping the same iseq / proc, so keying on those pointers makes them
// compare equal — matching MRI's "compare the definition, not the record". A
// native method is keyed by its own record pointer (two natives are equal only
// when they literally share the record, e.g. an intentional alias installed via
// aliasBuiltin), which keeps distinct closures generated from one literal — such
// as every attr_reader getter — correctly unequal.
func methodDefKey(m *Method) uintptr {
	switch {
	case m.iseq != nil:
		return reflect.ValueOf(m.iseq).Pointer()
	case m.proc != nil:
		return reflect.ValueOf(m.proc).Pointer()
	default:
		return reflect.ValueOf(m).Pointer()
	}
}

// methodSameDef reports whether two Method records share an underlying
// definition. A method_missing-backed Method (Object#method for a name answered
// only via respond_to_missing?) has no shared record, so two of them are equal
// exactly when they carry the same name; otherwise the definition-key rule
// applies.
func methodSameDef(a, b *Method) bool {
	if a.viaMissing || b.viaMissing {
		return a.viaMissing && b.viaMissing && a.name == b.name
	}
	return methodDefKey(a) == methodDefKey(b)
}

// methodDefHash gives a Method a hash that agrees with methodSameDef: a
// method_missing-backed Method hashes on its name (so two equal ones agree),
// every other on its definition-key pointer.
func methodDefHash(m *Method) int64 {
	if m.viaMissing {
		return fnvHash("mm:" + m.name)
	}
	return int64(methodDefKey(m))
}

// registerMethodReflect adds the reflection operators that compare, hash,
// compose and curry Method objects (and the composition operators on Proc,
// which share the same semantics). Behaviour matches MRI 3.4.
func (vm *VM) registerMethodReflect() {
	// Method#== / #eql?: same receiver and same underlying definition.
	methodEq := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*BoundMethod)
		b, ok := args[0].(*BoundMethod)
		if !ok {
			return object.False
		}
		return object.Bool(a.recv == b.recv && methodSameDef(a.m, b.m))
	}
	vm.cMethod.define("==", methodEq)
	// Method#eql? is an alias of Method#== (they share one record, so
	// Method.instance_method(:eql?) == Method.instance_method(:==), as in MRI).
	aliasBuiltin(vm.cMethod, "eql?", "==")
	vm.cMethod.define("hash", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*BoundMethod)
		return object.IntValue(vm.hashValue(b.recv) ^ methodDefHash(b.m))
	})

	// Method#>> and Method#<< compose the method with another callable.
	vm.cMethod.define(">>", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.composeCallable(self, args[0], true)
	})
	vm.cMethod.define("<<", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.composeCallable(self, args[0], false)
	})
	// Proc shares the exact same composition semantics.
	vm.cProc.define(">>", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.composeCallable(self, args[0], true)
	})
	vm.cProc.define("<<", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.composeCallable(self, args[0], false)
	})

	// Method#curry: curry the method as a lambda, optionally to a fixed arity.
	vm.cMethod.define("curry", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*BoundMethod)
		required, total, hasSplat := methodParamInfo(b.m)
		need := required
		if len(args) > 0 {
			need = int(intArg(args[0]))
			if need < required || (!hasSplat && need > total) {
				raise("ArgumentError", "wrong number of arguments (given %d, expected %d)", need, required)
			}
		}
		p := &Proc{isLambda: true, nativeArity: -1, native: func(vm *VM, callArgs []object.Value) object.Value {
			return vm.invoke(b.m, b.recv, callArgs, nil)
		}}
		return vm.curried(p, need, nil)
	})
}

// composeCallable builds the Proc composition of two callables. With forward
// true (>>) the result is `other.(self.(*args))`; otherwise (<<) it is
// `self.(other.(*args))`. The argument must be callable (respond to #call) —
// #to_proc coercion is deliberately not attempted, matching MRI. The result's
// lambda-ness follows the callable invoked first.
func (vm *VM) composeCallable(self, other object.Value, forward bool) object.Value {
	if !vm.respondsToDynamic(other, "call") {
		raise("TypeError", "callable object is expected")
	}
	lam := isLambdaCallable(self)
	if !forward {
		lam = isLambdaCallable(other)
	}
	return &Proc{isLambda: lam, nativeArity: -1, native: func(vm *VM, args []object.Value) object.Value {
		if forward {
			inner := vm.send(self, "call", args, nil)
			return vm.send(other, "call", []object.Value{inner}, nil)
		}
		inner := vm.send(other, "call", args, nil)
		return vm.send(self, "call", []object.Value{inner}, nil)
	}}
}

// aliasBuiltin installs newName on cls sharing the exact same Method record as
// oldName, so the two names are the same definition (== / eql? / hash all treat
// them as one method). Unlike Module#alias — which copies the record — this
// keeps a single shared *Method, which is how MRI models genuine built-in
// aliases such as Method#eql?/#== and String#size/#length.
func aliasBuiltin(cls *RClass, newName, oldName string) {
	if m, ok := cls.methods[oldName]; ok {
		cls.methods[newName] = m
	}
}

// isLambdaCallable reports whether a callable enforces lambda argument
// semantics: a lambda Proc or any Method does, an ordinary Proc (or other
// callable object) does not.
func isLambdaCallable(v object.Value) bool {
	switch c := v.(type) {
	case *Proc:
		return c.isLambda
	case *BoundMethod:
		return true
	default:
		return false
	}
}

// methodParamInfo reports a method's required-parameter count, total declared
// parameter count and whether it has a rest (splat) parameter.
func methodParamInfo(m *Method) (required, total int, hasSplat bool) {
	switch {
	case m.iseq != nil:
		return m.iseq.NumRequired, len(m.iseq.Params), m.iseq.SplatIndex >= 0
	case m.proc != nil && m.proc.iseq != nil:
		return m.proc.iseq.NumRequired, len(m.proc.iseq.Params), m.proc.iseq.SplatIndex >= 0
	default:
		return 0, 0, true // native/unknown: accept any arity
	}
}
