// Package vm interprets bytecode.
//
// Phase 0 is a straightforward stack machine. Each Ruby call recurses into a Go
// function (so the Go stack is the call stack and fib(20) just works); explicit
// frame objects, catch tables, and Fiber arrive in later phases (plan §6–§8).
//
// Runtime errors are fatal in Phase 0 (no rescue yet), so they travel as a
// panic(RubyError) recovered at the Run boundary. Once exceptions land
// (Phase 3) normal control flow switches to the status-return design of plan §8.
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

func raise(class, format string, args ...any) {
	panic(RubyError{Class: class, Message: fmt.Sprintf(format, args...)})
}

// NativeFn is a builtin implemented in Go.
type NativeFn func(vm *VM, self object.Value, args []object.Value) object.Value

// VM holds the global method tables and I/O.
type VM struct {
	methods  map[string]*bytecode.ISeq // user-defined methods (Phase 1: real method tables)
	builtins map[string]NativeFn
	out      io.Writer
	main     object.Value
}

// New returns a VM writing program output to out.
func New(out io.Writer) *VM {
	vm := &VM{
		methods:  map[string]*bytecode.ISeq{},
		builtins: map[string]NativeFn{},
		out:      out,
		main:     object.Main{},
	}
	vm.registerBuiltins()
	return vm
}

// Run executes the top-level ISeq and returns its value.
func (vm *VM) Run(iseq *bytecode.ISeq) (result object.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok {
				result, err = nil, re
				return
			}
			panic(r)
		}
	}()
	return vm.exec(iseq, vm.main, nil), nil
}

func (vm *VM) exec(iseq *bytecode.ISeq, self object.Value, args []object.Value) object.Value {
	if len(args) != len(iseq.Params) {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d)", len(args), len(iseq.Params))
	}
	locals := make([]object.Value, iseq.NumLocals)
	for i := range locals {
		locals[i] = object.NilV
	}
	copy(locals, args) // params occupy the first slots

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
			locals[in.A] = stack[len(stack)-1] // assignment is an expression: leave value
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
		case bytecode.OpCall:
			name := iseq.Names[in.A]
			argc := in.B
			callArgs := make([]object.Value, argc)
			copy(callArgs, stack[len(stack)-argc:])
			stack = stack[:len(stack)-argc]
			push(vm.call(self, name, callArgs))
		case bytecode.OpDefineMethod:
			vm.methods[iseq.Names[in.A]] = iseq.Children[in.B]
			push(object.NilV) // def evaluates to a value (Phase 1: the method name symbol)
		case bytecode.OpReturn:
			return pop()
		default:
			raise("VMError", "unknown opcode %s", in.Op)
		}
		pc++
	}
	return object.NilV
}

// call dispatches a self/funcall: builtins first, then user methods.
// Phase 1 replaces this with real method-table lookup on the receiver's class.
func (vm *VM) call(self object.Value, name string, args []object.Value) object.Value {
	if fn, ok := vm.builtins[name]; ok {
		return fn(vm, self, args)
	}
	if iseq, ok := vm.methods[name]; ok {
		return vm.exec(iseq, self, args)
	}
	raise("NoMethodError", "undefined method '%s' for %s", name, self.Inspect())
	return nil
}
