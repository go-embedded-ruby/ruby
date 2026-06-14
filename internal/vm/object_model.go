package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// NativeFn is a method implemented in Go.
type NativeFn func(vm *VM, self object.Value, args []object.Value) object.Value

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
	case object.Bool:
		if x {
			return vm.cTrueClass
		}
		return vm.cFalseClass
	case object.Nil:
		return vm.cNilClass
	case object.Main:
		return vm.cObject
	}
	return nil // unreachable for the closed set of value types
}

// send is the dispatch core (our objc_msgSend): find the method on the
// receiver's class chain, else route to method_missing (Object's default
// raises NoMethodError).
func (vm *VM) send(recv object.Value, name string, args []object.Value) object.Value {
	c := vm.classOf(recv)
	if m := lookupMethod(c, name); m != nil {
		return vm.invoke(m, recv, args)
	}
	mm := lookupMethod(c, "method_missing")
	mmArgs := append([]object.Value{object.String(name)}, args...)
	return vm.invoke(mm, recv, mmArgs)
}

func (vm *VM) invoke(m *Method, self object.Value, args []object.Value) object.Value {
	if m.native != nil {
		return m.native(vm, self, args)
	}
	return vm.exec(m.iseq, self, args, m.owner, m.name)
}

func getIvar(self object.Value, name string) object.Value {
	if ro, ok := self.(*RObject); ok {
		if v, ok := ro.ivars[name]; ok {
			return v
		}
	}
	return object.NilV
}

func setIvar(self object.Value, name string, v object.Value) {
	if ro, ok := self.(*RObject); ok {
		ro.ivars[name] = v
	}
}
