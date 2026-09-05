package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// BoundMethod is a Method object: a callable bound to a receiver, produced by
// Object#method(:name).
type BoundMethod struct {
	recv object.Value
	name string
	m    *Method
	// vm is the interpreter the method belongs to, kept so ToS can render the
	// receiver's class and the method's parameters (MRI's #<Method: …> form).
	vm *VM
	methodValueState
}

// newBoundMethod builds a Method bound to recv, resolved under name to m.
func (vm *VM) newBoundMethod(recv object.Value, name string, m *Method) *BoundMethod {
	return &BoundMethod{recv: recv, name: name, m: m, vm: vm}
}

// methodMissingMethod builds the synthetic Method returned by Object#method for
// a name the receiver only answers via respond_to_missing?. Its owner is the
// receiver's class (so Method#owner reports it) and its body forwards to
// method_missing(name, *args, &blk), resolved dynamically at call time.
func (vm *VM) methodMissingMethod(recv object.Value, name string) *Method {
	return &Method{
		name:       name,
		owner:      vm.classOf(recv),
		viaMissing: true,
		native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			fwd := append([]object.Value{object.Symbol(name)}, args...)
			return vm.send(self, "method_missing", fwd, blk)
		},
	}
}

// every Method created by define / OpDefineMethod / define_(singleton_)method
// carries a non-nil owner, so b.m.owner is always set.
func (b *BoundMethod) ToS() string {
	return b.vm.formatCallableString("Method", b.recv, b.name, b.m)
}
func (b *BoundMethod) Inspect() string { return b.ToS() }
func (b *BoundMethod) Truthy() bool    { return true }

// registerMethod installs the Method class and Object#method.
func (vm *VM) registerMethod() {
	vm.cMethod = newClass("Method", vm.cObject)
	vm.consts["Method"] = vm.cMethod

	vm.cObject.define("method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := args[0].ToS()
		m := vm.resolveMethod(self, name)
		if m == nil {
			// A name the receiver answers only through respond_to_missing? (with
			// include_private true) still yields a callable Method whose body routes
			// to method_missing dynamically — MRI resolves method_missing at call
			// time, so redefining it after the fact changes the result and the
			// original name is never invoked even if it later comes to exist.
			if vm.send(self, "respond_to_missing?", []object.Value{object.Symbol(name), object.True}, nil).Truthy() {
				return vm.newBoundMethod(self, name, vm.methodMissingMethod(self, name))
			}
			return raise("NameError", "undefined method '%s' for %s", name, vm.classOf(self).name)
		}
		return vm.newBoundMethod(self, name, m)
	})

	call := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		b := self.(*BoundMethod)
		return vm.invoke(b.m, b.recv, args, blk)
	}
	vm.cMethod.define("call", call)
	// Method#[] and Method#=== are genuine built-in aliases of Method#call: MRI
	// exposes them as the same method definition, so #instance_method(:[]) equals
	// #instance_method(:call). aliasBuiltin shares the one *Method record (a fresh
	// define would compare unequal even with an identical body).
	aliasBuiltin(vm.cMethod, "[]", "call")
	aliasBuiltin(vm.cMethod, "===", "call")
	vm.cMethod.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(self.(*BoundMethod).name)
	})
	vm.cMethod.define("arity", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(methodArity(self.(*BoundMethod).m)))
	})
	vm.cMethod.define("owner", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*BoundMethod).m.owner // always non-nil for a resolved method
	})
	vm.cMethod.define("receiver", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*BoundMethod).recv
	})
	vm.cMethod.define("to_proc", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*BoundMethod)
		return &Proc{
			native:      func(vm *VM, args []object.Value) object.Value { return vm.send(b.recv, b.name, args, nil) },
			nativeArity: methodArity(b.m),
			isLambda:    true,
		}
	})
}

// resolveMethod finds the Method that would handle recv.name (singleton methods
// and the class chain), or nil. Operators handled by the compiler fast path are
// not real methods and won't resolve here.
func (vm *VM) resolveMethod(recv object.Value, name string) *Method {
	if cls, ok := recv.(*RClass); ok {
		if m := lookupSMethod(cls, name); m != nil {
			return m
		}
	}
	c := vm.classOf(recv)
	if sc := vm.objSingleton(recv); sc != nil {
		c = sc
	}
	return undefAsNil(lookupMethod(c, name))
}

// methodArity reports a method's arity in Ruby's convention. It mirrors MRI's
// rb_iseq_arity: the count is the required positional parameters (leading plus
// any after a *splat) plus one when the method has a required keyword. The result
// is that count when the method takes a fixed number of arguments, or -(count+1)
// when it is variadic — i.e. it has an optional positional or a splat, or it
// accepts extra keywords (an optional keyword or a **rest) without demanding any
// required keyword. A required keyword keeps the arity non-negative even
// alongside optional keywords or a **rest. Native methods report -1 (variadic).
func methodArity(m *Method) int {
	switch m.attrKind {
	case attrReaderMethod:
		return 0
	case attrWriterMethod:
		return 1
	}
	is := methodISeq(m)
	if is == nil {
		if m.proc != nil {
			return m.proc.arityVal()
		}
		return -1
	}
	reqKw, optKw := keywordCounts(is)
	req := iseqRequiredPositional(is)
	if reqKw > 0 {
		req++
	}
	extraKeywords := (optKw > 0 || is.KwRestSlot >= 0) && reqKw == 0
	if iseqHasOptional(is) || is.SplatIndex >= 0 || extraKeywords {
		return -(req + 1)
	}
	return req
}

// methodISeq returns the ISeq that backs m's parameter shape: its own for a
// def/alias, or its block body's for a define_method-created method. nil for a
// native (or a define_method whose Proc carries no ISeq).
func methodISeq(m *Method) *bytecode.ISeq {
	switch {
	case m.iseq != nil:
		return m.iseq
	case m.proc != nil:
		return m.proc.iseq
	default:
		return nil
	}
}

// iseqRequiredPositional counts an ISeq's required positional parameters: the
// NumRequired leading ones plus any required parameters after a *splat (the
// "post" params, which occupy the Params slots following SplatIndex).
func iseqRequiredPositional(is *bytecode.ISeq) int {
	req := is.NumRequired
	if is.SplatIndex >= 0 {
		req += len(is.Params) - is.SplatIndex - 1
	}
	return req
}

// iseqHasOptional reports whether an ISeq has at least one optional positional
// parameter: with a splat, the slots between NumRequired and SplatIndex; without
// one, everything after NumRequired (post-without-splat is treated as optional,
// which the ISeq encoding does not distinguish).
func iseqHasOptional(is *bytecode.ISeq) bool {
	if is.SplatIndex >= 0 {
		return is.SplatIndex > is.NumRequired
	}
	return len(is.Params) > is.NumRequired
}

// keywordCounts reports how many of an ISeq's keyword parameters are required
// and how many are optional. A **nil "no keywords" marker (recorded as the
// sentinel keyword-rest name "nil") is not a keyword parameter, so it counts as
// neither.
func keywordCounts(is *bytecode.ISeq) (required, optional int) {
	for _, req := range is.KwRequired {
		if req {
			required++
		} else {
			optional++
		}
	}
	return required, optional
}
