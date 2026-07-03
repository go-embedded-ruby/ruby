package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// setupStruct installs the Struct class. Struct.new(:a, :b) is a singleton
// method that mints a fresh subclass whose instances carry the named members as
// instance variables, with accessors and the usual value methods.
func setupStruct(vm *VM) {
	cStruct := newClass("Struct", vm.cObject)
	vm.consts["Struct"] = cStruct
	cStruct.smethods["new"] = &Method{name: "new", owner: cStruct, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		// A trailing options hash carries keyword_init: it is not a member name.
		kwInit := false
		if n := len(args); n > 0 {
			if h, ok := object.KindOK[*object.Hash](args[n-1]); ok {
				if v, present := h.Get(object.Symbol("keyword_init")); present {
					kwInit = v.Truthy()
					args = args[:n-1]
				}
			}
		}
		names := make([]string, len(args))
		for i, a := range args {
			names[i] = a.ToS()
		}
		sub := vm.newStructClass(cStruct, names, kwInit)
		if blk != nil {
			// Struct.new(:a) { def m; …; end } evaluates the body in the new
			// subclass, just like class_eval, so it can add methods/constants. The
			// block was written in some lexical scope (e.g. inside `module Outer`), so
			// bare-constant lookup in the body must reach that scope: record the
			// block's captured lexical scope as the new class's lexParent so
			// resolveConst walks sub -> Outer and finds constants like an included
			// module defined alongside the Struct.new call (matches MRI).
			if blk.cref != nil && blk.cref != vm.cObject {
				sub.lexParent = blk.cref
			}
			vm.classEval(sub, blk, nil)
		}
		return sub
	}}
}

// newStructClass builds the anonymous subclass returned by Struct.new. With
// kwInit, instances are constructed from keyword arguments (S.new(a: 1, b: 2))
// rather than positionally.
func (vm *VM) newStructClass(parent *RClass, names []string, kwInit bool) *RClass {
	sub := newClass("", parent)
	// Its own `new` allocates an instance (overriding Struct.new, which would
	// otherwise be inherited and make yet another class).
	sub.smethods["new"] = &Method{name: "new", owner: sub, native: nativeNew}

	for _, nm := range names {
		ivar := "@" + nm
		// initialize always populates every member ivar, so a plain map read
		// (no presence check) always returns the stored value.
		sub.define(nm, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Kind[*RObject](self).ivars[ivar]
		})
		sub.define(nm+"=", func(_ *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
			object.Kind[*RObject](self).ivars[ivar] = a[0]
			return a[0]
		})
	}

	values := func(self object.Value) []object.Value {
		o := object.Kind[*RObject](self)
		out := make([]object.Value, len(names))
		for i, nm := range names {
			out[i] = o.ivars["@"+nm]
		}
		return out
	}

	sub.define("initialize", func(_ *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		o := object.Kind[*RObject](self)
		if kwInit {
			// Members come from a keyword hash; absent members are nil, unknown
			// keys raise, exactly as MRI's keyword_init structs do.
			h := object.NewHash()
			if len(a) > 0 {
				hh, ok := object.KindOK[*object.Hash](a[0])
				if !ok || len(a) > 1 {
					raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(a))
				}
				h = hh
			}
			member := map[string]bool{}
			for _, nm := range names {
				member[nm] = true
				v, ok := h.Get(object.Symbol(nm))
				if !ok {
					v = object.NilV
				}
				o.ivars["@"+nm] = v
			}
			for _, k := range h.Keys {
				if sym, ok := object.KindOK[object.Symbol](k); !ok || !member[string(sym)] {
					raise("ArgumentError", "unknown keyword: %s", k.Inspect())
				}
			}
			return object.NilV
		}
		if len(a) > len(names) {
			raise("ArgumentError", "struct size differs")
		}
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
		return object.NewArrayFromSlice(out)
	})
	toA := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(values(self))
	}
	sub.define("to_a", toA)
	sub.define("values", toA)
	sub.define("deconstruct", toA)
	sizeFn := func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(names)))
	}
	sub.define("size", sizeFn)
	sub.define("length", sizeFn)
	toH := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		vals := values(self)
		for i, nm := range names {
			h.Set(object.Symbol(nm), vals[i])
		}
		return h
	}
	sub.define("to_h", toH)
	// deconstruct_keys backs case/in hash patterns; the requested-key list is
	// advisory, so we return the full member hash.
	sub.define("deconstruct_keys", toH)
	sub.define("[]", func(_ *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		vals := values(self)
		{
			__sw171 := a[0]
			switch {
			case object.IsInt(__sw171):
				k := object.AsInteger(__sw171)
				_ = k
				idx := int(k)
				if idx < 0 {
					idx += len(vals)
				}
				if idx < 0 || idx >= len(vals) {
					raise("IndexError", "offset %d too large for struct(size:%d)", int(k), len(vals))
				}
				return vals[idx]
			case object.IsKind[object.Symbol](__sw171):
				k := object.Kind[object.Symbol](__sw171)
				_ = k
				return structMember(string(k), names, vals)
			case object.IsKind[*object.String](__sw171):
				k := object.Kind[*object.String](__sw171)
				_ = k
				return structMember(k.Str(), names, vals)
			default:
				k := __sw171
				_ = k
				raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(a[0]))
				return object.NilV
			}
		}
	})
	sub.define("==", func(_ *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		other, ok := object.KindOK[*RObject](a[0])
		if !ok || other.class != object.Kind[*RObject](self).class {
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
	// Struct is Enumerable: each yields the member values in order, and the
	// Enumerable mixin then supplies map/select/min/sum/… on top of it.
	sub.define("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		for _, v := range values(self) {
			vm.callBlock(blk, []object.Value{v})
		}
		return self
	})
	// []= assigns a member by index, Symbol or String name (the inverse of []),
	// raising the same errors for an out-of-range index or unknown name.
	sub.define("[]=", func(_ *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		o := object.Kind[*RObject](self)
		{
			__sw172 := a[0]
			switch {
			case object.IsInt(__sw172):
				k := object.AsInteger(__sw172)
				_ = k
				idx := int(k)
				if idx < 0 {
					idx += len(names)
				}
				if idx < 0 || idx >= len(names) {
					raise("IndexError", "offset %d too large for struct(size:%d)", int(k), len(names))
				}
				o.ivars["@"+names[idx]] = a[1]
			case object.IsKind[object.Symbol](__sw172):
				k := object.Kind[object.Symbol](__sw172)
				_ = k
				structSetMember(o, string(k), names, a[1])
			case object.IsKind[*object.String](__sw172):
				k := object.Kind[*object.String](__sw172)
				_ = k
				structSetMember(o, k.Str(), names, a[1])
			default:
				k := __sw172
				_ = k
				raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(a[0]))
			}
		}
		return a[1]
	})
	// each_pair yields [member, value] pairs in declaration order.
	sub.define("each_pair", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each_pair)")
		}
		vals := values(self)
		for i, nm := range names {
			vm.callBlock(blk, []object.Value{object.Symbol(nm), vals[i]})
		}
		return self
	})
	// Class-level members: Etc::Passwd.members and friends read the member list
	// off the Struct class itself.
	memberSyms := make([]object.Value, len(names))
	for i, nm := range names {
		memberSyms[i] = object.Symbol(nm)
	}
	sub.smethods["members"] = &Method{name: "members", owner: sub,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NewArrayFromSlice(append([]object.Value(nil), memberSyms...))
		}}
	sub.includes = append(sub.includes, object.Kind[*RClass](vm.consts["Enumerable"]))
	bumpMethodSerial()
	return sub
}

// structSetMember assigns the named member's ivar on a Struct instance, raising
// NameError when the name is not a member.
func structSetMember(o *RObject, name string, names []string, v object.Value) {
	for _, nm := range names {
		if nm == name {
			o.ivars["@"+nm] = v
			return
		}
	}
	raise("NameError", "no member '%s' in struct", name)
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
