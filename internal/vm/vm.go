// Package vm interprets bytecode.
//
// Phase 1 adds the live object model (plan §5): values dispatch through mutable
// per-class method tables (the project's objc_msgSend), so monkey-patching,
// define_method, method_missing, classes, instances and ivars all work. The
// arithmetic/comparison opcodes remain a fast path; method calls go through
// OpSend → send().
//
// Runtime errors are still fatal in Phase 1 (rescue arrives in Phase 3) and
// travel as panic(RubyError) recovered at the Run boundary.
package vm

import (
	"fmt"
	"io"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// RubyError is a runtime error surfaced to the caller.
type RubyError struct {
	Class   string
	Message string
}

func (e RubyError) Error() string { return e.Class + ": " + e.Message }

// raise never returns; the object.Value result lets callers write
// `return raise(...)` without an unreachable trailing return.
func raise(class, format string, args ...any) object.Value {
	panic(RubyError{Class: class, Message: fmt.Sprintf(format, args...)})
}

// VM holds I/O, the top-level self, the constant table and the base classes.
type VM struct {
	out    io.Writer
	main   object.Value
	consts map[string]object.Value // top-level constants (classes live here)

	cBasicObject, cObject, cModule, cClass        *RClass
	cInteger, cFloat, cString                      *RClass
	cTrueClass, cFalseClass, cNilClass             *RClass
}

// New returns a VM writing program output to out.
func New(out io.Writer) *VM {
	vm := &VM{out: out, main: object.Main{}, consts: map[string]object.Value{}}
	vm.bootstrap()
	return vm
}

// Run executes the top-level ISeq (self = main, default definee = Object).
func (vm *VM) Run(iseq *bytecode.ISeq) (result object.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, r.(RubyError)
		}
	}()
	return vm.exec(iseq, vm.main, nil, vm.cObject), nil
}

// exec runs one ISeq. definee is the class that `def` targets in this frame.
func (vm *VM) exec(iseq *bytecode.ISeq, self object.Value, args []object.Value, definee *RClass) object.Value {
	if len(args) != len(iseq.Params) {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d)", len(args), len(iseq.Params))
	}
	locals := make([]object.Value, iseq.NumLocals)
	for i := range locals {
		locals[i] = object.NilV
	}
	copy(locals, args)

	stack := make([]object.Value, 0, 16)
	push := func(v object.Value) { stack = append(stack, v) }
	pop := func() object.Value {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}

	pc := 0
	for pc < len(iseq.Insns) {
		in := iseq.Insns[pc]
		switch in.Op {
		case bytecode.OpNop:
		case bytecode.OpPushConst:
			push(iseq.Consts[in.A])
		case bytecode.OpPushNil:
			push(object.NilV)
		case bytecode.OpPushTrue:
			push(object.True)
		case bytecode.OpPushFalse:
			push(object.False)
		case bytecode.OpPushSelf:
			push(self)
		case bytecode.OpPop:
			pop()
		case bytecode.OpDup:
			push(stack[len(stack)-1])
		case bytecode.OpGetLocal:
			push(locals[in.A])
		case bytecode.OpSetLocal:
			locals[in.A] = stack[len(stack)-1]
		case bytecode.OpGetIvar:
			push(getIvar(self, iseq.Names[in.A]))
		case bytecode.OpSetIvar:
			setIvar(self, iseq.Names[in.A], stack[len(stack)-1])
		case bytecode.OpGetConst:
			name := iseq.Names[in.A]
			v, ok := vm.consts[name]
			if !ok {
				raise("NameError", "uninitialized constant %s", name)
			}
			push(v)
		case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv,
			bytecode.OpMod, bytecode.OpLt, bytecode.OpGt, bytecode.OpLe,
			bytecode.OpGe, bytecode.OpEq, bytecode.OpNeq:
			b := pop()
			a := pop()
			push(binary(in.Op, a, b))
		case bytecode.OpNeg:
			push(negate(pop()))
		case bytecode.OpNot:
			push(object.Bool(!pop().Truthy()))
		case bytecode.OpJump:
			pc = in.A
			continue
		case bytecode.OpBranchIf:
			if pop().Truthy() {
				pc = in.A
				continue
			}
		case bytecode.OpBranchUnless:
			if !pop().Truthy() {
				pc = in.A
				continue
			}
		case bytecode.OpSend:
			argc := in.B
			callArgs := make([]object.Value, argc)
			copy(callArgs, stack[len(stack)-argc:])
			stack = stack[:len(stack)-argc]
			recv := pop()
			push(vm.send(recv, iseq.Names[in.A], callArgs))
		case bytecode.OpDefineMethod:
			definee.methods[iseq.Names[in.A]] = &Method{name: iseq.Names[in.A], iseq: iseq.Children[in.B], owner: definee}
			push(object.NilV)
		case bytecode.OpDefineClass:
			push(vm.defineClass(iseq.Names[in.A], iseq.Children[in.B]))
		case bytecode.OpReturn:
			return pop()
		default:
			raise("VMError", "unknown opcode %s", in.Op)
		}
		pc++
	}
	return object.NilV
}

// defineClass creates or reopens a class, runs its body with self = the class,
// and returns the body's value.
func (vm *VM) defineClass(name string, body *bytecode.ISeq) object.Value {
	var class *RClass
	if existing, ok := vm.consts[name]; ok {
		class = existing.(*RClass) // reopen
	} else {
		super := vm.cObject
		if body.Super != "" {
			sc, ok := vm.consts[body.Super]
			if !ok {
				raise("NameError", "uninitialized constant %s", body.Super)
			}
			super = sc.(*RClass)
		}
		class = newClass(name, super)
		vm.consts[name] = class
	}
	return vm.exec(body, class, nil, class)
}
