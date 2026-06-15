package vm

import (
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
	kwargs *object.Hash // keyword arguments bound for this frame (nil if none)
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
	name   string
	native NativeFn
	iseq   *bytecode.ISeq
	owner  *RClass
}

// RClass is a class (the live, mutable method table that makes monkey-patching,
// define_method and method_missing fall out for free).
type RClass struct {
	name     string
	super    *RClass
	methods  map[string]*Method
	consts   map[string]object.Value
	includes []*RClass // modules mixed in via include (most recent last)
	isModule bool
}

func newClass(name string, super *RClass) *RClass {
	return &RClass{name: name, super: super, methods: map[string]*Method{}, consts: map[string]object.Value{}}
}

func (c *RClass) ToS() string     { return c.name }
func (c *RClass) Inspect() string { return c.name }
func (c *RClass) Truthy() bool    { return true }

// define installs a native method on the class.
func (c *RClass) define(name string, fn NativeFn) {
	c.methods[name] = &Method{name: name, native: fn, owner: c}
}

// RObject is an ordinary instance: a class plus instance variables.
type RObject struct {
	class *RClass
	ivars map[string]object.Value
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

func lookupOwnOrIncluded(c *RClass, name string) *Method {
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

// classOf returns the dynamic class of any value — the basis of dispatch.
func (vm *VM) classOf(v object.Value) *RClass {
	switch x := v.(type) {
	case *RObject:
		return x.class
	case *RClass:
		return vm.cClass
	case object.Integer:
		return vm.cInteger
	case object.Float:
		return vm.cFloat
	case object.String:
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
	c := vm.classOf(recv)
	if m := lookupMethod(c, name); m != nil {
		return vm.invoke(m, recv, args, blk)
	}
	mm := lookupMethod(c, "method_missing")
	mmArgs := append([]object.Value{object.Symbol(name)}, args...)
	return vm.invoke(mm, recv, mmArgs, blk)
}

func (vm *VM) invoke(m *Method, self object.Value, args []object.Value, blk *Proc) object.Value {
	if m.native != nil {
		return m.native(vm, self, args, blk)
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
	if p.native != nil {
		return p.native(vm, args)
	}
	np := len(p.iseq.Params)
	// Auto-splat: a block with multiple parameters called with a single Array
	// destructures it (Ruby semantics), so `{ |a, b| }` binds element-wise over
	// `[[1, 2]].each` and over Hash#each's [k, v] pairs.
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
	return vm.exec(p.iseq, p.self, bargs, vm.cObject, "", p.env, p.block, p)
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
