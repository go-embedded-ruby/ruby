package vm

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// bootstrap builds the base class hierarchy and installs the Phase 1 kernel.
// Kernel methods live on Object so every value answers them.
func (vm *VM) bootstrap() {
	vm.cBasicObject = newClass("BasicObject", nil)
	vm.cObject = newClass("Object", vm.cBasicObject)
	vm.cModule = newClass("Module", vm.cObject)
	vm.cClass = newClass("Class", vm.cModule)
	vm.cInteger = newClass("Integer", vm.cObject)
	vm.cFloat = newClass("Float", vm.cObject)
	vm.cString = newClass("String", vm.cObject)
	vm.cSymbol = newClass("Symbol", vm.cObject)
	vm.cArray = newClass("Array", vm.cObject)
	vm.cHash = newClass("Hash", vm.cObject)
	vm.cRange = newClass("Range", vm.cObject)
	vm.cTrueClass = newClass("TrueClass", vm.cObject)
	vm.cFalseClass = newClass("FalseClass", vm.cObject)
	vm.cNilClass = newClass("NilClass", vm.cObject)

	for _, c := range []*RClass{
		vm.cBasicObject, vm.cObject, vm.cModule, vm.cClass, vm.cInteger,
		vm.cFloat, vm.cString, vm.cSymbol, vm.cArray, vm.cHash, vm.cRange,
		vm.cTrueClass, vm.cFalseClass, vm.cNilClass,
	} {
		vm.consts[c.name] = c
	}

	// Exception hierarchy. Each is registered as a constant so it can be raised
	// and matched by rescue (rescue lands in a later increment).
	exc := func(name, super string) *RClass {
		c := newClass(name, vm.consts[super].(*RClass))
		vm.consts[name] = c
		return c
	}
	cException := newClass("Exception", vm.cObject)
	vm.consts["Exception"] = cException
	vm.cException = cException
	exc("StandardError", "Exception")
	exc("RuntimeError", "StandardError")
	exc("ArgumentError", "StandardError")
	exc("TypeError", "StandardError")
	exc("NameError", "StandardError")
	exc("NoMethodError", "NameError")
	exc("ZeroDivisionError", "StandardError")
	exc("RangeError", "StandardError")
	exc("IndexError", "StandardError")
	exc("KeyError", "IndexError")
	exc("StopIteration", "IndexError")
	exc("LocalJumpError", "StandardError")
	exc("NotImplementedError", "StandardError")
	exc("FrozenError", "RuntimeError")
	exc("IOError", "StandardError")

	// Exception instance protocol: initialize stores @message; message/to_s
	// return it (or the class name when unset).
	cException.define("initialize", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			self.(*RObject).ivars["@message"] = object.String(args[0].ToS())
		}
		return object.NilV
	})
	excMessage := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if m := getIvar(self, "@message"); m != object.NilV {
			return m
		}
		return object.String(vm.classOf(self).name)
	}
	cException.define("message", excMessage)
	cException.define("to_s", excMessage)

	// Kernel (on Object).
	vm.cObject.define("puts", nativePuts)
	vm.cObject.define("print", nativePrint)
	vm.cObject.define("p", nativeP)
	vm.cObject.define("class", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.classOf(self)
	})
	vm.cObject.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(self.ToS())
	})
	vm.cObject.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(self.Inspect())
	})
	vm.cObject.define("nil?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		_, isNil := self.(object.Nil)
		return object.Bool(isNil)
	})
	vm.cObject.define("initialize", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NilV
	})
	vm.cObject.define("method_missing", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := args[0].ToS()
		return raise("NoMethodError", "undefined method '%s' for %s", name, vm.classOf(self).name)
	})
	isAFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(classIsA(vm.classOf(self), classArg(args[0])))
	}
	vm.cObject.define("is_a?", isAFn)
	vm.cObject.define("kind_of?", isAFn)
	vm.cObject.define("instance_of?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.classOf(self) == classArg(args[0]))
	})
	vm.cObject.define("raise", nativeRaise)
	// Default equality: object identity for instances, structural for value
	// types (Comparable#== and user-defined == override this via dispatch).
	vm.cObject.define("==", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(rubyEqual(self, args[0]))
	})
	// Default <=>: 0 when equal (by ==), nil otherwise — the MRI Object#<=>.
	vm.cObject.define("<=>", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if vm.send(self, "==", []object.Value{args[0]}, nil).Truthy() {
			return object.Integer(0)
		}
		return object.NilV
	})

	// Module (Class inherits these).
	vm.cModule.define("include", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		target := self.(*RClass)
		for _, a := range args {
			target.includes = append(target.includes, a.(*RClass))
		}
		return target
	})

	// Symbol.
	vm.cSymbol.define("to_sym", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	// Spaceship (<=>) for the built-in ordered types; numerics compare across
	// Integer/Float, strings lexically, and a mismatched type yields nil.
	vm.cInteger.define("<=>", spaceshipNumeric)
	vm.cFloat.define("<=>", spaceshipNumeric)
	vm.cString.define("<=>", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(object.String)
		b, ok := args[0].(object.String)
		if !ok {
			return object.NilV
		}
		return object.Integer(strings.Compare(string(a), string(b)))
	})

	// String. A read-only slice of methods over the immutable Phase 2 String
	// (byte-based; length/chars/index are rune-aware for UTF-8). Mutating forms
	// (<<, gsub!, …) await the mutable byte+encoding representation.
	strOf := func(self object.Value) string { return string(self.(object.String)) }
	vm.cString.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(utf8.RuneCountInString(strOf(self)))
	})
	vm.cString.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(utf8.RuneCountInString(strOf(self)))
	})
	vm.cString.define("bytesize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(len(strOf(self)))
	})
	vm.cString.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(len(strOf(self)) == 0)
	})
	vm.cString.define("upcase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(strings.ToUpper(strOf(self)))
	})
	vm.cString.define("downcase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(strings.ToLower(strOf(self)))
	})
	vm.cString.define("capitalize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(capitalizeStr(strOf(self)))
	})
	vm.cString.define("swapcase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(swapcaseStr(strOf(self)))
	})
	vm.cString.define("reverse", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(reverseStr(strOf(self)))
	})
	vm.cString.define("strip", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(strings.Trim(strOf(self), wsCutset))
	})
	vm.cString.define("lstrip", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(strings.TrimLeft(strOf(self), wsCutset))
	})
	vm.cString.define("rstrip", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(strings.TrimRight(strOf(self), wsCutset))
	})
	vm.cString.define("chomp", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(chompStr(strOf(self)))
	})
	vm.cString.define("chop", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(chopStr(strOf(self)))
	})
	vm.cString.define("chars", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		var out []object.Value
		for _, r := range strOf(self) {
			out = append(out, object.String(string(r)))
		}
		return &object.Array{Elems: out}
	})
	vm.cString.define("bytes", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := strOf(self)
		out := make([]object.Value, len(s))
		for i := 0; i < len(s); i++ {
			out[i] = object.Integer(s[i])
		}
		return &object.Array{Elems: out}
	})
	vm.cString.define("split", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var parts []string
		if len(args) == 0 {
			parts = strings.Fields(strOf(self)) // runs of whitespace, no empties
		} else {
			parts = strings.Split(strOf(self), strArg(args[0]))
		}
		out := make([]object.Value, len(parts))
		for i, p := range parts {
			out[i] = object.String(p)
		}
		return &object.Array{Elems: out}
	})
	vm.cString.define("include?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(strings.Contains(strOf(self), strArg(args[0])))
	})
	vm.cString.define("start_with?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(strings.HasPrefix(strOf(self), strArg(args[0])))
	})
	vm.cString.define("end_with?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(strings.HasSuffix(strOf(self), strArg(args[0])))
	})
	vm.cString.define("index", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		byteIdx := strings.Index(strOf(self), strArg(args[0]))
		if byteIdx < 0 {
			return object.NilV
		}
		return object.Integer(utf8.RuneCountInString(strOf(self)[:byteIdx]))
	})
	vm.cString.define("sub", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.String(strings.Replace(strOf(self), strArg(args[0]), strArg(args[1]), 1))
	})
	vm.cString.define("gsub", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.String(strings.ReplaceAll(strOf(self), strArg(args[0]), strArg(args[1])))
	})
	vm.cString.define("to_i", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(parseLeadingInt(strOf(self)))
	})
	vm.cString.define("to_f", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(parseLeadingFloat(strOf(self)))
	})
	vm.cString.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cString.define("to_str", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cString.define("to_sym", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(strOf(self))
	})
	vm.cString.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return stringIndex(strOf(self), args)
	})

	// Array.
	vm.cArray.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(len(self.(*object.Array).Elems))
	})
	vm.cArray.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(len(self.(*object.Array).Elems))
	})
	vm.cArray.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(len(self.(*object.Array).Elems) == 0)
	})
	vm.cArray.define("first", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		if len(a.Elems) == 0 {
			return object.NilV
		}
		return a.Elems[0]
	})
	vm.cArray.define("last", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		if len(a.Elems) == 0 {
			return object.NilV
		}
		return a.Elems[len(a.Elems)-1]
	})
	vm.cArray.define("push", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		a.Elems = append(a.Elems, args...)
		return a
	})
	vm.cArray.define("<<", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		a.Elems = append(a.Elems, args[0])
		return a
	})
	vm.cArray.define("include?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		for _, e := range self.(*object.Array).Elems {
			if valueEqual(e, args[0]) {
				return object.True
			}
		}
		return object.False
	})
	vm.cArray.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		if i, ok := arrayIndex(a, intArg(args[0])); ok {
			return a.Elems[i]
		}
		return object.NilV
	})
	vm.cArray.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		i, n := intArg(args[0]), int64(len(a.Elems))
		if i < 0 {
			i += n
		}
		if i < 0 || i > n {
			raise("IndexError", "index %d out of array", intArg(args[0]))
		}
		if i == n {
			a.Elems = append(a.Elems, args[1])
		} else {
			a.Elems[i] = args[1]
		}
		return args[1]
	})
	vm.cArray.define("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		a := self.(*object.Array)
		for _, e := range a.Elems {
			vm.callBlock(blk, []object.Value{e})
		}
		return a
	})
	vm.cArray.define("map", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (map)")
		}
		a := self.(*object.Array)
		out := make([]object.Value, len(a.Elems))
		for i, e := range a.Elems {
			out[i] = vm.callBlock(blk, []object.Value{e})
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("reverse", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		out := make([]object.Value, len(a.Elems))
		for i, e := range a.Elems {
			out[len(a.Elems)-1-i] = e
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("uniq", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		var out []object.Value
		for _, e := range self.(*object.Array).Elems {
			dup := false
			for _, k := range out {
				if valueEqual(e, k) {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, e)
			}
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("compact", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		var out []object.Value
		for _, e := range self.(*object.Array).Elems {
			if _, isNil := e.(object.Nil); !isNil {
				out = append(out, e)
			}
		}
		return &object.Array{Elems: out}
	})
	vm.cArray.define("flatten", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Array{Elems: flattenArray(self.(*object.Array))}
	})
	vm.cArray.define("join", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		sep := ""
		if len(args) > 0 {
			sep = strArg(args[0])
		}
		return object.String(joinArray(self.(*object.Array), sep))
	})
	vm.cArray.define("index", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		for i, e := range self.(*object.Array).Elems {
			if valueEqual(e, args[0]) {
				return object.Integer(i)
			}
		}
		return object.NilV
	})
	vm.cArray.define("take", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		n := int(intArg(args[0]))
		if n < 0 {
			raise("ArgumentError", "attempt to take negative size")
		}
		if n > len(a.Elems) {
			n = len(a.Elems)
		}
		out := make([]object.Value, n)
		copy(out, a.Elems[:n])
		return &object.Array{Elems: out}
	})
	vm.cArray.define("drop", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		n := int(intArg(args[0]))
		if n < 0 {
			raise("ArgumentError", "attempt to drop negative size")
		}
		if n > len(a.Elems) {
			n = len(a.Elems)
		}
		out := make([]object.Value, len(a.Elems)-n)
		copy(out, a.Elems[n:])
		return &object.Array{Elems: out}
	})
	vm.cArray.define("sort", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		out := make([]object.Value, len(a.Elems))
		copy(out, a.Elems)
		sort.SliceStable(out, func(i, j int) bool { return vm.spaceship(out[i], out[j]) < 0 })
		return &object.Array{Elems: out}
	})
	vm.cArray.define("sort_by", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (sort_by)")
		}
		a := self.(*object.Array)
		out := make([]object.Value, len(a.Elems))
		copy(out, a.Elems)
		keys := make([]object.Value, len(out))
		for i, e := range out {
			keys[i] = vm.callBlock(blk, []object.Value{e})
		}
		sort.SliceStable(out, func(i, j int) bool { return vm.spaceship(keys[i], keys[j]) < 0 })
		return &object.Array{Elems: out}
	})
	vm.cArray.define("min_by", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		return vm.arrayByExtreme(self.(*object.Array), blk, "min_by", -1)
	})
	vm.cArray.define("max_by", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		return vm.arrayByExtreme(self.(*object.Array), blk, "max_by", 1)
	})
	vm.cArray.define("each_with_object", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each_with_object)")
		}
		memo := args[0]
		for _, e := range self.(*object.Array).Elems {
			vm.callBlock(blk, []object.Value{e, memo})
		}
		return memo
	})

	// Hash.
	vm.cHash.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if v, ok := self.(*object.Hash).Get(args[0]); ok {
			return v
		}
		return object.NilV
	})
	vm.cHash.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*object.Hash).Set(args[0], args[1])
		return args[1]
	})
	vm.cHash.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self.(*object.Hash).Len())
	})
	vm.cHash.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self.(*object.Hash).Len())
	})
	vm.cHash.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*object.Hash).Len() == 0)
	})
	hashKeyP := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := self.(*object.Hash).Get(args[0])
		return object.Bool(ok)
	}
	vm.cHash.define("key?", hashKeyP)
	vm.cHash.define("has_key?", hashKeyP)
	vm.cHash.define("include?", hashKeyP)
	vm.cHash.define("keys", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		ks := make([]object.Value, len(h.Keys))
		copy(ks, h.Keys)
		return &object.Array{Elems: ks}
	})
	vm.cHash.define("values", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vs := make([]object.Value, 0, len(h.Keys))
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			vs = append(vs, v)
		}
		return &object.Array{Elems: vs}
	})
	vm.cHash.define("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		h := self.(*object.Hash)
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			vm.callBlock(blk, []object.Value{hashPair(k, v)})
		}
		return h
	})
	// select/reject return a Hash (unlike Enumerable's Array forms), so they are
	// native rather than inherited.
	vm.cHash.define("select", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (select)")
		}
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			if vm.callBlock(blk, []object.Value{hashPair(k, v)}).Truthy() {
				out.Set(k, v)
			}
		}
		return out
	})
	vm.cHash.define("reject", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (reject)")
		}
		h := self.(*object.Hash)
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			if !vm.callBlock(blk, []object.Value{hashPair(k, v)}).Truthy() {
				out.Set(k, v)
			}
		}
		return out
	})

	// Range.
	vm.cRange.define("begin", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*object.Range).Lo
	})
	vm.cRange.define("first", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*object.Range).Lo
	})
	vm.cRange.define("end", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*object.Range).Hi
	})
	vm.cRange.define("last", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*object.Range).Hi
	})
	vm.cRange.define("exclude_end?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*object.Range).Exclusive)
	})
	rangeCover := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		v := args[0]
		// cover? is comparison-based: an incomparable member is simply not
		// covered (Ruby returns false rather than raising).
		lc, lok := rangeCmp(v, r.Lo)
		hc, hok := rangeCmp(v, r.Hi)
		if !lok || !hok || lc < 0 {
			return object.False
		}
		if r.Exclusive {
			return object.Bool(hc < 0)
		}
		return object.Bool(hc <= 0)
	}
	vm.cRange.define("include?", rangeCover)
	vm.cRange.define("cover?", rangeCover)
	vm.cRange.define("member?", rangeCover)
	vm.cRange.define("min", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		lo, _, _ := rangeInts(r)
		if rangeSize(r) == 0 {
			return object.NilV
		}
		return object.Integer(lo)
	})
	vm.cRange.define("max", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		_, hi, _ := rangeInts(r)
		if rangeSize(r) == 0 {
			return object.NilV
		}
		if r.Exclusive {
			return object.Integer(hi - 1)
		}
		return object.Integer(hi)
	})
	rangeSizeFn := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(rangeSize(self.(*object.Range)))
	}
	vm.cRange.define("size", rangeSizeFn)
	vm.cRange.define("count", rangeSizeFn)
	vm.cRange.define("to_a", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Array{Elems: rangeElems(self.(*object.Range))}
	})
	vm.cRange.define("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		r := self.(*object.Range)
		for _, e := range rangeElems(r) {
			vm.callBlock(blk, []object.Value{e})
		}
		return r
	})
	vm.cRange.define("map", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (map)")
		}
		elems := rangeElems(self.(*object.Range))
		out := make([]object.Value, len(elems))
		for i, e := range elems {
			out[i] = vm.callBlock(blk, []object.Value{e})
		}
		return &object.Array{Elems: out}
	})

	// Integer methods.
	intOf := func(self object.Value) int64 { return int64(self.(object.Integer)) }
	vm.cInteger.define("abs", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(absInt(intOf(self)))
	})
	vm.cInteger.define("even?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(intOf(self)%2 == 0)
	})
	vm.cInteger.define("odd?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(intOf(self)%2 != 0)
	})
	vm.cInteger.define("zero?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(intOf(self) == 0)
	})
	vm.cInteger.define("positive?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(intOf(self) > 0)
	})
	vm.cInteger.define("negative?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(intOf(self) < 0)
	})
	intSucc := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(intOf(self) + 1)
	}
	vm.cInteger.define("succ", intSucc)
	vm.cInteger.define("next", intSucc)
	vm.cInteger.define("pred", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(intOf(self) - 1)
	})
	vm.cInteger.define("to_i", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cInteger.define("to_int", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cInteger.define("to_f", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(float64(intOf(self)))
	})
	vm.cInteger.define("to_s", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		base := int64(10)
		if len(args) > 0 {
			base = intArg(args[0])
		}
		if base < 2 || base > 36 {
			raise("ArgumentError", "invalid radix %d", base)
		}
		return object.String(strconv.FormatInt(intOf(self), int(base)))
	})
	vm.cInteger.define("gcd", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := absInt(intOf(self)), absInt(intArg(args[0]))
		for b != 0 {
			a, b = b, a%b
		}
		return object.Integer(a)
	})
	vm.cInteger.define("divmod", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := intOf(self), intArg(args[0])
		if b == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		return &object.Array{Elems: []object.Value{object.Integer(floorDiv(a, b)), object.Integer(floorMod(a, b))}}
	})
	vm.cInteger.define("digits", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		n := intOf(self)
		if n < 0 {
			raise("Math::DomainError", "out of domain")
		}
		if n == 0 {
			return &object.Array{Elems: []object.Value{object.Integer(0)}}
		}
		var out []object.Value
		for n > 0 {
			out = append(out, object.Integer(n%10))
			n /= 10
		}
		return &object.Array{Elems: out}
	})
	vm.cInteger.define("chr", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		n := intOf(self)
		if n < 0 || n > 255 {
			raise("RangeError", "%d out of char range", n)
		}
		return object.String(string([]byte{byte(n)}))
	})
	vm.cInteger.define("upto", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (upto)")
		}
		for i := intOf(self); i <= intArg(args[0]); i++ {
			vm.callBlock(blk, []object.Value{object.Integer(i)})
		}
		return self
	})
	vm.cInteger.define("downto", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (downto)")
		}
		for i := intOf(self); i >= intArg(args[0]); i-- {
			vm.callBlock(blk, []object.Value{object.Integer(i)})
		}
		return self
	})

	// Float methods.
	floatOf := func(self object.Value) float64 { return float64(self.(object.Float)) }
	vm.cFloat.define("abs", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(math.Abs(floatOf(self)))
	})
	vm.cFloat.define("zero?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(floatOf(self) == 0)
	})
	vm.cFloat.define("positive?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(floatOf(self) > 0)
	})
	vm.cFloat.define("negative?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(floatOf(self) < 0)
	})
	vm.cFloat.define("to_f", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cFloat.define("to_i", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(floatOf(self)))
	})
	vm.cFloat.define("to_int", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(floatOf(self)))
	})
	vm.cFloat.define("ceil", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(math.Ceil(floatOf(self))))
	})
	vm.cFloat.define("floor", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(math.Floor(floatOf(self))))
	})
	vm.cFloat.define("round", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(math.Round(floatOf(self))))
	})
	vm.cFloat.define("nan?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(math.IsNaN(floatOf(self)))
	})
	vm.cFloat.define("finite?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		f := floatOf(self)
		return object.Bool(!math.IsInf(f, 0) && !math.IsNaN(f))
	})
	vm.cFloat.define("infinite?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		f := floatOf(self)
		if math.IsInf(f, 1) {
			return object.Integer(1)
		}
		if math.IsInf(f, -1) {
			return object.Integer(-1)
		}
		return object.NilV
	})

	// Class.
	vm.cClass.define("new", nativeNew)

	// Integer#times — the first block-driven iterator.
	vm.cInteger.define("times", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (times)")
		}
		n := int64(self.(object.Integer))
		for i := int64(0); i < n; i++ {
			vm.callBlock(blk, []object.Value{object.Integer(i)})
		}
		return self
	})
}

// nativeNew allocates an instance of the receiver class and runs initialize,
// forwarding any block.
func nativeNew(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
	class := self.(*RClass)
	obj := &RObject{class: class, ivars: map[string]object.Value{}}
	vm.send(obj, "initialize", args, blk)
	return obj
}

// intArg coerces an argument used as an array index to int64, or raises.
func intArg(v object.Value) int64 {
	if i, ok := v.(object.Integer); ok {
		return int64(i)
	}
	raise("TypeError", "no implicit conversion of %s into Integer", v.Inspect())
	return 0
}

// arrayIndex normalizes a (possibly negative) index and reports whether it is in
// range.
func arrayIndex(a *object.Array, i int64) (int, bool) {
	n := int64(len(a.Elems))
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return 0, false
	}
	return int(i), true
}

func nativePuts(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	if len(args) == 0 {
		fmt.Fprintln(vm.out)
		return object.NilV
	}
	for _, a := range args {
		putsValue(vm, a)
	}
	return object.NilV
}

// putsValue prints one value the way Kernel#puts does: arrays are flattened (an
// empty array prints nothing), everything else prints its to_s plus a newline.
func putsValue(vm *VM, v object.Value) {
	if arr, ok := v.(*object.Array); ok {
		for _, e := range arr.Elems {
			putsValue(vm, e)
		}
		return
	}
	fmt.Fprintln(vm.out, v.ToS())
}

func nativePrint(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	for _, a := range args {
		fmt.Fprint(vm.out, a.ToS())
	}
	return object.NilV
}

func nativeP(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	for _, a := range args {
		fmt.Fprintln(vm.out, a.Inspect())
	}
	switch len(args) {
	case 0:
		return object.NilV
	case 1:
		return args[0]
	default:
		return object.NilV // Ruby returns the args array; arrays arrive in Phase 2
	}
}


// wsCutset is the whitespace stripped by String#strip and friends, matching
// Ruby (space, tab, newline, carriage return, form feed, vertical tab, NUL).
const wsCutset = " \t\n\r\f\v\x00"

func capitalizeStr(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(strings.ToLower(s))
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func swapcaseStr(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			out = append(out, unicode.ToLower(r))
		case unicode.IsLower(r):
			out = append(out, unicode.ToUpper(r))
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

func reverseStr(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// chompStr removes one trailing line ending (\r\n, \n, or \r), as in Ruby.
func chompStr(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return s[:len(s)-2]
	}
	if strings.HasSuffix(s, "\n") || strings.HasSuffix(s, "\r") {
		return s[:len(s)-1]
	}
	return s
}

// chopStr removes the last character (\r\n counts as one), as in Ruby.
func chopStr(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return s[:len(s)-2]
	}
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[:len(r)-1])
}

// parseLeadingInt mimics String#to_i: optional whitespace and sign, then digits;
// 0 when there is no leading integer.
func parseLeadingInt(s string) int64 {
	s = strings.TrimLeft(s, wsCutset)
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i {
		return 0
	}
	n, err := strconv.ParseInt(s[:j], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// parseLeadingFloat mimics String#to_f: parse the longest leading float, 0.0 if
// none.
func parseLeadingFloat(s string) float64 {
	s = strings.TrimLeft(s, wsCutset)
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j {
			i = k
		}
	}
	f, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	return f
}

// stringIndex implements String#[]: s[i], s[i, len], and s[range], all
// rune-indexed, returning nil for an out-of-range start.
func stringIndex(s string, args []object.Value) object.Value {
	r := []rune(s)
	n := len(r)
	if len(args) == 2 {
		start := normIndex(intArg(args[0]), n)
		length := intArg(args[1])
		if start < 0 || start > n || length < 0 {
			return object.NilV
		}
		end := start + int(length)
		if end > n {
			end = n
		}
		return object.String(string(r[start:end]))
	}
	if rng, ok := args[0].(*object.Range); ok {
		lo := normIndex(intArg(rng.Lo), n)
		if lo < 0 || lo > n {
			return object.NilV
		}
		hi := normIndex(intArg(rng.Hi), n)
		if !rng.Exclusive {
			hi++
		}
		if hi > n {
			hi = n
		}
		if hi < lo {
			hi = lo
		}
		return object.String(string(r[lo:hi]))
	}
	i := normIndex(intArg(args[0]), n)
	if i < 0 || i >= n {
		return object.NilV
	}
	return object.String(string(r[i]))
}

// normIndex resolves a possibly-negative index against length n (no clamping of
// the upper bound; callers range-check).
func normIndex(i int64, n int) int {
	if i < 0 {
		return int(i) + n
	}
	return int(i)
}

// strArg coerces a String argument, raising TypeError otherwise.
func strArg(v object.Value) string {
	if s, ok := v.(object.String); ok {
		return string(s)
	}
	raise("TypeError", "no implicit conversion of %s into String", v.Inspect())
	return ""
}

// hashPair builds the [key, value] array Hash#each yields; block auto-splat then
// binds a two-parameter block element-wise, while a one-parameter block sees the
// pair (matching Ruby).
func hashPair(k, v object.Value) *object.Array {
	return &object.Array{Elems: []object.Value{k, v}}
}

// spaceship compares two values via their <=> method, raising ArgumentError if
// they are incomparable (a nil result).
func (vm *VM) spaceship(a, b object.Value) int {
	r := vm.send(a, "<=>", []object.Value{b}, nil)
	n, ok := r.(object.Integer)
	if !ok {
		raise("ArgumentError", "comparison of %s with %s failed", vm.classOf(a).name, vm.classOf(b).name)
	}
	return int(n)
}

// arrayByExtreme implements min_by/max_by: the element whose block key is
// smallest (want=-1) or largest (want=1). nil for an empty array.
func (vm *VM) arrayByExtreme(a *object.Array, blk *Proc, name string, want int) object.Value {
	if blk == nil {
		raise("LocalJumpError", "no block given (%s)", name)
	}
	if len(a.Elems) == 0 {
		return object.NilV
	}
	best := a.Elems[0]
	bestKey := vm.callBlock(blk, []object.Value{best})
	for _, e := range a.Elems[1:] {
		k := vm.callBlock(blk, []object.Value{e})
		if sign(vm.spaceship(k, bestKey)) == want {
			best, bestKey = e, k
		}
	}
	return best
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

// flattenArray recursively flattens nested arrays into one slice.
func flattenArray(a *object.Array) []object.Value {
	var out []object.Value
	for _, e := range a.Elems {
		if sub, ok := e.(*object.Array); ok {
			out = append(out, flattenArray(sub)...)
		} else {
			out = append(out, e)
		}
	}
	return out
}

// joinArray renders an array as a string, recursively joining nested arrays.
func joinArray(a *object.Array, sep string) string {
	var b strings.Builder
	for i, e := range a.Elems {
		if i > 0 {
			b.WriteString(sep)
		}
		if sub, ok := e.(*object.Array); ok {
			b.WriteString(joinArray(sub, sep))
		} else {
			b.WriteString(e.ToS())
		}
	}
	return b.String()
}

// classArg coerces an argument expected to be a class/module, else TypeError.
func classArg(v object.Value) *RClass {
	if c, ok := v.(*RClass); ok {
		return c
	}
	raise("TypeError", "class or module required")
	return nil
}

// classIsA reports whether class c is, inherits from, or includes target.
func classIsA(c, target *RClass) bool {
	for ; c != nil; c = c.super {
		if c == target {
			return true
		}
		for _, m := range c.includes {
			if classIsA(m, target) {
				return true
			}
		}
	}
	return false
}

// excError builds the RubyError carrying a raised Ruby exception object.
func (vm *VM) excError(exc object.Value) RubyError {
	cls := vm.classOf(exc)
	msg := cls.name
	if m := getIvar(exc, "@message"); m != object.NilV {
		msg = m.ToS()
	}
	return RubyError{Class: cls.name, Message: msg, Obj: exc}
}

// nativeRaise implements Kernel#raise: a message string (RuntimeError), an
// exception class (instantiated), an exception instance (re-raised), or a
// class + message pair.
func nativeRaise(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	switch len(args) {
	case 0:
		panic(vm.excError(vm.send(vm.consts["RuntimeError"].(*RClass), "new",
			[]object.Value{object.String("unhandled exception")}, nil)))
	case 1:
		switch a := args[0].(type) {
		case object.String:
			panic(vm.excError(vm.send(vm.consts["RuntimeError"].(*RClass), "new", []object.Value{a}, nil)))
		case *RClass:
			panic(vm.excError(vm.send(a, "new", nil, nil)))
		case *RObject:
			panic(vm.excError(a))
		}
		raise("TypeError", "exception class/object expected")
		return object.NilV
	default:
		panic(vm.excError(vm.send(classArg(args[0]), "new", []object.Value{args[1]}, nil)))
	}
}

// absInt is the absolute value of an int64.
func absInt(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// rubyEqual is the default Object#== : pointer identity for instances, and
// structural equality for the immutable value types.
func rubyEqual(a, b object.Value) bool {
	if ao, ok := a.(*RObject); ok {
		bo, ok := b.(*RObject)
		return ok && ao == bo
	}
	return valueEqual(a, b)
}

// spaceshipNumeric implements Integer#<=> and Float#<=>: -1/0/1 across the
// numeric tower, nil for a non-numeric argument.
func spaceshipNumeric(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
	a, _ := toFloat(self)
	b, ok := toFloat(args[0])
	if !ok {
		return object.NilV
	}
	switch {
	case a < b:
		return object.Integer(-1)
	case a > b:
		return object.Integer(1)
	default:
		return object.Integer(0)
	}
}

// rangeCmp orders two values for Range membership tests: numerics compare
// numerically, strings lexically; any other pairing is incomparable (ok=false,
// mirroring Ruby's <=> returning nil).
func rangeCmp(a, b object.Value) (ord int, ok bool) {
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			switch {
			case af < bf:
				return -1, true
			case af > bf:
				return 1, true
			default:
				return 0, true
			}
		}
		return 0, false
	}
	as, aok := a.(object.String)
	bs, bok := b.(object.String)
	if aok && bok {
		return strings.Compare(string(as), string(bs)), true
	}
	return 0, false
}

// rangeInts extracts integer endpoints. ok is false when either endpoint is not
// an Integer (string/float ranges are not iterable in this phase).
func rangeInts(r *object.Range) (lo, hi int64, ok bool) {
	li, lok := r.Lo.(object.Integer)
	hi2, hok := r.Hi.(object.Integer)
	if !lok || !hok {
		return 0, 0, false
	}
	return int64(li), int64(hi2), true
}

// rangeSize is the element count of an integer range (0 if empty or
// non-integer), matching Ruby's Range#size.
func rangeSize(r *object.Range) int64 {
	lo, hi, ok := rangeInts(r)
	if !ok {
		raise("TypeError", "can't iterate from %s", r.Lo.Inspect())
	}
	n := hi - lo
	if !r.Exclusive {
		n++
	}
	if n < 0 {
		return 0
	}
	return n
}

// rangeElems materializes an integer range to a slice, raising TypeError on
// non-integer endpoints (Ruby: "can't iterate from String").
func rangeElems(r *object.Range) []object.Value {
	lo, hi, ok := rangeInts(r)
	if !ok {
		raise("TypeError", "can't iterate from %s", r.Lo.Inspect())
	}
	if r.Exclusive {
		hi--
	}
	if hi < lo {
		return nil
	}
	out := make([]object.Value, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		out = append(out, object.Integer(i))
	}
	return out
}
