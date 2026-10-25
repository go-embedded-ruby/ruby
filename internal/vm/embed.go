package vm

import (
	"fmt"

	nd "github.com/go-ndarray/ndarray"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the seam the public embedding API (package ruby, embed.go at the
// repository root) reaches the VM through. Nothing here is reachable from Ruby,
// and nothing here runs unless a host builds an Interp: a VM made by New, and
// every program run by ruby.Run or the rbgo CLI, takes none of these paths.

// TopBinding returns a fresh binding of the top level — self is main, the
// definee is Object, and it has no locals yet. Evaluating through it with
// EvalIn keeps the locals each evaluation assigns, which is what makes a series
// of evaluations behave as one program.
func (vm *VM) TopBinding() *Binding {
	return vm.newToplevelBinding()
}

// EvalIn compiles src against b's locals and runs it, stamping file and line on
// the compiled code so backtraces, __FILE__ and __LINE__ report them. It returns
// the value of the last expression, or the error: a RubyError for a Ruby
// exception (a syntax error is a SyntaxError), with its backtrace. A SystemExit
// is returned like any other exception; it does not run the at_exit handlers,
// which RunAtExit does when the host is done.
//
// It leaves the VM clean after an exception, exactly as Run does, so the next
// evaluation starts with an empty frame stack.
func (vm *VM) EvalIn(b *Binding, src, file string, line int) (object.Value, error) {
	if err := parseFault(src); err != nil {
		// The front end failed on the source instead of reporting a syntax
		// error: a defect of the parser, which must not take the host with it
		// (Kernel#eval is protected the same way, by callNative).
		return nil, RubyError{Class: "SyntaxError", Message: fmt.Sprintf("%s:%d: the Ruby parser failed on this source: %v", file, line, err)}
	}
	return vm.Protect(func() object.Value {
		return vm.bindingEval(b, src, evalLocation{file: file, line: line})
	})
}

// Protect runs fn and turns a Ruby exception escaping it into a RubyError with
// its backtrace. Every other panic is re-raised after the frame stacks are
// reset: a break, a throw or a non-local return unwinding to a frame outside fn
// must reach it (a throw with no catch never gets here: Kernel#throw raises
// UncaughtThrowError itself), and a VM invariant or a host fault stays loud, as
// Run keeps it.
func (vm *VM) Protect(fn func() object.Value) (res object.Value, err error) {
	depth := len(vm.frameNames)
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		rerr, ok := r.(RubyError)
		if ok {
			rerr.Frames = vm.uncaughtBacktrace(rerr)
		}
		vm.unwindFramesTo(depth)
		if !ok {
			panic(r)
		}
		res, err = nil, rerr
	}()
	return fn(), nil
}

// unwindFramesTo truncates the frame-tracking stacks to depth: a panic unwinds
// exec frames without running the tail that pops them.
func (vm *VM) unwindFramesTo(depth int) {
	vm.frameNames = vm.frameNames[:min(depth, len(vm.frameNames))]
	vm.frameFiles = vm.frameFiles[:min(depth, len(vm.frameFiles))]
	vm.frameCrefs = vm.frameCrefs[:min(depth, len(vm.frameCrefs))]
	vm.frameMethods = vm.frameMethods[:min(depth, len(vm.frameMethods))]
	if depth == 0 {
		vm.fileStack = vm.fileStack[:0]
	}
}

// RunAtExit runs the at_exit handlers a program registered, last registered
// first, once.
func (vm *VM) RunAtExit() { vm.runAtExit() }

// ExitStatus reports the status a SystemExit carries, and whether rerr is one.
func (vm *VM) ExitStatus(rerr RubyError) (int, bool) {
	if !vm.classIsA(rerr.Class, "SystemExit") {
		return 0, false
	}
	return clampInt(vm.exitStatusOf(rerr.Obj)), true
}

// LocalGet reads a local of the binding.
func (b *Binding) LocalGet(name string) (object.Value, bool) {
	i := b.slotOf(name)
	if i < 0 {
		return nil, false
	}
	env, slot := b.at(i)
	return env.slots[slot], true
}

// LocalSet assigns a local of the binding, creating it when it does not exist.
func (b *Binding) LocalSet(name string, v object.Value) {
	if i := b.slotOf(name); i >= 0 {
		env, slot := b.at(i)
		env.slots[slot] = v
		return
	}
	b.addLocal(name, v)
}

// LocalNames lists the binding's named locals in creation order.
func (b *Binding) LocalNames() []string {
	out := make([]string, 0, len(b.names))
	for _, n := range b.names {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// ClassOf returns the class v is an instance of.
func (vm *VM) ClassOf(v object.Value) *RClass { return vm.classOf(v) }

// ConstIn reads a constant defined directly in scope (nil: the top level).
func (vm *VM) ConstIn(scope *RClass, name string) (object.Value, bool) {
	if scope == nil || scope == vm.cObject {
		v, ok := vm.consts[name]
		return v, ok
	}
	v, ok := scope.consts[name]
	return v, ok
}

// DefineModule returns the module named name inside parent (nil: the top
// level), creating it when it does not exist. It reports false when the
// constant exists and is not a module.
func (vm *VM) DefineModule(parent *RClass, name string) (*RClass, bool) {
	if v, ok := vm.ConstIn(parent, name); ok {
		c, isClass := v.(*RClass)
		return c, isClass && c.isModule
	}
	m := newClass(vm.qualified(parent, name), nil)
	m.isModule = true
	vm.installConst(parent, name, m)
	return m, true
}

// DefineClass returns the class named name inside parent (nil: the top level),
// creating it as a subclass of super (nil: Object) when it does not exist. It
// reports false when the constant exists and is not a class.
func (vm *VM) DefineClass(parent *RClass, name string, super *RClass) (*RClass, bool) {
	if v, ok := vm.ConstIn(parent, name); ok {
		c, isClass := v.(*RClass)
		return c, isClass && !c.isModule
	}
	if super == nil {
		super = vm.cObject
	}
	c := newClass(vm.qualified(parent, name), super)
	vm.installConst(parent, name, c)
	return c, true
}

func (vm *VM) qualified(parent *RClass, name string) string {
	if parent == nil || parent == vm.cObject {
		return name
	}
	return parent.name + "::" + name
}

func (vm *VM) installConst(parent *RClass, name string, c *RClass) {
	if parent == nil || parent == vm.cObject {
		vm.consts[name] = c
		return
	}
	c.lexParent = parent
	parent.consts[name] = c
}

// Name is the class's qualified name ("" for an anonymous one).
func (c *RClass) Name() string { return c.name }

// DefineNative installs a native instance method.
func (c *RClass) DefineNative(name string, fn NativeFn) { c.define(name, fn) }

// DefineSingletonNative installs a native singleton (class or module) method.
func (c *RClass) DefineSingletonNative(name string, fn NativeFn) {
	c.smethods[name] = &Method{name: name, native: fn, owner: c}
	bumpMethodSerial()
}

// NativeNoKW reports the keyword verdict of the call site that reached the
// running native method: true when the call passed no keyword arguments, so a
// trailing Hash is positional. False means "the site may have passed keywords",
// and the last-hash convention every native follows applies.
func (vm *VM) NativeNoKW() bool { return vm.nativeNoKW }

// Send calls the method name on recv. A Ruby exception propagates as a panic;
// run it under Protect.
func (vm *VM) Send(recv object.Value, name string, args []object.Value, blk *Proc) object.Value {
	return vm.send(recv, name, args, blk)
}

// CallBlock calls a block with args. A Ruby exception propagates as a panic, and
// so do break, next-less non-local exits and throw, which must unwind past the
// Go code that called the block to the frame that owns them.
func (vm *VM) CallBlock(p *Proc, args []object.Value) object.Value { return vm.callBlock(p, args) }

// ProcArity is Proc#arity.
func (vm *VM) ProcArity(p *Proc) int {
	n, _ := vm.send(p, "arity", nil, nil).(object.Integer)
	return clampInt(n)
}

// InspectValue is Kernel#inspect as p prints it.
func (vm *VM) InspectValue(v object.Value) string {
	if g, ok := v.(*GoObject); ok {
		return g.Inspect()
	}
	return vm.inspectStr(v)
}

// ToSValue is #to_s as puts prints it.
func (vm *VM) ToSValue(v object.Value) string {
	switch v.(type) {
	case *RObject, *GoObject:
		if s, ok := vm.send(v, "to_s", nil, nil).(*object.String); ok {
			return s.Str()
		}
	}
	return v.ToS()
}

// Raise raises a Ruby exception of the named class.
func Raise(class, msg string) { panic(RubyError{Class: class, Message: msg}) }

// Reraise raises err again, unchanged: the exception object a rescue sees is
// the one that was raised.
func Reraise(err RubyError) { panic(err) }

// WrapNDArray is the Ruby NDArray for a: the same array, not a copy.
func WrapNDArray(a *nd.Array) object.Value { return &NDArray{a: a} }

// UnwrapNDArray is the go-ndarray array behind an NDArray value.
func UnwrapNDArray(v object.Value) (*nd.Array, bool) {
	n, ok := v.(*NDArray)
	if !ok {
		return nil, false
	}
	return n.a, true
}

// GoObject is a Go value carried through Ruby unchanged: an instance of a class
// the host chose, whose methods are the host's native methods. Ruby sees an
// ordinary object; Go gets back the very value it put in.
type GoObject struct {
	vm  *VM
	cls *RClass
	val any
}

// WrapGo makes v an instance of cls.
func (vm *VM) WrapGo(v any, cls *RClass) *GoObject { return &GoObject{vm: vm, cls: cls, val: v} }

// Value is the wrapped Go value.
func (g *GoObject) Value() any { return g.val }

// Inspect calls the class's own #inspect when the host defined one, and
// otherwise reads "#<Class>".
func (g *GoObject) Inspect() string { return g.render("inspect") }

// ToS calls the class's own #to_s when the host defined one, and otherwise
// reads "#<Class>".
func (g *GoObject) ToS() string { return g.render("to_s") }

func (g *GoObject) Truthy() bool { return true }

func (g *GoObject) render(name string) string {
	if m := lookupMethod(g.cls, name); m != nil && !m.undefined && !g.vm.isKernelOwned(m) {
		if s, ok := g.vm.invoke(m, g, nil, nil).(*object.String); ok {
			return s.Str()
		}
	}
	return fmt.Sprintf("#<%s>", g.cls.name)
}

func (vm *VM) isKernelOwned(m *Method) bool {
	return m.owner == vm.cObject || m.owner == vm.cKernel || m.owner == vm.cBasicObject
}

// SetConstIn defines the constant name in a module or class the host defined.
func (vm *VM) SetConstIn(scope *RClass, name string, v object.Value) { scope.consts[name] = v }

// goObjectOp applies a binary operator to a host object: it is a call of the
// operator method its class defines (+, -, <, ==, …), whose result is the
// operator's — so an == that answers a mask Array, as a data frame's does,
// keeps it. != is !(self == other) unless the class defines != itself.
func (vm *VM) goObjectOp(op bytecode.Op, g *GoObject, b object.Value) object.Value {
	name := basicOpName(op)
	if op == bytecode.OpNeq {
		if m := lookupMethod(g.cls, "!="); m == nil || vm.isKernelOwned(m) {
			return object.Bool(!vm.send(g, "==", []object.Value{b}, nil).Truthy())
		}
	}
	return vm.send(g, name, []object.Value{b}, nil)
}
