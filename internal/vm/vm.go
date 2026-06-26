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
	"math/big"
	"path/filepath"
	"strings"
	"sync"

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

// throwSignal unwinds a Kernel#throw to the matching Kernel#catch (matched by tag
// identity). An unmatched throw surfaces as an UncaughtThrowError at Run.
type throwSignal struct {
	tag   object.Value
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
	errOut io.Writer // $stderr/STDERR sink; defaults to out (no separate stream)
	main   object.Value
	consts map[string]object.Value // top-level constants (classes live here)

	cBasicObject, cObject, cModule, cClass *RClass
	cInteger, cFloat, cString, cSymbol     *RClass
	cComplex, cRational                    *RClass
	cNDArray, cImage                       *RClass
	cSet                                   *RClass
	cTime                                  *RClass
	cBigDecimal                            *RClass
	cDate                                  *RClass
	cBag                                   *RClass
	cArray, cHash, cRange                  *RClass
	cProc                                  *RClass
	cMethod                                *RClass
	cEnumerator                            *RClass
	cYielder                               *RClass
	cEncoding                              *RClass
	encodings                              map[string]*encodingObj
	cLazy                                  *RClass
	lastMatch                              object.Value            // $~: last regexp MatchData (or nil)
	globals                                map[string]object.Value // user-assigned $globals
	cTrueClass, cFalseClass, cNilClass     *RClass
	cRegexp, cMatchData                    *RClass
	cException                             *RClass
	curExc                                 object.Value // most recently rescued exception (for bare `raise`)

	loaded        map[string]bool // require/require_relative: features loaded once
	requireDirs   []string        // stack of directories of the files currently being required
	defaultRandom *RandomObj      // process-wide generator for Kernel#rand / #srand
	currentFiber  *Fiber          // the fiber currently running (nil at the root), for Fiber.yield

	// Concurrency: an emulated GVL (one Ruby thread executes VM code at a time).
	// The running goroutine holds gvl; it is released only inside blocking native
	// methods (Thread#join, Mutex#lock, Queue#pop, sleep, Thread.pass), where the
	// thread's execution context is saved and the next runnable thread's restored.
	gvl           sync.Mutex
	currentThread *RThread   // the thread holding the GVL
	mainThread    *RThread   // the root thread
	threads       []*RThread // all live threads, for Thread.list (GVL-guarded)

	// envFree recycles per-call frame environments. exec checks one out at entry
	// and returns it at normal exit unless a closure captured it (see Env.captured
	// / markEnvCaptured). Touched only while the GVL is held, so it needs no lock.
	envFree []*Env

	// stackFree recycles per-frame operand-stack backing arrays (see getStack).
	stackFree [][]object.Value

	// objIDs assigns stable object_id/__id__ values to symbols and reference
	// objects (value types get a deterministic id from their value); nextObjID is
	// the counter for the next reference id. Lazily initialised; GVL-guarded.
	objIDs    map[object.Value]int64
	nextObjID int64
}

// objectID returns the receiver's object_id / __id__. Immediate values get the
// deterministic ids MRI uses (fixnum n -> 2n+1, nil -> 4, true -> 20,
// false -> 0); symbols and reference objects get a stable id memoised in objIDs
// (so the same object always reports the same id, distinct objects differ).
func (vm *VM) objectID(self object.Value) object.Value {
	switch v := self.(type) {
	case object.Integer:
		// Fixnum id is 2n+1 (matches MRI up to its 62-bit fixnum range). Bignums
		// are heap objects in MRI, so they fall through to the memoised path below.
		return object.NormInt(new(big.Int).Add(new(big.Int).Lsh(big.NewInt(int64(v)), 1), big.NewInt(1)))
	case object.Bool:
		if v {
			return object.Integer(20)
		}
		return object.Integer(0)
	case object.Nil:
		return object.Integer(4)
	}
	if vm.objIDs == nil {
		vm.objIDs = map[object.Value]int64{}
	}
	if id, ok := vm.objIDs[self]; ok {
		return object.Integer(id)
	}
	vm.nextObjID += 8 // even ids, never colliding with the odd fixnum ids
	vm.objIDs[self] = vm.nextObjID
	return object.Integer(vm.nextObjID)
}

// envFreeMax caps the env free-list so a deep call burst doesn't pin memory.
const envFreeMax = 1024

// stackFree recycles per-frame operand-stack backing arrays, mirroring envFree.
// Each exec checks one out (getStack) and returns it (putStack) on normal exit;
// an exception unwinding past the return leaves it to the GC. GVL-guarded.
//
// The operand stack escapes to the heap (the push/pop closures capture and
// reassign it), so without pooling every frame allocated a fresh backing array;
// recycling removes that per-call allocation on the hot call-bound path.

// getStack returns a recycled operand stack (len 0), or a fresh one.
func (vm *VM) getStack() []object.Value {
	n := len(vm.stackFree)
	if n == 0 {
		return make([]object.Value, 0, 16)
	}
	s := vm.stackFree[n-1]
	vm.stackFree = vm.stackFree[:n-1]
	return s[:0]
}

// putStack returns an operand stack to the free-list. The slice must be empty
// of live references the caller still needs; exec only recycles on normal
// completion, when the stack holds nothing the frame will read again.
func (vm *VM) putStack(s []object.Value) {
	// getStack only ever hands out backing arrays with cap >= 16, so s always has
	// capacity worth recycling; the only reason to drop it is a full free-list.
	if len(vm.stackFree) >= envFreeMax {
		return
	}
	// Clear so a pooled stack pins nothing for the GC.
	s = s[:cap(s)]
	for i := range s {
		s[i] = nil
	}
	vm.stackFree = append(vm.stackFree, s[:0])
}

// getEnv returns a recycled frame env, or a fresh one if the free-list is empty.
func (vm *VM) getEnv() *Env {
	n := len(vm.envFree)
	if n == 0 {
		return &Env{}
	}
	e := vm.envFree[n-1]
	vm.envFree = vm.envFree[:n-1]
	return e
}

// putEnv returns an env to the free-list, but only if no closure captured it (so
// recycling can never alias a live env) and the list has room. References are
// cleared so a pooled env pins nothing for the GC.
func (vm *VM) putEnv(e *Env) {
	if e.captured || len(vm.envFree) >= envFreeMax {
		return
	}
	e.parent = nil
	e.kwargs = nil
	e.slots = nil
	e.inline = [4]object.Value{}
	vm.envFree = append(vm.envFree, e)
}

// New returns a VM writing program output to out.
func New(out io.Writer) *VM {
	vm := &VM{out: out, errOut: out, main: object.NewMain(), consts: map[string]object.Value{}, loaded: map[string]bool{}, globals: map[string]object.Value{}}
	// The main thread holds the GVL for the VM's lifetime, releasing it only at
	// blocking points so spawned Ruby threads can run (see thread.go).
	vm.gvl.Lock()
	vm.mainThread = &RThread{status: "run", done: make(chan struct{}), locals: map[object.Value]object.Value{}, parked: true}
	vm.currentThread = vm.mainThread
	vm.threads = []*RThread{vm.mainThread}
	vm.bootstrap()
	// $LOAD_PATH (and its alias $:) is a real, mutable Array that require /
	// require_relative search, so gems doing `$LOAD_PATH.unshift "lib"` work.
	loadPath := &object.Array{}
	vm.globals["$LOAD_PATH"] = loadPath
	vm.globals["$:"] = loadPath
	vm.installPrelude()
	vm.registerEnumerator() // after the prelude so it can mix in Enumerable
	vm.registerLazy()       // after Enumerator (Enumerator::Lazy is built on it)
	return vm
}

// SetScriptPath records the path of the top-level program so require_relative
// (and a path-relative require) can resolve against its directory. Hosts call it
// before Run; with no script set, resolution falls back to the process CWD.
func (vm *VM) SetScriptPath(path string) {
	if path != "" {
		vm.requireDirs = []string{filepath.Dir(path)}
	}
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
			if sig, ok := r.(throwSignal); ok {
				r = RubyError{Class: "UncaughtThrowError", Message: "uncaught throw " + sig.tag.Inspect()}
			}
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
	env := vm.getEnv()
	env.parent, env.kwargs, env.captured = parentEnv, kwargs, false
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

	stack := vm.getStack()
	push := func(v object.Value) { stack = append(stack, v) }
	pop := func() object.Value {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}

	// caches is the per-send-site inline-cache slice, fetched lazily on the first
	// OpSend (iseqCaches type-asserts the bytecode.ISeq's `any` field, so a
	// send-free body — e.g. the hot `times { t += i }` block — never pays it).
	var caches []inlineCache

	pc := 0
	var handlers []handlerFrame
	result := object.Value(object.NilV)
	finished := false

	// runChunk runs the instruction loop until the frame finishes (OpReturn /
	// falling off the end) or a panic unwinds out. It is the shared loop body for
	// both the handler-bearing path (wrapped in a recover that resumes at a
	// rescue) and the common no-rescue path (run directly, so a method without a
	// begin/rescue — fib, dispatch, attr accessors — pays no per-frame defer).
	runChunk := func() {
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
			case bytecode.OpSetScopedConst:
				// Foo::BAR = value. Stack is [recv, value]; pop the value, then the
				// receiver, set recv::name and push the value back (assignment is an
				// expression yielding its right-hand side).
				val := pop()
				recv := pop()
				cls, ok := recv.(*RClass)
				if !ok {
					raise("TypeError", "%s is not a class/module", recv.Inspect())
				}
				cls.consts[iseq.Names[in.A]] = val
				push(val)
			case bytecode.OpSetConst:
				// Assignment is an expression: set the constant, keep its value.
				vm.consts[iseq.Names[in.A]] = stack[len(stack)-1]
			case bytecode.OpGetGVar:
				push(vm.gvar(iseq.Names[in.A]))
			case bytecode.OpSetGVar:
				// Assignment is an expression: set the global, keep its value.
				vm.globals[iseq.Names[in.A]] = stack[len(stack)-1]
			case bytecode.OpGetCVar:
				name := iseq.Names[in.A]
				vm.checkCVarScope(definee)
				if c := cvarOwner(definee, name); c != nil {
					push(c.cvars[name])
				} else {
					raise("NameError", "uninitialized class variable %s in %s", name, definee.name)
				}
			case bytecode.OpGetCVarQuiet:
				// The read side of @@name ||= …: an undefined class variable is
				// nil here rather than a NameError (Ruby's ||=/&&= semantics).
				name := iseq.Names[in.A]
				vm.checkCVarScope(definee)
				if c := cvarOwner(definee, name); c != nil {
					push(c.cvars[name])
				} else {
					push(object.NilV)
				}
			case bytecode.OpSetCVar:
				// Set where the variable already lives in the hierarchy, else
				// define it on the current class. Assignment keeps its value.
				name := iseq.Names[in.A]
				vm.checkCVarScope(definee)
				if c := cvarOwner(definee, name); c != nil {
					c.cvars[name] = stack[len(stack)-1]
				} else {
					definee.cvars[name] = stack[len(stack)-1]
				}
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
				name := iseq.Names[in.A]
				if caches == nil {
					caches = iseqCaches(iseq)
				}
				if in.C == 0 {
					// No literal block: take the monomorphic fast path that resolves
					// and invokes the method directly, skipping the dispatchSend→send
					// layers. The per-call-site inline cache (caches[pc]) turns the
					// method-table walk into a pointer compare on a cache hit — the
					// dominant case for call-bound code (dispatch / fib / proc). A
					// class receiver (singleton dispatch) or an unresolved name
					// (operator fallback / method_missing) falls back to send.
					base := len(stack) - argc
					recv := stack[base-1]
					if _, isClass := recv.(*RClass); !isClass {
						if m := vm.lookupCached(&caches[pc], recv, name); m != nil {
							// Pass the args in place from the operand stack: invoke
							// (→ exec / a native method) consumes them before this frame
							// touches the region again, so no per-call args copy is
							// needed — this removes the single dominant allocation on the
							// call-bound path. invokeInPlace copies into a fresh slice
							// only when the callee might retain the args (native bodies).
							res := vm.invokeInPlace(m, recv, stack[base:], nil)
							stack = stack[:base-1]
							stack = append(stack, res)
							pc++
							continue
						}
					}
					callArgs := make([]object.Value, argc)
					copy(callArgs, stack[base:])
					stack = stack[:base-1]
					push(vm.dispatchSend(recv, name, callArgs, nil))
				} else {
					callArgs := make([]object.Value, argc)
					copy(callArgs, stack[len(stack)-argc:])
					stack = stack[:len(stack)-argc]
					recv := pop()
					// A literal block: capture this frame's env, self, block.
					markEnvCaptured(env)
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
				bumpMethodSerial() // a (re)definition can change what a cached send resolves to
				// Hook: definee.method_added(:name) for instance-method defs, if
				// the class/module defines the hook (singleton method).
				if hook := lookupSMethod(definee, "method_added"); hook != nil {
					vm.invoke(hook, definee, []object.Value{object.Symbol(name)}, nil)
				}
				push(object.NilV)
			case bytecode.OpDefineSMethod:
				definee.smethods[iseq.Names[in.A]] = &Method{name: iseq.Names[in.A], iseq: iseq.Children[in.B], owner: definee}
				push(object.NilV)
			case bytecode.OpDefineSingletonMethod:
				// def recv.foo: a class receiver gains a class method; any other
				// object gains a method on its singleton class.
				name := iseq.Names[in.A]
				recv := pop()
				switch t := recv.(type) {
				case *RClass:
					t.smethods[name] = &Method{name: name, iseq: iseq.Children[in.B], owner: t}
				case *RObject:
					sc := vm.singletonClass(t)
					sc.methods[name] = &Method{name: name, iseq: iseq.Children[in.B], owner: sc}
				default:
					raise("TypeError", "can't define singleton method %q for %s", name, vm.classOf(recv).name)
				}
				push(object.NilV)
			case bytecode.OpOpenSingletonClass:
				// class << target: run the child body with target's singleton (meta)
				// class as the definee, so its method/constant defs attach there.
				target := pop()
				sc, ok := vm.singletonDefinee(target)
				if !ok {
					raise("TypeError", "can't define singleton")
				}
				push(vm.exec(iseq.Children[in.A], sc, nil, sc, "", nil, nil, nil))
			case bytecode.OpAlias:
				vm.aliasMethod(definee, iseq.Names[in.A], iseq.Names[in.B])
				push(object.NilV)
			case bytecode.OpUndef:
				vm.undefMethod(definee, iseq.Names[in.A])
				push(object.NilV)
			case bytecode.OpDefineClass:
				push(vm.defineClass(iseq.Names[in.A], iseq.Children[in.B]))
			case bytecode.OpDefineModule:
				push(vm.defineModule(iseq.Names[in.A], iseq.Children[in.B]))
			case bytecode.OpDefineClassScoped:
				// C flags: bit 0 = parent on stack, bit 1 = super-expr on stack.
				// They were pushed parent-then-super, so pop super first.
				var superExpr object.Value
				if in.C&2 != 0 {
					superExpr = pop()
				}
				var parent *RClass
				if in.C&1 != 0 {
					parent = vm.asModuleParent(pop())
				}
				push(vm.defineClassIn(parent, iseq.Names[in.A], iseq.Children[in.B], superExpr))
			case bytecode.OpDefineModuleScoped:
				parent := vm.asModuleParent(pop())
				push(vm.defineModuleIn(parent, iseq.Names[in.A], iseq.Children[in.B]))
			case bytecode.OpInvokeSuper:
				superBlk := block
				if in.C > 0 { // an explicit `super(...) { … }` literal block overrides the frame block
					markEnvCaptured(env)
					superBlk = &Proc{iseq: iseq.Children[in.C-1], env: env, self: self, block: block}
				}
				var superArgs []object.Value
				if in.B == 1 { // bare super forwards the frame's own arguments
					superArgs = args
				} else {
					superArgs = make([]object.Value, in.A)
					copy(superArgs, stack[len(stack)-in.A:])
					stack = stack[:len(stack)-in.A]
				}
				push(vm.invokeSuper(self, definee, methodName, superArgs, superBlk))
			case bytecode.OpInvokeSuperArray:
				superBlk := block
				switch {
				case in.C == 1: // a &block-pass value (on top of the args array) overrides the frame block
					superBlk = vm.toBlock(pop())
				case in.C > 1: // a literal `super(*a) { … }` block, from child C-2
					markEnvCaptured(env)
					superBlk = &Proc{iseq: iseq.Children[in.C-2], env: env, self: self, block: block}
				}
				argsArr := pop().(*object.Array)
				push(vm.invokeSuper(self, definee, methodName, argsArr.Elems, superBlk))
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
			case bytecode.OpBinding:
				markEnvCaptured(env)
				push(&Binding{env: env, self: self, definee: definee, names: append([]string(nil), iseq.Locals...)})
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
			case bytecode.OpXStr:
				push(object.NewString(vm.runShellCommand(iseq.Names[in.A])))
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
					markEnvCaptured(env)
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
	}

	if iseqHasHandler(iseq) {
		// This frame can rescue: run under a recover that, on a Ruby exception with
		// a live handler, unwinds the operand stack and resumes at the rescue pc;
		// other panics (or no handler) re-propagate. The loop re-enters after a
		// caught exception (handler set a new pc) until the frame finishes.
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
				runChunk()
			}()
		}
	} else {
		// No begin/rescue in this ISeq: run the loop directly. A panic propagates
		// to an enclosing frame's handler (or the Run boundary) with no per-frame
		// defer — the common case (fib, dispatch, accessors) skips that overhead.
		runChunk()
	}
	// Recycle this frame's env and operand stack on normal completion (putEnv is
	// a no-op if a closure captured the env). An exception unwinding past here
	// skips recycling and leaves both to the GC — correct, just not pooled.
	vm.putEnv(env)
	vm.putStack(stack)
	return result
}

// defineClass creates or reopens a class, runs its body with self = the class,
// and returns the body's value. It is the bare top-level form; scoped paths and
// expression superclasses go through defineClassIn.
func (vm *VM) defineClass(name string, body *bytecode.ISeq) object.Value {
	return vm.defineClassIn(nil, name, body, nil)
}

// constTable returns the constant table a const name is defined into for the
// given lexical parent: the parent class/module's own table, or the global
// top-level table when parent is nil.
func (vm *VM) constTable(parent *RClass) map[string]object.Value {
	if parent == nil {
		return vm.consts
	}
	return parent.consts
}

// scopedName qualifies a constant name with its lexical parent, so a nested
// class reports its full `Parent::Name` (matching MRI).
func scopedName(parent *RClass, name string) string {
	if parent == nil || parent.name == "" {
		return name
	}
	return parent.name + "::" + name
}

// defineClassIn creates or reopens a class named `name` in `parent`'s constant
// table (the global table when parent is nil). superExpr, when non-nil, is the
// evaluated superclass value (a `::`-path or other expression); otherwise
// body.Super (a bare name) is consulted. It runs the class body with self = the
// class and returns the body's value.
func (vm *VM) defineClassIn(parent *RClass, name string, body *bytecode.ISeq, superExpr object.Value) object.Value {
	table := vm.constTable(parent)
	var class *RClass
	if existing, ok := table[name]; ok {
		var isClass bool
		class, isClass = existing.(*RClass)
		if !isClass || class.isModule {
			raise("TypeError", "%s is not a class", name)
		}
	} else {
		super := vm.cObject
		switch {
		case superExpr != nil:
			sc, ok := superExpr.(*RClass)
			if !ok || sc.isModule {
				raise("TypeError", "superclass must be a Class (%s given)", vm.classOf(superExpr).name)
			}
			super = sc
		case body.Super != "":
			sc, ok := vm.consts[body.Super]
			if !ok {
				raise("NameError", "uninitialized constant %s", body.Super)
			}
			super = sc.(*RClass)
		}
		class = newClass(scopedName(parent, name), super)
		table[name] = class
		// Hook: superclass.inherited(subclass), fired when the class is created
		// (before its body runs) if the superclass defines the hook.
		if hook := lookupSMethod(super, "inherited"); hook != nil {
			vm.invoke(hook, super, []object.Value{class}, nil)
		}
	}
	return vm.exec(body, class, nil, class, "", nil, nil, nil)
}

// defineModule creates or reopens a module and runs its body with self = the
// module.
func (vm *VM) defineModule(name string, body *bytecode.ISeq) object.Value {
	return vm.defineModuleIn(nil, name, body)
}

// defineModuleIn creates or reopens a module named `name` in `parent`'s constant
// table (the global table when parent is nil), runs its body with self = the
// module, and returns the body's value.
func (vm *VM) defineModuleIn(parent *RClass, name string, body *bytecode.ISeq) object.Value {
	table := vm.constTable(parent)
	var mod *RClass
	if existing, ok := table[name]; ok {
		var isClass bool
		mod, isClass = existing.(*RClass)
		if !isClass || !mod.isModule {
			raise("TypeError", "%s is not a module", name)
		}
	} else {
		mod = newClass(scopedName(parent, name), nil)
		mod.isModule = true
		table[name] = mod
	}
	return vm.exec(body, mod, nil, mod, "", nil, nil, nil)
}

// asModuleParent coerces a popped value to the class/module that a scoped
// definition/assignment nests into, raising a TypeError otherwise.
func (vm *VM) asModuleParent(v object.Value) *RClass {
	cls, ok := v.(*RClass)
	if !ok {
		raise("TypeError", "%s is not a class/module", v.Inspect())
	}
	return cls
}

// invokeSuper dispatches `super`: it finds methodName starting above the current
// method's owner (its superclass chain, including their mixins) and invokes it,
// forwarding the current block.
func (vm *VM) invokeSuper(self object.Value, definee *RClass, methodName string, args []object.Value, blk *Proc) object.Value {
	if methodName == "" {
		raise("RuntimeError", "super called outside of method")
	}
	// super resolves to the next definition of methodName after the current
	// method's owner (definee) in the receiver's ancestor chain — so it walks
	// prepended and included modules, not just the superclass.
	anc := vm.ancestors(vm.classOf(self))
	start := -1
	for i, k := range anc {
		if k == definee {
			start = i
			break
		}
	}
	if start >= 0 {
		for _, k := range anc[start+1:] {
			if m, ok := k.methods[methodName]; ok {
				return vm.invoke(m, self, args, blk)
			}
		}
	} else if m := lookupSMethod(definee.super, methodName); m != nil {
		// definee is outside the receiver's ancestry: this is a class-method
		// super (def self.foo), so walk the singleton-method chain.
		return vm.invoke(m, self, args, blk)
	}
	raise("NoMethodError", "super: no superclass method '%s'", methodName)
	return object.NilV
}
