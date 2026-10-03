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
// native method is keyed by the record holding its definition — itself, or the
// original an `alias` copied it from (Method.defOf) — which keeps distinct
// closures generated from one literal, such as every attr_reader getter,
// correctly unequal while still making an alias equal to its original.
func methodDefKey(m *Method) uintptr {
	switch {
	case m.iseq != nil:
		return reflect.ValueOf(m.iseq).Pointer()
	case m.proc != nil:
		return reflect.ValueOf(m.proc).Pointer()
	default:
		return reflect.ValueOf(methodDefRecord(m)).Pointer()
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
	defineBuiltinSecondName(vm.cMethod, "eql?", "==")
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
	m, ok := cls.methods[oldName]
	if !ok {
		// A missing old name used to mean "do nothing", silently -- and that is
		// how Float#inspect came to be owned by Kernel: cFloat had no own to_s
		// when numeric_edges.go asked for the alias, so nothing was installed and
		// nothing said so. A registration path that can fail quietly is not
		// checkable by any test that only looks at the names which ARE there,
		// so this panics instead. It runs at VM construction, so a wrong site
		// cannot reach a user program.
		panic("aliasBuiltin: " + cls.name + "#" + oldName + " is not defined yet (asked for alias " + newName + ")")
	}
	cls.methods[newName] = m
}

// defineBuiltinSecondName gives an existing built-in a SECOND name that is its
// own original_name, which is what MRI does for most of the pairs that look like
// aliases: it calls rb_define_method a second time over the same C function.
// That yields a distinct method entry -- so #name and #original_name are both
// the new name -- while rb_method_definition_eq still compares the shared cfunc,
// so #== answers true. Measured, for the 60 sites in this shape:
//
//	Hash.instance_method(:length).original_name   ruby 4.0.5  :length
//	Proc.instance_method(:yield).original_name    ruby 4.0.5  :yield
//	Object.instance_method(:fail).original_name   ruby 4.0.5  :fail
//
// aliasBuiltin remains for the 20 sites that really are rb_define_alias, where
// MRI reports the OLD name (Numeric#imag of #imaginary, say). The two helpers
// differ in exactly one observable, and the choice per site is measured, never
// guessed -- see the table in issue #754.
func defineBuiltinSecondName(cls *RClass, newName, oldName string) {
	secondNameIn(cls.methods, "defineBuiltinSecondName", cls.name+"#", newName, oldName)
}

// defineBuiltinSecondNameS is defineBuiltinSecondName for a SINGLETON method
// (Dir.getwd over Dir.pwd, Thread.fork over Thread.start), whose records live in
// a separate table. MRI's answer is per class AND per table -- Complex#imag is a
// second definition where Numeric#imag is a real alias, measured -- so neither
// table can be assumed to follow the other.
func defineBuiltinSecondNameS(cls *RClass, newName, oldName string) {
	secondNameIn(cls.smethods, "defineBuiltinSecondNameS", cls.name+".", newName, oldName)
}

// aliasBuiltinS is aliasBuiltin for a singleton method: a genuine
// rb_define_alias, where MRI reports the OLD name as #original_name.
func aliasBuiltinS(cls *RClass, newName, oldName string) {
	m, ok := cls.smethods[oldName]
	if !ok {
		panic("aliasBuiltinS: " + cls.name + "." + oldName + " is not defined yet (asked for alias " + newName + ")")
	}
	cls.smethods[newName] = m
}

func secondNameIn(tbl map[string]*Method, who, qualified, newName, oldName string) {
	m, ok := tbl[oldName]
	if !ok {
		panic(who + ": " + qualified + oldName + " is not defined yet (asked for " + newName + ")")
	}
	clone := *m
	clone.name = newName
	clone.origName = newName
	clone.defOf = methodDefRecord(m)
	tbl[newName] = &clone
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
