// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file implements ObjectSpace::WeakMap and ObjectSpace::WeakKeyMap. rbgo
// runs on Go's garbage collector, which offers no reliable "value became
// unreachable" hook usable per-entry, so both maps hold their entries strongly:
// the weak-reclamation aspect (entries silently vanishing once the key/value is
// collected) is not reproduced. Every other observable behaviour — identity vs.
// equality key semantics, the accessor/iteration API, argument validation and
// the inspect forms — matches MRI, which is what the ruby/spec suite checks
// (it explicitly cannot spec reclamation). This is the documented limitation.

// wmPair is one WeakMap entry (identity-keyed).
type wmPair struct{ k, v object.Value }

// WeakMapObj is an ObjectSpace::WeakMap instance: an identity-keyed association
// preserving insertion order.
type WeakMapObj struct {
	cls   *RClass
	pairs []wmPair
}

func (m *WeakMapObj) ToS() string     { return "#<ObjectSpace::WeakMap>" }
func (m *WeakMapObj) Inspect() string { return m.ToS() }
func (m *WeakMapObj) Truthy() bool    { return true }

// wkmEntry is one WeakKeyMap entry (equality-keyed); h caches the key's #hash.
type wkmEntry struct {
	k, v object.Value
	h    int64
}

// WeakKeyMapObj is an ObjectSpace::WeakKeyMap instance (3.3+): keys are compared
// by #hash + #eql?, only garbage-collectable (reference) objects may be keys, and
// keys are stored as given (never duped/frozen).
type WeakKeyMapObj struct {
	cls     *RClass
	entries []wkmEntry
}

func (m *WeakKeyMapObj) ToS() string     { return "#<ObjectSpace::WeakKeyMap>" }
func (m *WeakKeyMapObj) Inspect() string { return m.ToS() }
func (m *WeakKeyMapObj) Truthy() bool    { return true }

// weakElemInspect renders one WeakMap element as MRI's address form
// #<ClassName:0x...>, computed without sending #inspect so it works for a
// BasicObject element (which has no #inspect).
func (vm *VM) weakElemInspect(v object.Value) string {
	return fmt.Sprintf("#<%s:0x%016x>", vm.classOf(v).name, uint64(vm.refID(v)))
}

// wmIndex returns the index of key in the identity-keyed map, or -1.
func (m *WeakMapObj) wmIndex(key object.Value) int {
	for i, p := range m.pairs {
		if p.k == key {
			return i
		}
	}
	return -1
}

// registerWeakMaps installs ObjectSpace::WeakMap and ObjectSpace::WeakKeyMap on
// the ObjectSpace module.
func (vm *VM) registerWeakMaps(mod *RClass) {
	vm.registerWeakMap(mod)
	vm.registerWeakKeyMap(mod)
}

// includeWeakMapEnumerable mixes Enumerable into ObjectSpace::WeakMap. Enumerable
// is defined by the prelude, which loads after the Go builtins, so this runs from
// the post-prelude registration phase (see New).
func (vm *VM) includeWeakMapEnumerable() {
	en := vm.consts["Enumerable"].(*RClass)
	vm.cWeakMap.includes = append(vm.cWeakMap.includes, en)
}

func (vm *VM) registerWeakMap(mod *RClass) {
	cls := newClass("ObjectSpace::WeakMap", vm.cObject)
	vm.cWeakMap = cls
	mod.consts["WeakMap"] = cls
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &WeakMapObj{cls: cls}
	}}

	cls.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := self.(*WeakMapObj)
		key, val := args[0], args[1]
		if i := m.wmIndex(key); i >= 0 {
			m.pairs[i].v = val
		} else {
			m.pairs = append(m.pairs, wmPair{key, val})
		}
		return val
	})
	cls.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := self.(*WeakMapObj)
		if i := m.wmIndex(args[0]); i >= 0 {
			return m.pairs[i].v
		}
		return object.NilV
	})
	cls.define("include?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*WeakMapObj).wmIndex(args[0]) >= 0)
	})
	aliasBuiltin(cls, "key?", "include?")
	aliasBuiltin(cls, "member?", "include?")
	cls.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(self.(*WeakMapObj).pairs)))
	})
	aliasBuiltin(cls, "length", "size")
	cls.define("delete", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		m := self.(*WeakMapObj)
		if i := m.wmIndex(args[0]); i >= 0 {
			v := m.pairs[i].v
			m.pairs = append(m.pairs[:i], m.pairs[i+1:]...)
			return v
		}
		if blk != nil {
			return vm.callBlock(blk, []object.Value{args[0]})
		}
		return object.NilV
	})
	cls.define("keys", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := self.(*WeakMapObj)
		out := object.NewArray()
		for _, p := range m.pairs {
			out.Elems = append(out.Elems, p.k)
		}
		return out
	})
	cls.define("values", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := self.(*WeakMapObj)
		out := object.NewArray()
		for _, p := range m.pairs {
			out.Elems = append(out.Elems, p.v)
		}
		return out
	})
	// each / each_pair yield [key, value]; each_key yields keys; each_value values.
	// Without a block an empty map returns self, while a non-empty one raises
	// LocalJumpError — WeakMap's iterators do not return an Enumerator.
	eachWith := func(pick func(wmPair) []object.Value) NativeFn {
		return func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
			m := self.(*WeakMapObj)
			if blk == nil {
				if len(m.pairs) == 0 {
					return self
				}
				raise("LocalJumpError", "no block given (yield)")
			}
			for _, p := range m.pairs {
				vm.callBlock(blk, pick(p))
			}
			return self
		}
	}
	cls.define("each", eachWith(func(p wmPair) []object.Value { return []object.Value{p.k, p.v} }))
	aliasBuiltin(cls, "each_pair", "each")
	cls.define("each_key", eachWith(func(p wmPair) []object.Value { return []object.Value{p.k} }))
	cls.define("each_value", eachWith(func(p wmPair) []object.Value { return []object.Value{p.v} }))
	cls.define("inspect", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := self.(*WeakMapObj)
		head := fmt.Sprintf("#<ObjectSpace::WeakMap:0x%016x", uint64(vm.refID(self)))
		if len(m.pairs) == 0 {
			return object.NewString(head + ">")
		}
		parts := make([]string, len(m.pairs))
		for i, p := range m.pairs {
			parts[i] = vm.weakElemInspect(p.k) + " => " + vm.weakElemInspect(p.v)
		}
		return object.NewString(head + ": " + strings.Join(parts, ", ") + ">")
	})
	aliasBuiltin(cls, "to_s", "inspect")
}

// wkmImmediate reports whether key is a non-garbage-collectable immediate, which
// a WeakKeyMap cannot use as a key.
func wkmImmediate(key object.Value) bool {
	switch key.(type) {
	case object.Integer, object.Float, object.Symbol, object.Bool, object.Nil:
		return true
	}
	return false
}

// wkmHash returns key's #hash as an int64, requiring the key to define #hash
// (a BasicObject key raises NoMethodError, as MRI). Visibility is ignored so a
// private #hash is honoured.
func (vm *VM) wkmHash(key object.Value) int64 {
	if vm.findMethod(key, "hash") == nil {
		raise("NoMethodError", "undefined method 'hash' for an instance of %s", vm.classOf(key).name)
	}
	h, ok := hashAsInt(vm.send(key, "hash", nil, nil))
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Integer", vm.classOf(vm.send(key, "hash", nil, nil)).name)
	}
	return h
}

// wkmIndex finds key in the equality-keyed map, returning its index or -1. An
// identical (same-object) key matches without consulting #eql?; otherwise a
// candidate must share the cached #hash and satisfy key.eql?(storedKey).
func (vm *VM) wkmIndex(m *WeakKeyMapObj, key object.Value) int {
	for i, e := range m.entries {
		if e.k == key {
			return i
		}
	}
	kh := vm.wkmHash(key)
	for i, e := range m.entries {
		if e.h == kh && vm.send(key, "eql?", []object.Value{e.k}, nil).Truthy() {
			return i
		}
	}
	return -1
}

func (vm *VM) registerWeakKeyMap(mod *RClass) {
	cls := newClass("ObjectSpace::WeakKeyMap", vm.cObject)
	vm.cWeakKeyMap = cls
	mod.consts["WeakKeyMap"] = cls
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &WeakKeyMapObj{cls: cls}
	}}

	cls.define("[]=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := self.(*WeakKeyMapObj)
		key, val := args[0], args[1]
		if wkmImmediate(key) {
			raise("ArgumentError", "WeakKeyMap keys must be garbage collectable")
		}
		if i := vm.wkmIndex(m, key); i >= 0 {
			m.entries[i].v = val
			return val
		}
		m.entries = append(m.entries, wkmEntry{k: key, v: val, h: vm.wkmHash(key)})
		return val
	})
	cls.define("[]", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := self.(*WeakKeyMapObj)
		if wkmImmediate(args[0]) {
			return object.NilV
		}
		if i := vm.wkmIndex(m, args[0]); i >= 0 {
			return m.entries[i].v
		}
		return object.NilV
	})
	cls.define("getkey", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := self.(*WeakKeyMapObj)
		if wkmImmediate(args[0]) {
			return object.NilV
		}
		if i := vm.wkmIndex(m, args[0]); i >= 0 {
			return m.entries[i].k
		}
		return object.NilV
	})
	cls.define("key?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := self.(*WeakKeyMapObj)
		if wkmImmediate(args[0]) {
			return object.False
		}
		return object.Bool(vm.wkmIndex(m, args[0]) >= 0)
	})
	cls.define("delete", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		m := self.(*WeakKeyMapObj)
		if !wkmImmediate(args[0]) {
			if i := vm.wkmIndex(m, args[0]); i >= 0 {
				v := m.entries[i].v
				m.entries = append(m.entries[:i], m.entries[i+1:]...)
				return v
			}
		}
		if blk != nil {
			return vm.callBlock(blk, []object.Value{args[0]})
		}
		return object.NilV
	})
	cls.define("clear", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := self.(*WeakKeyMapObj)
		m.entries = nil
		return self
	})
	cls.define("inspect", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := self.(*WeakKeyMapObj)
		return object.NewString(fmt.Sprintf("#<ObjectSpace::WeakKeyMap:0x%016x size=%d>", uint64(vm.refID(self)), len(m.entries)))
	})
	aliasBuiltin(cls, "to_s", "inspect")
}
