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
	Obj     object.Value // the Ruby exception object, when raised from Ruby (else nil)
}

func (e RubyError) Error() string { return e.Class + ": " + e.Message }

// raise never returns; the object.Value result lets callers write
// `return raise(...)` without an unreachable trailing return.
func raise(class, format string, args ...any) object.Value {
	panic(RubyError{Class: class, Message: fmt.Sprintf(format, args...)})
}

// breakSignal unwinds a block `break` to the method the block was passed to.
// owner identifies the executing block so the matching call site catches it
// (and a break through a Ruby-level iterator like Enumerable#map lands on the
// outer call, not the inner each).
type breakSignal struct {
	owner *Proc
	value object.Value
}

// sendCatchBreak performs a send carrying a literal block, turning a `break`
// raised by that block into the call's result.
func (vm *VM) sendCatchBreak(recv object.Value, name string, args []object.Value, blk *Proc) (result object.Value) {
	defer func() {
		if r := recover(); r != nil {
			if sig, ok := r.(breakSignal); ok && sig.owner == blk {
				result = sig.value
				return
			}
			panic(r)
		}
	}()
	return vm.send(recv, name, args, blk)
}

// handlerFrame is an active begin/rescue handler: where to resume and the
// operand-stack depth to restore.
type handlerFrame struct {
	pc int
	sp int
}

// exceptionObject returns the Ruby exception object for a RubyError, building
// one from the class name + message when the error did not originate from a
// Ruby `raise` (internal raises carry no object).
func (vm *VM) exceptionObject(e RubyError) object.Value {
	if e.Obj != nil {
		return e.Obj
	}
	cls, ok := vm.consts[e.Class].(*RClass)
	if !ok {
		cls = vm.consts["StandardError"].(*RClass)
	}
	return &RObject{class: cls, ivars: map[string]object.Value{"@message": object.String(e.Message)}}
}

// VM holds I/O, the top-level self, the constant table and the base classes.
type VM struct {
	out    io.Writer
	main   object.Value
	consts map[string]object.Value // top-level constants (classes live here)

	cBasicObject, cObject, cModule, cClass *RClass
	cInteger, cFloat, cString, cSymbol     *RClass
	cArray, cHash, cRange                  *RClass
	cTrueClass, cFalseClass, cNilClass     *RClass
	cException                             *RClass
	curExc                                 object.Value // most recently rescued exception (for bare `raise`)
}

// New returns a VM writing program output to out.
func New(out io.Writer) *VM {
	vm := &VM{out: out, main: object.NewMain(), consts: map[string]object.Value{}}
	vm.bootstrap()
	vm.loadPrelude(preludeSource)
	return vm
}

// Run executes the top-level ISeq (self = main, default definee = Object).
func (vm *VM) Run(iseq *bytecode.ISeq) (result object.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, r.(RubyError)
		}
	}()
	return vm.exec(iseq, vm.main, nil, vm.cObject, "", nil, nil, nil), nil
}

// exec runs one ISeq. definee is the class that `def` targets in this frame;
// methodName is the name of the running method ("" at top level / class bodies),
// used to resolve `super`.
func (vm *VM) exec(iseq *bytecode.ISeq, self object.Value, args []object.Value, definee *RClass, methodName string, parentEnv *Env, block, selfBlock *Proc) object.Value {
	if len(args) != len(iseq.Params) {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d)", len(args), len(iseq.Params))
	}
	env := &Env{slots: make([]object.Value, iseq.NumLocals), parent: parentEnv}
	for i := range env.slots {
		env.slots[i] = object.NilV
	}
	copy(env.slots, args)

	stack := make([]object.Value, 0, 16)
	push := func(v object.Value) { stack = append(stack, v) }
	pop := func() object.Value {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}

	pc := 0
	var handlers []handlerFrame
	result := object.Value(object.NilV)
	finished := false
	for !finished {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				rerr, ok := r.(RubyError)
				if !ok || len(handlers) == 0 {
					panic(r) // not a Ruby exception, or no handler in this frame
				}
				h := handlers[len(handlers)-1]
				handlers = handlers[:len(handlers)-1]
				stack = stack[:h.sp]
				exc := vm.exceptionObject(rerr)
				vm.curExc = exc
				push(exc)
				pc = h.pc
			}()
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
				case bytecode.OpNewArray:
					n := in.A
					elems := make([]object.Value, n)
					copy(elems, stack[len(stack)-n:])
					stack = stack[:len(stack)-n]
					push(&object.Array{Elems: elems})
				case bytecode.OpNewHash:
					n := in.A * 2
					region := stack[len(stack)-n:]
					h := object.NewHash()
					for i := 0; i < n; i += 2 {
						h.Set(region[i], region[i+1])
					}
					stack = stack[:len(stack)-n]
					push(h)
				case bytecode.OpNewRange:
					hi := pop()
					lo := pop()
					push(&object.Range{Lo: lo, Hi: hi, Exclusive: in.A == 1})
				case bytecode.OpPop:
					pop()
				case bytecode.OpDup:
					push(stack[len(stack)-1])
				case bytecode.OpGetLocal:
					push(env.ancestor(in.B).slots[in.A])
				case bytecode.OpSetLocal:
					env.ancestor(in.B).slots[in.A] = stack[len(stack)-1]
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
					push(vm.binaryOp(in.Op, a, b))
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
					var blk *Proc
					if in.C > 0 { // a literal block: capture this frame's env, self, block
						blk = &Proc{iseq: iseq.Children[in.C-1], env: env, self: self, block: block}
					}
					if blk != nil {
						push(vm.sendCatchBreak(recv, iseq.Names[in.A], callArgs, blk))
					} else {
						push(vm.send(recv, iseq.Names[in.A], callArgs, nil))
					}
				case bytecode.OpDefineMethod:
					definee.methods[iseq.Names[in.A]] = &Method{name: iseq.Names[in.A], iseq: iseq.Children[in.B], owner: definee}
					push(object.NilV)
				case bytecode.OpDefineClass:
					push(vm.defineClass(iseq.Names[in.A], iseq.Children[in.B]))
				case bytecode.OpDefineModule:
					push(vm.defineModule(iseq.Names[in.A], iseq.Children[in.B]))
				case bytecode.OpInvokeSuper:
					var superArgs []object.Value
					if in.B == 1 { // bare super forwards the frame's own arguments
						superArgs = args
					} else {
						superArgs = make([]object.Value, in.A)
						copy(superArgs, stack[len(stack)-in.A:])
						stack = stack[:len(stack)-in.A]
					}
					push(vm.invokeSuper(self, definee, methodName, superArgs, block))
				case bytecode.OpInvokeBlock:
					if block == nil {
						raise("LocalJumpError", "no block given (yield)")
					}
					yargs := make([]object.Value, in.A)
					copy(yargs, stack[len(stack)-in.A:])
					stack = stack[:len(stack)-in.A]
					push(vm.callBlock(block, yargs))
				case bytecode.OpBlockGiven:
					push(object.Bool(block != nil))
				case bytecode.OpReturn:
					result = pop()
					finished = true
					return
				case bytecode.OpBreak:
					panic(breakSignal{owner: selfBlock, value: pop()})
				case bytecode.OpPushHandler:
					handlers = append(handlers, handlerFrame{pc: in.A, sp: len(stack)})
				case bytecode.OpPopHandler:
					handlers = handlers[:len(handlers)-1]
				case bytecode.OpReThrow:
					panic(vm.excError(pop()))
				default:
					raise("VMError", "unknown opcode %s", in.Op)
				}
				pc++
			}
			finished = true
		}()
	}
	return result
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
	return vm.exec(body, class, nil, class, "", nil, nil, nil)
}

// defineModule creates or reopens a module and runs its body with self = the
// module.
func (vm *VM) defineModule(name string, body *bytecode.ISeq) object.Value {
	var mod *RClass
	if existing, ok := vm.consts[name]; ok {
		mod = existing.(*RClass) // reopen
	} else {
		mod = newClass(name, nil)
		mod.isModule = true
		vm.consts[name] = mod
	}
	return vm.exec(body, mod, nil, mod, "", nil, nil, nil)
}

// invokeSuper dispatches `super`: it finds methodName starting above the current
// method's owner (its superclass chain, including their mixins) and invokes it,
// forwarding the current block.
func (vm *VM) invokeSuper(self object.Value, definee *RClass, methodName string, args []object.Value, blk *Proc) object.Value {
	if methodName == "" {
		raise("RuntimeError", "super called outside of method")
	}
	m := lookupMethod(definee.super, methodName)
	if m == nil {
		raise("NoMethodError", "super: no superclass method '%s'", methodName)
	}
	return vm.invoke(m, self, args, blk)
}
