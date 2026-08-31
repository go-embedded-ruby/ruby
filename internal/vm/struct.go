package vm

import (
	"reflect"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// structDef records the member layout of a Struct subclass minted by
// Struct.new. kwInit is a tri-state: kwUnset (Struct.new(:a) — positional),
// kwTrue (keyword_init: true — keyword-only) or kwFalse (keyword_init: false —
// positional, same as unset). It is stored on the RClass and resolved through
// the superclass chain by structDefOf.
type structDef struct {
	names  []string
	kwInit int8
}

const (
	kwUnset int8 = iota
	kwTrue
	kwFalse
)

// structDefOf returns the nearest structDef up c's superclass chain, or nil when
// c is not a Struct subclass. Walking the chain lets `class Foo < Struct.new(:a)`
// (and deeper subclasses like StructClasses::Honda) share their parent's members.
func structDefOf(c *RClass) *structDef {
	for ; c != nil; c = c.super {
		if c.structDef != nil {
			return c.structDef
		}
	}
	return nil
}

// structValues returns the live member-value slice of the Struct instance self
// (which every caller reaches only for a genuine Struct instance).
func structValues(self object.Value) []object.Value {
	return self.(*RObject).structVals
}

// setupStruct installs the Struct class. Struct.new(:a, :b) mints a fresh
// subclass whose instances carry the named members (stored in RObject.structVals,
// not in instance variables — matching MRI), with per-member accessors. The value
// methods (#[], #to_a, #==, #hash, #inspect, …) live on Struct itself and read
// the member layout from the instance's class, so a module included into a
// subclass can still override them (MRI ancestry: subclass → included module →
// Struct).
func setupStruct(vm *VM) {
	cStruct := newClass("Struct", vm.cObject)
	vm.consts["Struct"] = cStruct

	// Struct.new mints a subclass. Inherited by direct subclasses of Struct
	// (class Apple < Struct; Apple.new("X", :a) defines the constant in Apple's
	// namespace), so the receiver class drives the parent and the name scope.
	cStruct.smethods["new"] = &Method{name: "new", owner: cStruct, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		base := self.(*RClass)
		kwInit := kwUnset
		// A trailing Hash carrying keyword_init: is the options hash, not a member.
		// A trailing Hash WITHOUT keyword_init: is left in args, where the member
		// loop rejects it (TypeError) — matching Struct.new(:a, {name: 'x'}).
		if n := len(args); n > 0 {
			if h, ok := args[n-1].(*object.Hash); ok {
				if v, present := h.Get(object.Symbol("keyword_init")); present {
					switch {
					case object.IsNil(v):
						kwInit = kwUnset
					case v.Truthy():
						kwInit = kwTrue
					default:
						kwInit = kwFalse
					}
					args = args[:n-1]
				}
			}
		}
		// The first argument, when a String (or nil, or #to_str-able), names the
		// struct as a constant under the receiver; a Symbol first argument is a
		// member, and no name is assigned.
		name := ""
		haveName := false
		if len(args) > 0 {
			switch a0 := args[0].(type) {
			case object.Nil:
				haveName, args = true, args[1:]
			case *object.String:
				name, haveName, args = a0.Str(), true, args[1:]
			case object.Symbol:
				// a member; leave in args
			default:
				if vm.respondsTo(a0, "to_str") {
					name, haveName, args = vm.send(a0, "to_str", nil, nil).ToS(), true, args[1:]
				}
			}
		}
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
		sub := vm.newStructClass(base, names, kwInit)
		// keyword_init? is a class method of each minted subclass (never of Struct
		// itself): true / false when keyword_init: was given, nil when it was omitted.
		sub.smethods["keyword_init?"] = &Method{name: "keyword_init?", owner: sub,
			native: func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
				switch structDefOf(self.(*RClass)).kwInit {
				case kwTrue:
					return object.True
				case kwFalse:
					return object.False
				default:
					return object.NilV
				}
			}}
		if haveName && name != "" {
			if !validConstName(name) {
				raise("NameError", "identifier %s needs to be constant", name)
			}
			if _, exists := base.consts[name]; exists {
				vm.warnRedefineConst(base, name)
			}
			vm.assignConstIn(base, name, sub)
		}
		if blk != nil {
			// The block is class-evaluated (self = the new class), so it can define
			// methods/constants and set class ivars; it also receives the class as its
			// block parameter (Struct.new(:a) { |cls| … }). Record the block's captured
			// lexical scope as lexParent so bare-constant lookup in the body reaches the
			// scope it was written in (matches MRI).
			if blk.cref != nil && blk.cref != vm.cObject {
				sub.lexParent = blk.cref
			}
			vm.classEval(sub, blk, []object.Value{sub})
		}
		return sub
	}}

	// --- shared value methods (defined on Struct, read members per instance) ---

	cStruct.define("initialize", func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		// structVals is always pre-allocated by the subclass's .new (structAlloc)
		// before #initialize runs, so it is never nil here.
		o := self.(*RObject)
		d := structDefOf(o.class)
		names := d.names
		if d.kwInit == kwTrue {
			// Members come from a keyword hash; missing members stay nil, unknown keys
			// raise, and a positional (non-hash) argument is a wrong-arg-count error.
			var h *object.Hash
			if len(a) > 1 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(a))
			}
			if len(a) == 1 {
				hh, ok := a[0].(*object.Hash)
				if !ok {
					raise("ArgumentError", "wrong number of arguments (given 1, expected 0)")
				}
				h = hh
			}
			member := map[string]int{}
			for i, nm := range names {
				member[nm] = i
			}
			if h != nil {
				var unknown []string
				for _, k := range h.Keys {
					sym, isSym := k.(object.Symbol)
					if idx, ok := member[string(sym)]; isSym && ok {
						v, _ := h.Get(k)
						o.structVals[idx] = v
					} else if isSym {
						unknown = append(unknown, string(sym))
					} else {
						unknown = append(unknown, k.Inspect())
					}
				}
				if len(unknown) > 0 {
					// MRI's Struct keyword error always uses the plural "keywords" and
					// names the bare symbols (no leading colon).
					raise("ArgumentError", "unknown keywords: %s", strings.Join(unknown, ", "))
				}
			}
			return object.NilV
		}
		// keyword_init unset (Ruby 3.2 auto-detect): a lone Hash argument whose keys
		// are all Symbols naming members is taken as keyword initialisation. MRI
		// decides this from the caller's keyword flag, which our calling convention
		// does not carry to a native method, so we approximate it by the keys naming
		// members — a Hash with any non-member or non-Symbol key stays a positional
		// value (so Struct.new(:a).new(b: 1) still stores {b: 1} in member a).
		if d.kwInit == kwUnset && len(a) == 1 && len(names) > 0 {
			if h, ok := a[0].(*object.Hash); ok && structHashAllMembers(h, names) {
				for i, nm := range names {
					if v, present := h.Get(object.Symbol(nm)); present {
						o.structVals[i] = v
					} else {
						o.structVals[i] = object.NilV
					}
				}
				return object.NilV
			}
		}
		if len(a) > len(names) {
			raise("ArgumentError", "struct size differs")
		}
		for i := range names {
			if i < len(a) {
				o.structVals[i] = a[i]
			} else {
				o.structVals[i] = object.NilV
			}
		}
		return object.NilV
	})
	// MRI keeps Struct#initialize private (Car.private_instance_methods includes
	// :initialize).
	vm.setInstanceVisibility(cStruct, "initialize", visPrivate)

	cStruct.define("members", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return structMemberSyms(structDefOf(self.(*RObject).class).names)
	})

	cStruct.define("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each")
		}
		for _, v := range structValues(self) {
			vm.callBlock(blk, []object.Value{v})
		}
		return self
	})

	cStruct.define("each_pair", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each_pair")
		}
		names := structDefOf(self.(*RObject).class).names
		vals := structValues(self)
		for i, nm := range names {
			// MRI yields ONE two-element Array per pair (like Hash#each): a `|k, v|`
			// block auto-splats it, while a single-parameter block (or an Enumerator
			// consumer, e.g. each_pair.map { |var| var }) receives the [name, value]
			// Array whole.
			vm.callBlock(blk, []object.Value{object.NewArrayFromSlice([]object.Value{object.Symbol(nm), vals[i]})})
		}
		return self
	})

	cStruct.define("[]", func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		if len(a) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(a))
		}
		names := structDefOf(self.(*RObject).class).names
		vals := structValues(self)
		switch k := a[0].(type) {
		case object.Integer:
			return vals[structIndex(int(k), len(vals))]
		case object.Symbol:
			return vals[structNameIndex(string(k), names)]
		case *object.String:
			return vals[structNameIndex(k.Str(), names)]
		default:
			raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(a[0]))
			return object.NilV
		}
	})

	cStruct.define("[]=", func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		o := self.(*RObject)
		if o.frozen {
			vm.raiseFrozen(self)
		}
		names := structDefOf(o.class).names
		switch k := a[0].(type) {
		case object.Integer:
			o.structVals[structIndex(int(k), len(names))] = a[1]
		case object.Symbol:
			o.structVals[structNameIndex(string(k), names)] = a[1]
		case *object.String:
			o.structVals[structNameIndex(k.Str(), names)] = a[1]
		default:
			raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(a[0]))
		}
		return a[1]
	})

	toA := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vals := structValues(self)
		out := make([]object.Value, len(vals))
		copy(out, vals)
		return object.NewArrayFromSlice(out)
	}
	cStruct.define("to_a", toA)
	// #values and #deconstruct are true aliases of #to_a (shared Method record, so
	// Struct.instance_method(:deconstruct) == Struct.instance_method(:to_a)).
	aliasBuiltin(cStruct, "values", "to_a")
	aliasBuiltin(cStruct, "deconstruct", "to_a")

	cStruct.define("to_h", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		names := structDefOf(self.(*RObject).class).names
		vals := structValues(self)
		h := object.NewHash()
		for i, nm := range names {
			if blk != nil {
				res := vm.callBlock(blk, []object.Value{object.Symbol(nm), vals[i]})
				// MRI coerces the returned pair with #to_ary (never #to_a); a non-Array,
				// non-to_ary object is a TypeError naming the object's real class.
				key, val := dataToHPair(vm, res)
				h.Set(key, val)
			} else {
				h.Set(object.Symbol(nm), vals[i])
			}
		}
		return h
	})

	cStruct.define("deconstruct_keys", func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		if len(a) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(a))
		}
		names := structDefOf(self.(*RObject).class).names
		vals := structValues(self)
		h := object.NewHash()
		if object.IsNil(a[0]) { // deconstruct_keys(nil) → every member
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
			idx, found := structDeconstructIndex(vm, k, names)
			if !found {
				break // stop at the first key that names no member / index out of range
			}
			h.Set(k, vals[idx])
		}
		return h
	})

	cStruct.define("values_at", func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		vals := structValues(self)
		out := []object.Value{}
		for _, arg := range a {
			if rng, ok := arg.(*object.Range); ok {
				out = append(out, structRangeValues(rng, vals)...)
				continue
			}
			i, ok := arg.(object.Integer)
			if !ok {
				raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(arg))
			}
			out = append(out, vals[structIndex(int(i), len(vals))])
		}
		return object.NewArrayFromSlice(out)
	})

	cStruct.define("dig", func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		if len(a) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		names := structDefOf(self.(*RObject).class).names
		vals := structValues(self)
		var v object.Value
		switch k := a[0].(type) {
		case object.Integer:
			idx := int(k)
			if idx < 0 {
				idx += len(vals)
			}
			if idx < 0 || idx >= len(vals) {
				return object.NilV
			}
			v = vals[idx]
		case object.Symbol:
			// Unlike #[], #dig with a name that is not a member returns nil (treating
			// the missing member as nil) rather than raising NameError.
			if i, found := structFind(string(k), names); found {
				v = vals[i]
			} else {
				v = object.NilV
			}
		case *object.String:
			if i, found := structFind(k.Str(), names); found {
				v = vals[i]
			} else {
				v = object.NilV
			}
		default:
			raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(a[0]))
		}
		if len(a) == 1 {
			return v
		}
		if object.IsNil(v) {
			return object.NilV
		}
		if !vm.respondsTo(v, "dig") {
			raise("TypeError", "%s does not have #dig method", classNameOf(v))
		}
		return vm.send(v, "dig", a[1:], nil)
	})

	sizeFn := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(structValues(self))))
	}
	cStruct.define("size", sizeFn)
	aliasBuiltin(cStruct, "length", "size")

	cStruct.define("==", func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.structEqual(self, a[0], false, nil))
	})
	cStruct.define("eql?", func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.structEqual(self, a[0], true, nil))
	})
	cStruct.define("hash", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(vm.structHash(self.(*RObject), nil))
	})

	cStruct.define("to_s", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*RObject)
		names := structDefOf(o.class).names
		vals := o.structVals
		var b strings.Builder
		b.WriteString("#<struct")
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
			b.WriteString(vm.inspectStr(vals[i]))
		}
		b.WriteString(">")
		return object.NewString(b.String())
	})
	// #inspect is an alias of #to_s (shared Method record).
	aliasBuiltin(cStruct, "inspect", "to_s")

	bumpMethodSerial()
}

// newStructClass builds the anonymous subclass returned by Struct.new: it records
// the member layout, installs the per-member accessors and the class-level
// helpers (.new which pre-allocates the member slots, .[] as a synonym for .new,
// and .members), then returns it. base is the receiver of Struct.new (Struct
// itself, or a direct subclass of it).
func (vm *VM) newStructClass(base *RClass, names []string, kwInit int8) *RClass {
	// Struct is Enumerable (map/select/min/sum/… on top of #each). Enumerable is
	// installed by the prelude, after Struct's bootstrap, so mix it into Struct
	// lazily here — every Struct subclass is minted after the prelude has loaded.
	cStruct := vm.consts["Struct"].(*RClass)
	if en, ok := vm.consts["Enumerable"].(*RClass); ok && !hasInclude(cStruct, en) {
		cStruct.includes = append(cStruct.includes, en)
	}
	sub := newClass("", base)
	sub.structDef = &structDef{names: names, kwInit: kwInit}

	// .new allocates the instance AND its member slots before #initialize runs, so
	// a subclass whose #initialize sets a member before calling super finds the
	// slots ready (StructClasses::Honda does exactly that).
	structAlloc := &Method{name: "new", owner: sub, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		cls := self.(*RClass)
		obj := &RObject{class: cls, ivars: map[string]object.Value{}}
		d := structDefOf(cls)
		obj.structVals = make([]object.Value, len(d.names))
		for i := range obj.structVals {
			obj.structVals[i] = object.NilV
		}
		vm.send(obj, "initialize", args, blk)
		return obj
	}}
	sub.smethods["new"] = structAlloc
	// Struct[] (i.e. the subclass's .[]) is a synonym for .new.
	sub.smethods["[]"] = structAlloc

	for i, nm := range names {
		idx := i
		sub.define(nm, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return self.(*RObject).structVals[idx]
		})
		sub.define(nm+"=", func(vm *VM, self object.Value, a []object.Value, _ *Proc) object.Value {
			o := self.(*RObject)
			if o.frozen {
				vm.raiseFrozen(self)
			}
			o.structVals[idx] = a[0]
			return a[0]
		})
	}

	// Class-level .members reads the layout off the Struct class itself. It lives
	// only on classes minted by Struct.new (not on Struct or a plain `class X <
	// Struct`), matching MRI.
	sub.smethods["members"] = &Method{name: "members", owner: sub,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return structMemberSyms(names)
		}}
	return sub
}

// namedThroughNesting reports whether c has a permanent classpath — its own name
// and every enclosing lexical namespace are named. MRI's Struct/Data #inspect
// (rb_class_path_cached) shows the class name only for such classes, omitting it
// for an anonymous class and for a class nested in an anonymous namespace (e.g.
// `class Foo < Struct.new(:a)` defined inside `Class.new`): the name our object
// model records ("Foo") is not the permanent path MRI would cache.
func namedThroughNesting(c *RClass) bool {
	if c.name == "" {
		return false
	}
	for p := c.lexParent; p != nil; p = p.lexParent {
		if p.name == "" {
			return false
		}
	}
	return true
}

// hasInclude reports whether module m is already in c's include list.
func hasInclude(c, m *RClass) bool {
	for _, inc := range c.includes {
		if inc == m {
			return true
		}
	}
	return false
}

// structHashAllMembers reports whether every key of a non-empty Hash is a Symbol
// naming a member — the shape that Struct#initialize (keyword_init unset) treats
// as keyword initialisation.
func structHashAllMembers(h *object.Hash, names []string) bool {
	if len(h.Keys) == 0 {
		return false
	}
	member := make(map[string]bool, len(names))
	for _, nm := range names {
		member[nm] = true
	}
	for _, k := range h.Keys {
		sym, ok := k.(object.Symbol)
		if !ok || !member[string(sym)] {
			return false
		}
	}
	return true
}

// structMemberSyms returns a fresh Array of the member names as Symbols.
func structMemberSyms(names []string) object.Value {
	out := make([]object.Value, len(names))
	for i, nm := range names {
		out[i] = object.Symbol(nm)
	}
	return object.NewArrayFromSlice(out)
}

// structIndex resolves a possibly-negative member index against a struct of the
// given size, raising the MRI IndexError (with the "too large" / "too small"
// wording) when out of range.
func structIndex(i, size int) int {
	idx := i
	if idx < 0 {
		idx += size
	}
	if idx < 0 {
		raise("IndexError", "offset %d too small for struct(size:%d)", i, size)
	}
	if idx >= size {
		raise("IndexError", "offset %d too large for struct(size:%d)", i, size)
	}
	return idx
}

// structNameIndex resolves a member name to its index, raising NameError when the
// name is not a member.
func structNameIndex(name string, names []string) int {
	for i, nm := range names {
		if nm == name {
			return i
		}
	}
	raise("NameError", "no member '%s' in struct", name)
	return 0
}

// structRangeValues returns the member values selected by a Range for
// #values_at: indices past the end contribute nil, and a start that is negative
// out of range (or past the end) raises RangeError — matching MRI.
func structRangeValues(rng *object.Range, vals []object.Value) []object.Value {
	size := len(vals)
	lo := 0
	if !object.IsNil(rng.Lo) {
		lo = int(intArg(rng.Lo))
		if lo < 0 {
			lo += size
		}
		if lo < 0 || lo > size {
			raise("RangeError", "%s out of range", rng.Inspect())
		}
	}
	hi := size // endless range runs to the last member
	if !object.IsNil(rng.Hi) {
		hi = int(intArg(rng.Hi))
		if hi < 0 {
			hi += size
		}
		if !rng.Exclusive {
			hi++
		}
	}
	if hi < lo {
		hi = lo
	}
	out := make([]object.Value, 0, hi-lo)
	for i := lo; i < hi; i++ {
		if i >= 0 && i < size {
			out = append(out, vals[i])
		} else {
			out = append(out, object.NilV)
		}
	}
	return out
}

// structDeconstructIndex resolves a #deconstruct_keys key to a member index.
// Symbol/String keys look up by name; anything else is coerced to an Integer
// (directly or via #to_int) and used as a position. A name/position that does not
// exist returns found=false so the caller stops; a non-coercible key raises
// TypeError.
func structDeconstructIndex(vm *VM, k object.Value, names []string) (int, bool) {
	switch key := k.(type) {
	case object.Symbol:
		return structFind(string(key), names)
	case *object.String:
		return structFind(key.Str(), names)
	case object.Integer:
		return structPos(int(key), len(names))
	default:
		if vm.respondsTo(k, "to_int") {
			iv := vm.send(k, "to_int", nil, nil)
			i, ok := iv.(object.Integer)
			if !ok {
				raise("TypeError", "can't convert %s into Integer", vm.classOf(k).name)
			}
			return structPos(int(i), len(names))
		}
		raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(k))
		return 0, false
	}
}

// structFind returns the index of a named member, or found=false when absent.
func structFind(name string, names []string) (int, bool) {
	for i, nm := range names {
		if nm == name {
			return i, true
		}
	}
	return 0, false
}

// structPos resolves a possibly-negative position, or found=false when out of range.
func structPos(i, size int) (int, bool) {
	idx := i
	if idx < 0 {
		idx += size
	}
	if idx < 0 || idx >= size {
		return 0, false
	}
	return idx, true
}

// structEqual implements Struct#== (eql=false) and Struct#eql? (eql=true): true
// when other is a Struct of the same class whose members are correspondingly
// == (resp. eql?). Struct-valued members recurse (with a pair cycle guard so
// mutually-recursive structs compare as equal rather than looping); other member
// types compare through valueEqual / valueEql.
func (vm *VM) structEqual(a, b object.Value, eql bool, seen map[[2]*RObject]bool) bool {
	if a == b {
		return true
	}
	oa, ok1 := a.(*RObject)
	ob, ok2 := b.(*RObject)
	if !ok1 || !ok2 || oa.class != ob.class {
		return false
	}
	// oa is the #== / #eql? receiver, always a Struct or Data instance, so its
	// class always carries a member layout (structDef or dataDef).
	names, _ := memberDefNames(oa.class)
	pair := [2]*RObject{oa, ob}
	if seen[pair] {
		return true
	}
	if seen == nil {
		seen = map[[2]*RObject]bool{}
	}
	seen[pair] = true
	defer delete(seen, pair)
	for i := range names {
		av, bv := oa.structVals[i], ob.structVals[i]
		if sa, ok := av.(*RObject); ok && hasMemberDef(sa.class) {
			if sb, ok := bv.(*RObject); ok && hasMemberDef(sb.class) {
				if !vm.structEqual(av, bv, eql, seen) {
					return false
				}
				continue
			}
		}
		if eql {
			if !valueEql(av, bv) {
				return false
			}
		} else if !valueEqual(av, bv) {
			return false
		}
	}
	return true
}

// structHash combines the struct class identity with each member's hash. A cycle
// guard contributes a fixed sentinel for a re-entered struct so a self- or
// mutually-recursive struct hashes without looping.
func (vm *VM) structHash(o *RObject, seen map[*RObject]bool) int64 {
	if seen[o] {
		return 0x27d4eb2f
	}
	if seen == nil {
		seen = map[*RObject]bool{}
	}
	seen[o] = true
	defer delete(seen, o)
	h := int64(reflect.ValueOf(o.class).Pointer())
	for _, v := range o.structVals {
		var hv int64
		if so, ok := v.(*RObject); ok && hasMemberDef(so.class) {
			hv = vm.structHash(so, seen)
		} else {
			hv = vm.hashValue(v)
		}
		h = h*31 + hv
	}
	return h
}

// validConstName reports whether s is a valid (ASCII) Ruby constant name: an
// uppercase letter followed by word characters.
func validConstName(s string) bool {
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c != '_' && (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// warnRedefineConst emits MRI's "already initialized constant" warning to
// $stderr when Struct.new(name, …) overwrites an existing constant.
func (vm *VM) warnRedefineConst(scope *RClass, name string) {
	vm.send(vm.main, "warn", []object.Value{object.NewString("warning: already initialized constant " + scopedNameFor(scope, name))}, nil)
}
