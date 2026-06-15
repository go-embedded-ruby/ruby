package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// setupStruct installs the Struct class. Struct.new(:a, :b) is a singleton
// method that mints a fresh subclass whose instances carry the named members as
// instance variables, with accessors and the usual value methods.
func setupStruct(vm *VM) {
	cStruct := newClass("Struct", vm.cObject)
	vm.consts["Struct"] = cStruct
	cStruct.smethods["new"] = &Method{name: "new", owner: cStruct, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		names := make([]string, len(args))
		for i, a := range args {
			names[i] = a.ToS()
		}
		return vm.newStructClass(cStruct, names)
	}}
}

// newStructClass builds the anonymous subclass returned by Struct.new.
func (vm *VM) newStructClass(parent *RClass, names []string) *RClass {
	sub := newClass("", parent)
	// Its own `new` allocates an instance (overriding Struct.new, which would
	// otherwise be inherited and make yet another class).
	sub.smethods["new"] = &Method{name: "new", owner: sub, native: nativeNew}

	for _, nm := range names {
		ivar := "@" + nm
		// initialize always populates every member ivar, so a plain map read
		// (no presence check) always returns the stored value.
		sub.define(nm, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return self.(*RObject).ivars[ivar]
		})
		sub.define(nm+"=", func(_ *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
			self.(*RObject).ivars[ivar] = a[0]
			return a[0]
		})
	}

	values := func(self object.Value) []object.Value {
		o := self.(*RObject)
		out := make([]object.Value, len(names))
		for i, nm := range names {
			out[i] = o.ivars["@"+nm]
		}
		return out
	}

	sub.define("initialize", func(_ *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		if len(a) > len(names) {
			raise("ArgumentError", "struct size differs")
		}
		o := self.(*RObject)
		for i, nm := range names {
			if i < len(a) {
				o.ivars["@"+nm] = a[i]
			} else {
				o.ivars["@"+nm] = object.NilV
			}
		}
		return object.NilV
	})
	sub.define("members", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		out := make([]object.Value, len(names))
		for i, nm := range names {
			out[i] = object.Symbol(nm)
		}
		return &object.Array{Elems: out}
	})
	toA := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Array{Elems: values(self)}
	}
	sub.define("to_a", toA)
	sub.define("values", toA)
	sub.define("deconstruct", toA)
	sizeFn := func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(len(names))
	}
	sub.define("size", sizeFn)
	sub.define("length", sizeFn)
	sub.define("to_h", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		vals := values(self)
		for i, nm := range names {
			h.Set(object.Symbol(nm), vals[i])
		}
		return h
	})
	sub.define("[]", func(_ *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		vals := values(self)
		switch k := a[0].(type) {
		case object.Integer:
			idx := int(k)
			if idx < 0 {
				idx += len(vals)
			}
			if idx < 0 || idx >= len(vals) {
				raise("IndexError", "offset %d too large for struct(size:%d)", int(k), len(vals))
			}
			return vals[idx]
		case object.Symbol:
			return structMember(string(k), names, vals)
		case object.String:
			return structMember(string(k), names, vals)
		default:
			raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(a[0]))
			return object.NilV
		}
	})
	sub.define("==", func(_ *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		other, ok := a[0].(*RObject)
		if !ok || other.class != self.(*RObject).class {
			return object.False
		}
		sv, ov := values(self), values(other)
		for i := range sv {
			if !valueEqual(sv[i], ov[i]) {
				return object.False
			}
		}
		return object.True
	})
	return sub
}

// structMember returns the value for a member looked up by name, or raises
// NameError when no such member exists.
func structMember(name string, names []string, vals []object.Value) object.Value {
	for i, nm := range names {
		if nm == name {
			return vals[i]
		}
	}
	raise("NameError", "no member '%s' in struct", name)
	return object.NilV
}
