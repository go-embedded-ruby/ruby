package vm

import (
	"fmt"
	"runtime"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// NativeFn is a method implemented in Go. blk is the block passed at the call
// site (nil if none).
type NativeFn func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value

// Env is one lexical frame of local-variable slots. parent links to the
// enclosing frame so a block (closure) can read and write the locals of the
// method (or outer block) it was defined in.
type Env struct {
	slots  []object.Value
	parent *Env
	kwargs *object.Hash    // keyword arguments bound for this frame (nil if none)
	inline [4]object.Value // backs slots for the common small-frame case (no 2nd alloc)
	// captured is set when a Proc or Binding closes over this env (or a lexical
	// descendant whose chain reaches it), meaning the env outlives its frame and
	// must NOT be returned to the env free-list when exec finishes. A frame that
	// creates no escaping closure (e.g. plain recursion like fib) stays false and
	// is recycled. See markEnvCaptured.
	captured bool
}

// markEnvCaptured marks e and its lexical ancestors as captured, so none of them
// is recycled. Block envs chain to their defining env, so a closure created deep
// inside nested blocks pins the whole enclosing chain; method envs have no parent
// and stop the walk.
func markEnvCaptured(e *Env) {
	for ; e != nil && !e.captured; e = e.parent {
		e.captured = true
	}
}

// ancestor returns the env depth levels up the parent chain.
func (e *Env) ancestor(depth int) *Env {
	for ; depth > 0; depth-- {
		e = e.parent
	}
	return e
}

// Proc is a captured block: its compiled body, the env it closes over, and the
// self it runs against. It is also a first-class Ruby value (implements
// object.Value), so a `&block` param can reify it and Proc#call can invoke it.
type Proc struct {
	iseq *bytecode.ISeq
	env  *Env
	self object.Value
	// block is the method-level block in scope where this block literal was
	// written. Blocks are transparent to `yield`: a `yield` inside a block
	// reaches the enclosing method's block, which is what lets Enumerable
	// methods iterate via `each { ... yield ... }`.
	block *Proc
	// native, when non-nil, makes this a synthesized Proc (e.g. Symbol#to_proc)
	// whose body is Go rather than an ISeq; nativeArity backs Proc#arity for it.
	native      func(vm *VM, args []object.Value) object.Value
	nativeArity int
	isLambda    bool // true for lambda { } / ->(){}: backs Proc#lambda?
}

func (p *Proc) ToS() string     { return "#<Proc>" }
func (p *Proc) Inspect() string { return "#<Proc>" }
func (p *Proc) Truthy() bool    { return true }

// Method is a Ruby method: either native (Go) or an ISeq (compiled Ruby).
type Method struct {
	name     string
	native   NativeFn
	iseq     *bytecode.ISeq
	proc     *Proc          // for define_method: a block-backed method body
	compiled CompiledMethod // AOT-lowered native body (rbgo build); preferred when set
	owner    *RClass
}

// RClass is a class (the live, mutable method table that makes monkey-patching,
// define_method and method_missing fall out for free).
type RClass struct {
	name     string
	super    *RClass
	methods  map[string]*Method
	smethods map[string]*Method // singleton (class) methods: def self.foo
	consts   map[string]object.Value
	cvars    map[string]object.Value // class variables (@@name), shared down the hierarchy
	includes []*RClass               // modules mixed in via include (most recent last)
	prepends []*RClass               // modules mixed in via prepend (most recent last), ahead of own methods
	isModule bool
}

func newClass(name string, super *RClass) *RClass {
	return &RClass{name: name, super: super, methods: map[string]*Method{}, smethods: map[string]*Method{}, consts: map[string]object.Value{}, cvars: map[string]object.Value{}}
}

func (c *RClass) ToS() string     { return c.name }
func (c *RClass) Inspect() string { return c.name }
func (c *RClass) Truthy() bool    { return true }

// define installs a native method on the class.
func (c *RClass) define(name string, fn NativeFn) {
	c.methods[name] = &Method{name: name, native: fn, owner: c}
}

// RObject is an ordinary instance: a class plus instance variables, and an
// optional singleton class holding per-object methods (def obj.x,
// define_singleton_method, extend).
type RObject struct {
	class     *RClass
	ivars     map[string]object.Value
	singleton *RClass // lazily created; its super is `class`
}

// singletonClass returns o's singleton class, creating it on first use. Its
// super is the object's class, so a normal method lookup through it finds the
// per-object methods first, then the class chain.
func (vm *VM) singletonClass(o *RObject) *RClass {
	if o.singleton == nil {
		o.singleton = newClass("", o.class)
	}
	return o.singleton
}

func (o *RObject) ToS() string     { return "#<" + o.class.name + ">" }
func (o *RObject) Inspect() string { return o.ToS() }
func (o *RObject) Truthy() bool    { return true }

// lookupMethod walks the ancestor chain: at each class, its own methods then
// its included modules (most-recently-included first), then up to its super.
func lookupMethod(c *RClass, name string) *Method {
	for ; c != nil; c = c.super {
		if m := lookupOwnOrIncluded(c, name); m != nil {
			return m
		}
	}
	return nil
}

// scopedConst resolves Recv::name. It searches the class/module's own constant
// table up the superclass chain; failing that it falls back to the global
// constant table (constants are currently stored flat there), so both a native
// module constant like Math::PI and a user Foo::BAR resolve.
func (vm *VM) scopedConst(cls *RClass, name string) object.Value {
	for c := cls; c != nil; c = c.super {
		if v, ok := c.consts[name]; ok {
			return v
		}
	}
	if v, ok := vm.consts[name]; ok {
		return v
	}
	raise("NameError", "uninitialized constant %s::%s", cls.name, name)
	return object.NilV
}

// checkCVarScope rejects class-variable access whose lexical class is Object —
// i.e. at the top level or in a method defined directly on Object. MRI raises a
// RuntimeError there rather than treating Object as the owning class.
func (vm *VM) checkCVarScope(definee *RClass) {
	if definee == vm.cObject {
		raise("RuntimeError", "class variable access from toplevel")
	}
}

// cvarOwner returns the nearest class in c's superclass chain that already
// defines the class variable name, or nil if none does — class variables are
// shared down the hierarchy.
func cvarOwner(c *RClass, name string) *RClass {
	for ; c != nil; c = c.super {
		if _, ok := c.cvars[name]; ok {
			return c
		}
	}
	return nil
}

// lookupSMethod finds a singleton (class) method, walking the superclass chain
// so a subclass inherits its ancestors' class methods.
func lookupSMethod(c *RClass, name string) *Method {
	for ; c != nil; c = c.super {
		if m, ok := c.smethods[name]; ok {
			return m
		}
	}
	return nil
}

func lookupOwnOrIncluded(c *RClass, name string) *Method {
	// Prepended modules take priority over the class's own methods; own methods
	// take priority over included modules (Ruby's ancestor order).
	for i := len(c.prepends) - 1; i >= 0; i-- {
		if m := lookupOwnOrIncluded(c.prepends[i], name); m != nil {
			return m
		}
	}
	if m, ok := c.methods[name]; ok {
		return m
	}
	for i := len(c.includes) - 1; i >= 0; i-- {
		if m := lookupOwnOrIncluded(c.includes[i], name); m != nil {
			return m
		}
	}
	return nil
}

// ancestors returns c's method-resolution order: for each class up the
// superclass chain, its prepended modules (most-recent first) before the class
// itself, then its included modules — every module expanded for its own
// prepends/includes, and each ancestor appearing once (first occurrence wins).
// This is the basis of both Module#ancestors and super.
func (vm *VM) ancestors(c *RClass) []*RClass {
	var out []*RClass
	seen := map[*RClass]bool{}
	push := func(k *RClass) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	var addTree func(m *RClass)
	addTree = func(m *RClass) {
		for i := len(m.prepends) - 1; i >= 0; i-- {
			addTree(m.prepends[i])
		}
		push(m)
		for i := len(m.includes) - 1; i >= 0; i-- {
			addTree(m.includes[i])
		}
	}
	for k := c; k != nil; k = k.super {
		for i := len(k.prepends) - 1; i >= 0; i-- {
			addTree(k.prepends[i])
		}
		push(k)
		for i := len(k.includes) - 1; i >= 0; i-- {
			addTree(k.includes[i])
		}
	}
	return out
}

// classOf returns the dynamic class of any value — the basis of dispatch.
func (vm *VM) classOf(v object.Value) *RClass {
	switch x := v.(type) {
	case *RObject:
		return x.class
	case *RClass:
		return vm.cClass
	case object.Integer:
		return vm.cInteger
	case *object.Bignum:
		return vm.cInteger
	case object.Float:
		return vm.cFloat
	case *object.Complex:
		return vm.cComplex
	case *object.Rational:
		return vm.cRational
	case *NDArray:
		return vm.cNDArray
	case *Image:
		return vm.cImage
	case *Set:
		return vm.cSet
	case *Time:
		return vm.cTime
	case *BigDecimal:
		return vm.cBigDecimal
	case *Date:
		return vm.cDate
	case *Bag:
		return vm.cBag
	case *object.String:
		return vm.cString
	case object.Symbol:
		return vm.cSymbol
	case *object.Array:
		return vm.cArray
	case *object.Hash:
		return vm.cHash
	case *object.Range:
		return vm.cRange
	case *Proc:
		return vm.cProc
	case *BoundMethod:
		return vm.cMethod
	case *Enumerator:
		return vm.cEnumerator
	case *LazyEnum:
		return vm.cLazy
	case *RandomObj:
		return vm.consts["Random"].(*RClass)
	case *Fiber:
		return vm.consts["Fiber"].(*RClass)
	case *RThread:
		return vm.consts["Thread"].(*RClass)
	case *RMutex:
		return vm.consts["Mutex"].(*RClass)
	case *RQueue:
		return vm.consts["Queue"].(*RClass)
	case *IOObj:
		return x.cls
	case *Binding:
		return vm.consts["Binding"].(*RClass)
	case *Regexp:
		return vm.cRegexp
	case *MatchData:
		return vm.cMatchData
	case object.Bool:
		if x {
			return vm.cTrueClass
		}
		return vm.cFalseClass
	case object.Nil:
		return vm.cNilClass
	case *object.Main:
		return vm.cObject
	}
	return nil // unreachable for the closed set of value types
}

// send is the dispatch core (our objc_msgSend): find the method on the
// receiver's class chain, else route to method_missing (Object's default
// raises NoMethodError). blk is the block passed to the call (nil if none).
func (vm *VM) send(recv object.Value, name string, args []object.Value, blk *Proc) object.Value {
	// A class receiver consults its singleton-method chain (def self.foo, and
	// inherited class methods) before the generic Class instance methods.
	if cls, ok := recv.(*RClass); ok {
		if m := lookupSMethod(cls, name); m != nil {
			return vm.invoke(m, recv, args, blk)
		}
	}
	c := vm.classOf(recv)
	// An object with a singleton class dispatches through it first (per-object
	// methods + extended modules), then its class chain (the singleton's super).
	if o, ok := recv.(*RObject); ok && o.singleton != nil {
		c = o.singleton
	}
	if m := lookupMethod(c, name); m != nil {
		return vm.invoke(m, recv, args, blk)
	}
	// The arithmetic/comparison operators are a compiler fast path rather than
	// real methods, so send(:+, x), reduce(:+) and respond_to-style dispatch
	// route them through the same operator logic here.
	if len(args) == 1 {
		if op, ok := operatorOpcode(name); ok {
			return vm.binaryOp(op, recv, args[0])
		}
	}
	mm := lookupMethod(c, "method_missing")
	mmArgs := append([]object.Value{object.Symbol(name)}, args...)
	return vm.invoke(mm, recv, mmArgs, blk)
}

// callNative runs a Go-implemented method, converting a genuine Go runtime
// fault (e.g. a binding indexing past a missing argument) into a rescuable Ruby
// ArgumentError so arbitrary input from an embedding host — the wasm playground
// REPL, say — can never crash the process. Ruby-level raises (RubyError) and
// control-flow signals (break/return) pass through untouched, and a broken VM
// invariant still panics elsewhere so real bugs stay loud.
func (vm *VM) callNative(m *Method, self object.Value, args []object.Value, blk *Proc) (res object.Value) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(runtime.Error); ok {
				panic(RubyError{Class: "ArgumentError", Message: fmt.Sprintf("%s: %v", m.name, r)})
			}
			panic(r)
		}
	}()
	return m.native(vm, self, args, blk)
}

func (vm *VM) invoke(m *Method, self object.Value, args []object.Value, blk *Proc) object.Value {
	if m.compiled != nil {
		return m.compiled(vm, self, args, blk)
	}
	if m.native != nil {
		return vm.callNative(m, self, args, blk)
	}
	if m.proc != nil {
		// A define_method body runs the block with self rebound to the receiver,
		// keeping its closure environment.
		return vm.callBlockSelf(m.proc, self, args)
	}
	return vm.exec(m.iseq, self, args, m.owner, m.name, nil, blk, nil)
}

// callBlock invokes a captured block with args. Block arity is lenient: extra
// arguments are dropped and missing ones default to nil (Ruby semantics).
// arityVal backs Proc#arity: a synthesized proc reports nativeArity; an ISeq
// block reports its parameter count.
func (p *Proc) arityVal() int {
	if p.native != nil {
		return p.nativeArity
	}
	return len(p.iseq.Params)
}

// toBlock coerces a &block-pass value into a *Proc: nil for nil, the Proc
// itself, else whatever its to_proc returns (which must be a Proc).
func (vm *VM) toBlock(v object.Value) *Proc {
	switch p := v.(type) {
	case object.Nil:
		return nil
	case *Proc:
		return p
	default:
		conv := vm.send(v, "to_proc", nil, nil)
		cp, ok := conv.(*Proc)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Proc", vm.classOf(v).name)
		}
		return cp
	}
}

// dispatchSend sends, routing a literal/passed block through sendCatchBreak so a
// `break` unwinds to the call site.
func (vm *VM) dispatchSend(recv object.Value, name string, args []object.Value, blk *Proc) object.Value {
	if blk != nil {
		return vm.sendCatchBreak(recv, name, args, blk)
	}
	return vm.send(recv, name, args, nil)
}

func (vm *VM) callBlock(p *Proc, args []object.Value) object.Value {
	return vm.callBlockSelf(p, p.self, args)
}

// callBlockSelf is callBlock with an explicit self, used by define_method to
// rebind the block's self to the method's receiver.
func (vm *VM) callBlockSelf(p *Proc, self object.Value, args []object.Value) object.Value {
	if p.native != nil {
		return p.native(vm, args)
	}
	return vm.exec(p.iseq, self, vm.bindBlockArgs(p, args), vm.cObject, "", p.env, p.block, p)
}

// classEval runs a block as class_eval/module_eval would: self and the method
// definition target are both cls, so a `def` inside the block adds an instance
// method to cls.
func (vm *VM) classEval(cls *RClass, p *Proc, args []object.Value) object.Value {
	// class_eval/module_eval always receive a literal (ISeq) block, never a
	// synthesized native Proc, so no native fast path is needed here.
	return vm.exec(p.iseq, cls, vm.bindBlockArgs(p, args), cls, "", p.env, p.block, p)
}

// bindBlockArgs maps call args onto a block's parameters, with the auto-splat a
// multi-parameter block applies to a single Array argument.
func (vm *VM) bindBlockArgs(p *Proc, args []object.Value) []object.Value {
	np := len(p.iseq.Params)
	if np > 1 && len(args) == 1 {
		if arr, ok := args[0].(*object.Array); ok {
			args = arr.Elems
		}
	}
	bargs := make([]object.Value, np)
	for i := range bargs {
		if i < len(args) {
			bargs[i] = args[i]
		} else {
			bargs[i] = object.NilV
		}
	}
	return bargs
}

func getIvar(self object.Value, name string) object.Value {
	if t := ivarTable(self); t != nil {
		if v, ok := t[name]; ok {
			return v
		}
	}
	return object.NilV
}

func setIvar(self object.Value, name string, v object.Value) {
	if t := ivarTable(self); t != nil {
		t[name] = v
	}
}

// ivarTable returns the instance-variable map backing self, or nil for values
// (Integer, String, …) that cannot hold ivars in this phase.
func ivarTable(self object.Value) map[string]object.Value {
	switch o := self.(type) {
	case *RObject:
		return o.ivars
	case *object.Main:
		return o.IvarTable()
	}
	return nil
}
