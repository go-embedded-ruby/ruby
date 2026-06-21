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
	"strings"

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
	return &RObject{class: cls, ivars: map[string]object.Value{"@message": object.NewString(e.Message)}}
}

// VM holds I/O, the top-level self, the constant table and the base classes.
type VM struct {
	out    io.Writer
	main   object.Value
	consts map[string]object.Value // top-level constants (classes live here)

	cBasicObject, cObject, cModule, cClass *RClass
	cInteger, cFloat, cString, cSymbol     *RClass
	cComplex, cRational                    *RClass
	cNDArray, cImage                       *RClass
	cSet                                   *RClass
	cTime                                  *RClass
	cBigDecimal                            *RClass
	cArray, cHash, cRange                  *RClass
	cProc                                  *RClass
	lastMatch                              object.Value // $~: last regexp MatchData (or nil)
	cTrueClass, cFalseClass, cNilClass     *RClass
	cRegexp, cMatchData                    *RClass
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

// SetConst installs v as a top-level constant, visible to a subsequently-run
// program as a bare constant reference. Embedding hosts use it to seed a run —
// the wasm playground binds INPUT to the raw bytes of an image before
// evaluating Ruby that processes it.
func (vm *VM) SetConst(name string, v object.Value) { vm.consts[name] = v }

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
// bindKeywords peels the trailing keyword hash off args (Ruby's last-hash
// convention), validates it against the method's keyword params (raising on
// unknown/missing keywords), and returns it (never nil). It shortens *args by
// the consumed hash so positional arity is checked on the remaining args.
func (vm *VM) bindKeywords(iseq *bytecode.ISeq, args *[]object.Value) *object.Hash {
	kwargs := object.NewHash()
	if a := *args; len(a) > 0 {
		if h, ok := a[len(a)-1].(*object.Hash); ok {
			kwargs = h
			*args = a[:len(a)-1]
		}
	}
	valid := make(map[object.Symbol]bool, len(iseq.KwNames))
	for _, kn := range iseq.KwNames {
		valid[object.Symbol(kn)] = true
	}
	// With a **rest param, surplus keywords are captured rather than rejected.
	if iseq.KwRestSlot < 0 {
		var unknown []string
		for _, k := range kwargs.Keys {
			if sym, ok := k.(object.Symbol); ok && valid[sym] {
				continue
			}
			unknown = append(unknown, k.Inspect())
		}
		if len(unknown) > 0 {
			raise("ArgumentError", "unknown keyword%s: %s", plural(len(unknown)), strings.Join(unknown, ", "))
		}
	}
	var missing []string
	for i, kn := range iseq.KwNames {
		if iseq.KwRequired[i] {
			if _, ok := kwargs.Get(object.Symbol(kn)); !ok {
				missing = append(missing, ":"+kn)
			}
		}
	}
	if len(missing) > 0 {
		raise("ArgumentError", "missing keyword%s: %s", plural(len(missing)), strings.Join(missing, ", "))
	}
	return kwargs
}

// plural returns "s" when n > 1, for "keyword"/"keywords" in error messages.
func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}

func (vm *VM) exec(iseq *bytecode.ISeq, self object.Value, args []object.Value, definee *RClass, methodName string, parentEnv *Env, block, selfBlock *Proc) object.Value {
	var kwargs *object.Hash
	if len(iseq.KwNames) > 0 || iseq.KwRestSlot >= 0 {
		kwargs = vm.bindKeywords(iseq, &args)
	}
	if len(args) < iseq.NumRequired || (iseq.SplatIndex < 0 && len(args) > len(iseq.Params)) {
		var expected string
		switch {
		case iseq.SplatIndex >= 0:
			expected = fmt.Sprintf("%d+", iseq.NumRequired)
		case iseq.NumRequired == len(iseq.Params):
			expected = fmt.Sprintf("%d", iseq.NumRequired)
		default:
			expected = fmt.Sprintf("%d..%d", iseq.NumRequired, len(iseq.Params))
		}
		raise("ArgumentError", "wrong number of arguments (given %d, expected %s)", len(args), expected)
	}
	env := &Env{parent: parentEnv, kwargs: kwargs}
	if iseq.NumLocals <= len(env.inline) {
		env.slots = env.inline[:iseq.NumLocals]
	} else {
		env.slots = make([]object.Value, iseq.NumLocals)
	}
	for i := range env.slots {
		env.slots[i] = object.NilV
	}
	if iseq.SplatIndex >= 0 {
		si := iseq.SplatIndex
		nbind := len(args)
		if nbind > si {
			nbind = si
		}
		copy(env.slots[:nbind], args[:nbind])
		rest := []object.Value{}
		if len(args) > si {
			rest = append(rest, args[si:]...)
		}
		env.slots[si] = &object.Array{Elems: rest}
	} else {
		copy(env.slots, args)
	}
	// Supplied keyword args bind into the slots right after the positionals; the
	// prologue fills defaults for any absent optional ones.
	if kwargs != nil {
		base := len(iseq.Params)
		named := make(map[object.Symbol]bool, len(iseq.KwNames))
		for i, kn := range iseq.KwNames {
			named[object.Symbol(kn)] = true
			if v, ok := kwargs.Get(object.Symbol(kn)); ok {
				env.slots[base+i] = v
			}
		}
		// **rest captures every keyword not bound to a named param.
		if iseq.KwRestSlot >= 0 {
			rest := object.NewHash()
			for _, k := range kwargs.Keys {
				if sym, ok := k.(object.Symbol); ok && named[sym] {
					continue
				}
				v, _ := kwargs.Get(k)
				rest.Set(k, v)
			}
			env.slots[iseq.KwRestSlot] = rest
		}
	}
	// &block reifies the method's block as a Proc (nil → no block given).
	if iseq.BlockSlot >= 0 {
		if block != nil {
			env.slots[iseq.BlockSlot] = block
		} else {
			env.slots[iseq.BlockSlot] = object.NilV
		}
	}

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
					// A string literal evaluates to a fresh mutable object each time
					// (Ruby semantics), so clone string constants on push; every other
					// constant is immutable and can be shared.
					if s, ok := iseq.Consts[in.A].(*object.String); ok {
						push(s.Dup())
					} else {
						push(iseq.Consts[in.A])
					}
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
				case bytecode.OpHashSetPair:
					v := pop()
					k := pop()
					// the accumulator hash is now on top of the stack; mutate in place.
					stack[len(stack)-1].(*object.Hash).Set(k, v)
				case bytecode.OpHashMerge:
					val := pop()
					other, ok := val.(*object.Hash)
					if !ok {
						raise("TypeError", "no implicit conversion of %s into Hash", vm.classOf(val).name)
					}
					acc := stack[len(stack)-1].(*object.Hash)
					for _, k := range other.Keys {
						v, _ := other.Get(k)
						acc.Set(k, v)
					}
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
				case bytecode.OpGetScopedConst:
					name := iseq.Names[in.A]
					recv := pop()
					cls, ok := recv.(*RClass)
					if !ok {
						raise("TypeError", "%s is not a class/module", recv.Inspect())
					}
					push(vm.scopedConst(cls, name))
				case bytecode.OpSetConst:
					// Assignment is an expression: set the constant, keep its value.
					vm.consts[iseq.Names[in.A]] = stack[len(stack)-1]
				case bytecode.OpGetGVar:
					push(vm.gvar(iseq.Names[in.A]))
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
				case bytecode.OpTruthy:
					push(object.Bool(pop().Truthy()))
				case bytecode.OpRaiseNoMatch:
					subj := pop()
					raise("NoMatchingPatternError", "%s", subj.Inspect())
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
				case bytecode.OpBranchNil:
					if _, isNil := pop().(object.Nil); isNil {
						pc = in.A
						continue
					}
				case bytecode.OpSend:
					argc := in.B
					callArgs := make([]object.Value, argc)
					copy(callArgs, stack[len(stack)-argc:])
					stack = stack[:len(stack)-argc]
					recv := pop()
					name := iseq.Names[in.A]
					if in.C == 0 {
						// No literal block: take the monomorphic fast path that resolves
						// and invokes the method directly, skipping the dispatchSend→send
						// layers. A class receiver (singleton dispatch) or an unresolved
						// name (operator fallback / method_missing) falls back to send.
						if _, isClass := recv.(*RClass); !isClass {
							if m := lookupMethod(vm.classOf(recv), name); m != nil {
								push(vm.invoke(m, recv, callArgs, nil))
								pc++
								continue
							}
						}
						push(vm.dispatchSend(recv, name, callArgs, nil))
					} else {
						// A literal block: capture this frame's env, self, block.
						blk := &Proc{iseq: iseq.Children[in.C-1], env: env, self: self, block: block}
						push(vm.dispatchSend(recv, name, callArgs, blk))
					}
				case bytecode.OpSendBlockArg:
					blockVal := pop()
					argc := in.B
					callArgs := make([]object.Value, argc)
					copy(callArgs, stack[len(stack)-argc:])
					stack = stack[:len(stack)-argc]
					recv := pop()
					push(vm.dispatchSend(recv, iseq.Names[in.A], callArgs, vm.toBlock(blockVal)))
				case bytecode.OpDefineMethod:
					name := iseq.Names[in.A]
					m := &Method{name: name, iseq: iseq.Children[in.B], owner: definee}
					// Attach the AOT-compiled body only on the first definition of
					// this name; a redefinition gets a fresh, interpreted Method
					// (deopt), since the compiled body matched the original source.
					if _, redef := definee.methods[name]; !redef {
						m.compiled = compiledFor(definee.name, name)
					}
					definee.methods[name] = m
					push(object.NilV)
				case bytecode.OpDefineSMethod:
					definee.smethods[iseq.Names[in.A]] = &Method{name: iseq.Names[in.A], iseq: iseq.Children[in.B], owner: definee}
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
				case bytecode.OpArgGiven:
					push(object.Bool(in.A < len(args)))
				case bytecode.OpKwGiven:
					_, ok := env.kwargs.Get(object.Symbol(iseq.KwNames[in.A]))
					push(object.Bool(ok))
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
				case bytecode.OpRegexp:
					push(vm.compileRegexp(iseq.Names[in.A], iseq.Names[in.B]))
				case bytecode.OpSplatToArray:
					v := pop()
					if arr, ok := v.(*object.Array); ok {
						push(arr)
					} else {
						push(&object.Array{Elems: []object.Value{v}})
					}
				case bytecode.OpExpandArray:
					elems := pop().(*object.Array).Elems
					n := len(elems)
					pre, post, hasSplat := in.A, in.B, in.C == 1
					vals := make([]object.Value, 0, pre+post+1)
					if hasSplat && n >= pre+post {
						// Enough elements: the splat takes the middle, post the tail.
						for i := 0; i < pre; i++ {
							vals = append(vals, elems[i])
						}
						mid := make([]object.Value, n-pre-post)
						copy(mid, elems[pre:n-post])
						vals = append(vals, &object.Array{Elems: mid})
						for i := 0; i < post; i++ {
							vals = append(vals, elems[n-post+i])
						}
					} else {
						// Too short (or no splat): fill targets left-to-right, the
						// splat is empty, and missing targets get nil.
						idx := 0
						nextVal := func() object.Value {
							if idx < n {
								v := elems[idx]
								idx++
								return v
							}
							idx++
							return object.NilV
						}
						for i := 0; i < pre; i++ {
							vals = append(vals, nextVal())
						}
						if hasSplat {
							vals = append(vals, &object.Array{})
						}
						for i := 0; i < post; i++ {
							vals = append(vals, nextVal())
						}
					}
					// Push in reverse so the first target's value is on top.
					for i := len(vals) - 1; i >= 0; i-- {
						push(vals[i])
					}
				case bytecode.OpConcatArray:
					b2 := pop().(*object.Array)
					a2 := pop().(*object.Array)
					elems := make([]object.Value, 0, len(a2.Elems)+len(b2.Elems))
					elems = append(elems, a2.Elems...)
					elems = append(elems, b2.Elems...)
					push(&object.Array{Elems: elems})
				case bytecode.OpSendArray:
					argsArr := pop().(*object.Array)
					recv := pop()
					var blk *Proc
					if in.C > 0 {
						blk = &Proc{iseq: iseq.Children[in.C-1], env: env, self: self, block: block}
					}
					push(vm.dispatchSend(recv, iseq.Names[in.A], argsArr.Elems, blk))
				case bytecode.OpSendArrayBlockArg:
					blockVal := pop()
					argsArr := pop().(*object.Array)
					recv := pop()
					push(vm.dispatchSend(recv, iseq.Names[in.A], argsArr.Elems, vm.toBlock(blockVal)))
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
