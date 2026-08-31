package vm

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// anonClassRepr renders an anonymous class/module the way MRI does inside a
// recursive Data #inspect sentinel — "#<Class:0x…>" / "#<Module:0x…>" with the
// class's identity address — so a self-referential anonymous Data prints
// "#<data #<Class:0x…>:...>" (matching MRI's rb_class_name fallback).
func anonClassRepr(c *RClass) string {
	kind := "Class"
	if c.isModule {
		kind = "Module"
	}
	return fmt.Sprintf("#<%s:0x%016x>", kind, reflect.ValueOf(c).Pointer())
}

// dataDef records the member layout of a Data subclass minted by Data.define.
// Unlike structDef there is no keyword-init tri-state: a Data class always
// accepts both positional and keyword construction. It is stored on the RClass
// and resolved through the superclass chain by dataDefOf.
type dataDef struct {
	names []string
}

// dataDefOf returns the nearest dataDef up c's superclass chain, or nil when c is
// not a Data subclass. Walking the chain lets `class Foo < SomeData` (a plain
// subclass of a Data.define class) share its parent's members.
func dataDefOf(c *RClass) *dataDef {
	for ; c != nil; c = c.super {
		if c.dataDef != nil {
			return c.dataDef
		}
	}
	return nil
}

// memberDefNames returns the member names of a Struct- or Data-defined class, and
// true, or (nil, false) when the class carries neither layout. It lets the shared
// value helpers (structEqual/structHash) treat Struct and Data uniformly, so a
// Data whose member is itself a Data recurses under the same cycle guard.
func memberDefNames(c *RClass) ([]string, bool) {
	if d := structDefOf(c); d != nil {
		return d.names, true
	}
	if d := dataDefOf(c); d != nil {
		return d.names, true
	}
	return nil, false
}

// hasMemberDef reports whether c carries a Struct or Data member layout.
func hasMemberDef(c *RClass) bool {
	_, ok := memberDefNames(c)
	return ok
}

// setupData installs the Data class (Ruby 3.2+ immutable value objects).
// Data.define(:a, :b) mints a frozen-instance subclass whose members are stored
// in RObject.structVals (shared with Struct — a Data is an immutable Struct with
// no setters). The value methods (#with, #to_h, #==, #inspect, …) live on Data
// itself and read the member layout from the instance's class, so a subclass or
// an included module can still override them. Data itself is abstract: Data.new
// is undefined and only Data.define mints usable classes.
func setupData(vm *VM) {
	cData := newClass("Data", vm.cObject)
	vm.consts["Data"] = cData

	// Data.new is undefined (MRI): only Data.define mints instantiable classes.
	// The message names the actual receiver, so `class Bad < Data; Bad.new` reports
	// "class Bad". Each class minted by Data.define shadows this with its own .new.
	cData.smethods["new"] = &Method{name: "new", owner: cData, native: func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return raise("NoMethodError", "undefined method 'new' for class %s", self.(*RClass).name)
	}}

	// Data.define(*members, &block) mints an anonymous Data subclass. Members are
	// Symbols or Strings; a String first argument is a member (Data has no name
	// argument, unlike Struct.new). The class takes its name lazily from the
	// constant it is first assigned to (Foo = Data.define(:a)).
	cData.smethods["define"] = &Method{name: "define", owner: cData, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		base := self.(*RClass)
		names := make([]string, len(args))
		seen := map[string]bool{}
		for i, a := range args {
			var nm string
			switch v := a.(type) {
			case object.Symbol:
				nm = string(v)
			case *object.String:
				nm = v.Str()
			default:
				raise("TypeError", "%s is not a symbol nor a string", a.Inspect())
			}
			if seen[nm] {
				raise("ArgumentError", "duplicate member: %s", nm)
			}
			seen[nm] = true
			names[i] = nm
		}
		sub := vm.newDataClass(base, names)
		if blk != nil {
			// The block is class-evaluated (self = the new class), so it can define
			// instance methods that use the readers. Record its captured lexical scope
			// so bare-constant lookup in the body reaches where it was written.
			if blk.cref != nil && blk.cref != vm.cObject {
				sub.lexParent = blk.cref
			}
			vm.classEval(sub, blk, []object.Value{sub})
		}
		return sub
	}}

	// --- shared value methods (defined on Data, read members per instance) ---

	// #initialize validates a keyword hash and stores the members. Data's .new
	// converts positional construction into a keyword hash before dispatching here,
	// so a subclass may override #initialize and always sees keyword arguments (and
	// can call super). structVals is (re)allocated so a bare-allocate + #initialize
	// (Data.instance_method(:initialize).bind_call) also works.
	cData.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*RObject)
		names := dataDefOf(o.class).names
		n := len(names)
		if len(o.structVals) != n {
			o.structVals = make([]object.Value, n)
		}
		var h *object.Hash
		switch len(args) {
		case 0:
			// no keyword hash: every member is missing (unless there are none)
		case 1:
			hh, ok := args[0].(*object.Hash)
			if !ok {
				raise("ArgumentError", "wrong number of arguments (given 1, expected 0)")
			}
			h = hh
		default:
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
		}
		idx := make(map[string]int, n)
		for i, nm := range names {
			idx[nm] = i
		}
		vals := make([]object.Value, n)
		provided := make([]bool, n)
		var unknown []object.Value
		if h != nil {
			for _, k := range h.Keys {
				nm, coerced := dataKeyName(vm, k)
				v, _ := h.Get(k)
				if i, ok := idx[nm]; ok {
					vals[i] = v
					provided[i] = true
				} else {
					unknown = append(unknown, coerced)
				}
			}
		}
		var missing []object.Value
		for i, nm := range names {
			if !provided[i] {
				missing = append(missing, object.Symbol(nm))
			}
		}
		// MRI reports missing members before unknown keywords.
		if len(missing) > 0 {
			dataKeywordError("missing", missing)
		}
		if len(unknown) > 0 {
			dataKeywordError("unknown", unknown)
		}
		for i := range names {
			o.structVals[i] = vals[i]
		}
		return object.NilV
	})

	cData.define("members", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return structMemberSyms(dataDefOf(self.(*RObject).class).names)
	})

	// #with(**kw) returns a NEW frozen instance with the given members replaced;
	// with no arguments it returns self. It allocates directly and drives
	// #initialize (so an overridden #initialize runs and unknown keys raise), never
	// routing through .new.
	cData.define("with", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*RObject)
		names := dataDefOf(o.class).names
		kw, pos := splitDataKwargs(args)
		if len(pos) > 0 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
		}
		if kw == nil {
			return self
		}
		merged := object.NewHash()
		for i, nm := range names {
			merged.Set(object.Symbol(nm), o.structVals[i])
		}
		for _, k := range kw.Keys {
			v, _ := kw.Get(k)
			merged.Set(k, v)
		}
		obj := &RObject{class: o.class, ivars: map[string]object.Value{}}
		obj.structVals = make([]object.Value, len(names))
		for i := range obj.structVals {
			obj.structVals[i] = object.NilV
		}
		vm.send(obj, "initialize", []object.Value{merged}, nil)
		obj.frozen = true
		return obj
	})

	cData.define("to_h", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		o := self.(*RObject)
		names := dataDefOf(o.class).names
		h := object.NewHash()
		for i, nm := range names {
			if blk != nil {
				res := vm.callBlock(blk, []object.Value{object.Symbol(nm), o.structVals[i]})
				key, val := dataToHPair(vm, res)
				h.Set(key, val)
			} else {
				h.Set(object.Symbol(nm), o.structVals[i])
			}
		}
		return h
	})

	cData.define("deconstruct", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vals := structValues(self)
		out := make([]object.Value, len(vals))
		copy(out, vals)
		return object.NewArrayFromSlice(out)
	})

	cData.define("deconstruct_keys", func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		if len(a) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(a))
		}
		names := dataDefOf(self.(*RObject).class).names
		vals := structValues(self)
		h := object.NewHash()
		if object.IsNil(a[0]) { // nil -> every member
			for i, nm := range names {
				h.Set(object.Symbol(nm), vals[i])
			}
			return h
		}
		keys, ok := a[0].(*object.Array)
		if !ok {
			raise("TypeError", "wrong argument type %s (expected Array or nil)", classNameOf(a[0]))
		}
		if len(keys.Elems) > len(names) {
			return h // more keys than members: MRI returns {} without inspecting them
		}
		for _, k := range keys.Elems {
			nm, coerced := dataKeyName(vm, k)
			i, found := structFind(nm, names)
			if !found {
				break // stop at the first key that names no member
			}
			h.Set(coerced, vals[i])
		}
		return h
	})

	cData.define("==", func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.structEqual(self, a[0], false, nil))
	})
	cData.define("eql?", func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.structEqual(self, a[0], true, nil))
	})
	cData.define("hash", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(vm.structHash(self.(*RObject), nil))
	})

	cData.define("to_s", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.dataInspect(self.(*RObject)))
	})
	// #inspect is a true alias of #to_s (shared Method record).
	aliasBuiltin(cData, "inspect", "to_s")

	bumpMethodSerial()
}

// newDataClass builds the anonymous subclass returned by Data.define: it records
// the member layout, installs the per-member readers (no writers — Data is
// immutable) and the class-level helpers (.new / .[] which construct a frozen
// instance, and .members), then returns it. base is the receiver of Data.define
// (Data itself, or a subclass of it).
func (vm *VM) newDataClass(base *RClass, names []string) *RClass {
	sub := newClass("", base)
	sub.dataDef = &dataDef{names: names}

	dataAlloc := &Method{name: "new", owner: sub, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.dataNew(self.(*RClass), args, blk)
	}}
	sub.smethods["new"] = dataAlloc
	// Data[] (the subclass's .[]) is a synonym for .new.
	sub.smethods["[]"] = dataAlloc

	for i, nm := range names {
		idx := i
		sub.define(nm, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return self.(*RObject).structVals[idx]
		})
	}

	// Class-level .members lives only on classes minted by Data.define (not on Data
	// itself nor on a plain `class X < Data`), matching MRI.
	sub.smethods["members"] = &Method{name: "members", owner: sub,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return structMemberSyms(names)
		}}
	return sub
}

// dataNew implements a Data subclass's .new (and .[]): it classifies the call as
// positional or keyword, normalises it into a keyword hash (matching MRI, which
// converts positional arguments to keywords before calling #initialize), drives
// #initialize and freezes the result. Arity errors that MRI raises from .new
// (too many positional arguments, or positional mixed with keywords) are raised
// here; a missing member surfaces from #initialize as a "missing keyword" error.
func (vm *VM) dataNew(cls *RClass, args []object.Value, blk *Proc) object.Value {
	names := dataDefOf(cls).names
	n := len(names)
	kw, pos := splitDataKwargs(args)
	var initHash *object.Hash
	if kw != nil {
		if len(pos) > 0 {
			// keyword given alongside positional arguments: MRI counts the keyword hash
			// as one of the given arguments.
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(pos)+1)
		}
		initHash = kw
	} else {
		if len(pos) > n {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0..%d)", len(pos), n)
		}
		initHash = object.NewHash()
		for i := 0; i < len(pos); i++ {
			initHash.Set(object.Symbol(names[i]), pos[i])
		}
	}
	obj := &RObject{class: cls, ivars: map[string]object.Value{}}
	obj.structVals = make([]object.Value, n)
	for i := range obj.structVals {
		obj.structVals[i] = object.NilV
	}
	vm.send(obj, "initialize", []object.Value{initHash}, blk)
	obj.frozen = true
	return obj
}

// splitDataKwargs separates a native Data call's arguments into its trailing
// keyword Hash and the leading positional arguments, returning (nil, args) when
// no keyword Hash is present. An empty keyword splat (`Point.new(1, **{})`) is
// elided by the caller before the native runs, so it never reaches here and
// stays positional.
func splitDataKwargs(args []object.Value) (*object.Hash, []object.Value) {
	m := len(args)
	if m == 0 {
		return nil, args
	}
	h, ok := args[m-1].(*object.Hash)
	if !ok {
		return nil, args
	}
	return h, args[:m-1]
}

// dataKeyName resolves a Data keyword/member key to its member name and the
// coerced key value (used both to store #with / #deconstruct_keys results under
// the given key and to render an "unknown keyword" error). Symbols and Strings
// map directly; any other object is converted with #to_str (which must return a
// String); a non-coercible key raises the MRI TypeError.
func dataKeyName(vm *VM, k object.Value) (string, object.Value) {
	switch key := k.(type) {
	case object.Symbol:
		return string(key), key
	case *object.String:
		return key.Str(), key
	default:
		if vm.respondsTo(k, "to_str") {
			r := vm.send(k, "to_str", nil, nil)
			s, ok := r.(*object.String)
			if !ok {
				raise("TypeError", "can't convert %s into String", classNameOf(k))
			}
			return s.Str(), s
		}
		raise("TypeError", "%s is not a symbol nor a string", k.Inspect())
		return "", nil
	}
}

// dataKeywordError raises MRI's "missing keyword(s)" / "unknown keyword(s)"
// ArgumentError: singular when one key, plural otherwise, each key rendered with
// #inspect (so Symbols read `:a` and Strings read `"a"`).
func dataKeywordError(kind string, keys []object.Value) {
	label := kind + " keyword"
	if len(keys) != 1 {
		label = kind + " keywords"
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k.Inspect()
	}
	raise("ArgumentError", "%s: %s", label, strings.Join(parts, ", "))
}

// dataToHPair validates one [key, value] pair returned by a #to_h block. The pair
// must be an Array (or #to_ary-coercible — never #to_a) of exactly two elements,
// matching MRI's errors.
func dataToHPair(vm *VM, res object.Value) (object.Value, object.Value) {
	arr, ok := res.(*object.Array)
	if !ok {
		if vm.respondsTo(res, "to_ary") {
			if a, isArr := vm.send(res, "to_ary", nil, nil).(*object.Array); isArr {
				arr, ok = a, true
			}
		}
		if !ok {
			raise("TypeError", "wrong element type %s (expected array)", vm.classOf(res).name)
		}
	}
	if len(arr.Elems) != 2 {
		raise("ArgumentError", "element has wrong array length (expected 2, was %d)", len(arr.Elems))
	}
	return arr.Elems[0], arr.Elems[1]
}

// dataInspect renders MRI's Data #inspect / #to_s: `#<data Name a=1, b=2>`, or
// without the name for an anonymous class (`#<data a=1>`). It uses the class's
// real name (never a #name override) and a cycle guard so a self-referential Data
// renders the recursive member as `#<data Name:...>` rather than looping.
func (vm *VM) dataInspect(o *RObject) string {
	names := dataDefOf(o.class).names
	if !object.ReprEnter(o) {
		nm := o.class.name
		if !namedThroughNesting(o.class) {
			nm = anonClassRepr(o.class) // anonymous / non-permanent path: #<Class:0x…>
		}
		return "#<data " + nm + ":...>"
	}
	defer object.ReprLeave(o)
	var b strings.Builder
	b.WriteString("#<data")
	if namedThroughNesting(o.class) { // the real class name, never #name (which may be overridden)
		b.WriteString(" ")
		b.WriteString(o.class.name)
	}
	for i, nm := range names {
		if i == 0 {
			b.WriteString(" ")
		} else {
			b.WriteString(", ")
		}
		b.WriteString(nm)
		b.WriteString("=")
		b.WriteString(vm.inspectStr(o.structVals[i]))
	}
	b.WriteString(">")
	return b.String()
}
