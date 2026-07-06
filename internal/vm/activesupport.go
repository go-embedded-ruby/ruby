// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"regexp"
	"sort"
	"strings"

	"github.com/go-ruby-activesupport/activesupport/coreext"
	"github.com/go-ruby-activesupport/activesupport/inflector"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerActiveSupport installs the ActiveSupport surface (require
// "active_support" / "active_support/all"): the ActiveSupport::Inflector module
// (pluralize/singularize/camelize/underscore/humanize/titleize/tableize/classify/
// dasherize/demodulize/deconstantize/foreign_key/ordinal/ordinalize/parameterize/
// transliterate/constantize/safe_constantize + an inflections registration DSL)
// and the core extensions added to String/Array/Hash/Integer/Object/Enumerable.
//
// The word-inflection engine and the core-ext helpers live in the pure-Go (CGO=0)
// github.com/go-ruby-activesupport/activesupport library (its inflector and
// coreext packages), which is MRI-byte-faithful to the activesupport gem. This
// file is the thin shell mapping rbgo's String/Symbol/Array/Hash/Integer value
// model onto those helpers and back. The two Ruby-object seams the library
// documents are wired here: coreext.Dispatcher (Object#try's method dispatch)
// and inflector.Resolver (Inflector.constantize's constant lookup). Inflection
// rules are held per-VM (vm.asInflections, a clone of the gem's English defaults)
// so an inflections registration never leaks across interpreters.
//
// Like MRI's own core extensions once active_support is loaded, these methods are
// installed unconditionally at startup; require "active_support" is a provided-
// feature no-op that only reports the first load, matching a normal gem require.
func (vm *VM) registerActiveSupport() {
	if vm.asInflections == nil {
		vm.asInflections = inflector.DefaultLocale.Clone()
	}

	as := newClass("ActiveSupport", nil)
	as.isModule = true
	vm.consts["ActiveSupport"] = as

	vm.registerActiveSupportInflector(as)
	vm.registerActiveSupportCoreExt()
}

// registerActiveSupportInflector installs ActiveSupport::Inflector and its module
// methods, plus the ActiveSupport::Inflector::Inflections registration DSL
// (Inflector.inflections { |inflect| … }).
func (vm *VM) registerActiveSupportInflector(as *RClass) {
	mod := newClass("ActiveSupport::Inflector", nil)
	mod.isModule = true
	as.consts["Inflector"] = mod
	vm.consts["ActiveSupport::Inflector"] = mod

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	sm("pluralize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.Pluralize(strArg(args[0])))
	})
	sm("singularize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.Singularize(strArg(args[0])))
	})
	sm("camelize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		upper := true
		if len(args) > 1 {
			upper = camelUpper(args[1])
		}
		return object.NewString(vm.asInflections.Camelize(strArg(args[0]), upper))
	})
	sm("underscore", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.Underscore(strArg(args[0])))
	})
	sm("humanize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.Humanize(strArg(args[0]), true, false))
	})
	sm("titleize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.Titleize(strArg(args[0]), false))
	})
	sm("tableize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.Tableize(strArg(args[0])))
	})
	sm("classify", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.Classify(strArg(args[0])))
	})
	sm("dasherize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(inflector.Dasherize(strArg(args[0])))
	})
	sm("demodulize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(inflector.Demodulize(strArg(args[0])))
	})
	sm("deconstantize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(inflector.Deconstantize(strArg(args[0])))
	})
	sm("foreign_key", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.ForeignKey(strArg(args[0]), true))
	})
	sm("ordinal", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(inflector.Ordinal(int(intArg(args[0]))))
	})
	sm("ordinalize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(inflector.Ordinalize(int(intArg(args[0]))))
	})
	sm("parameterize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(inflector.Parameterize(strArg(args[0]), "-", false))
	})
	sm("transliterate", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		repl := "?"
		if len(args) > 1 {
			repl = strArg(args[1])
		}
		return object.NewString(inflector.Transliterate(strArg(args[0]), repl))
	})
	sm("constantize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		v, err := inflector.Constantize(strArg(args[0]), vm.asResolver())
		if err != nil {
			raise("NameError", "%s", err.Error())
		}
		return v.(object.Value)
	})
	sm("safe_constantize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if v := inflector.SafeConstantize(strArg(args[0]), vm.asResolver()); v != nil {
			return v.(object.Value)
		}
		return object.NilV
	})

	vm.registerActiveSupportInflections(mod)
}

// asResolver builds the inflector.Resolver seam over rbgo's constant space: it
// resolves a "::"-qualified constant name from the top level, exactly as Ruby's
// Object.const_get does for Inflector.constantize.
func (vm *VM) asResolver() inflector.Resolver {
	return func(name string) (any, bool) {
		segs := strings.Split(strings.TrimPrefix(name, "::"), "::")
		var cur object.Value
		for i, seg := range segs {
			if seg == "" {
				return nil, false
			}
			if i == 0 {
				v, ok := vm.cObject.consts[seg]
				if !ok {
					return nil, false
				}
				cur = v
				continue
			}
			cls, ok := cur.(*RClass)
			if !ok {
				return nil, false
			}
			v, ok := vm.constInAncestors(cls, seg)
			if !ok {
				return nil, false
			}
			cur = v
		}
		return cur, true
	}
}

// ASInflections is the DSL self a `ActiveSupport::Inflector.inflections { |inflect|
// … }` block runs against: inflect.plural/singular/human register a (regexp,
// replacement) rule, inflect.irregular a singular/plural pair, inflect.uncountable
// marks never-inflected words and inflect.acronym an acronym. It wraps the VM's
// live inflection ruleset, so registrations take effect immediately for both
// Inflector.pluralize and the String core extensions.
type ASInflections struct{ inf *inflector.Inflections }

func (i *ASInflections) ToS() string     { return "#<ActiveSupport::Inflector::Inflections>" }
func (i *ASInflections) Inspect() string { return i.ToS() }
func (i *ASInflections) Truthy() bool    { return true }

// registerActiveSupportInflections installs ActiveSupport::Inflector::Inflections
// and Inflector.inflections, the registration entry point (with or without a
// block; the block form yields the ruleset and is the common one).
func (vm *VM) registerActiveSupportInflections(mod *RClass) {
	cls := newClass("ActiveSupport::Inflector::Inflections", vm.cObject)
	mod.consts["Inflections"] = cls
	vm.consts["ActiveSupport::Inflector::Inflections"] = cls

	cls.define("plural", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ci, pat := regexParts(args[0])
		self.(*ASInflections).inf.Plural(ci, pat, replBackrefs(strArg(args[1])))
		return object.NilV
	})
	cls.define("singular", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ci, pat := regexParts(args[0])
		self.(*ASInflections).inf.Singular(ci, pat, replBackrefs(strArg(args[1])))
		return object.NilV
	})
	cls.define("human", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ci, pat := regexParts(args[0])
		self.(*ASInflections).inf.Human(ci, pat, replBackrefs(strArg(args[1])))
		return object.NilV
	})
	cls.define("irregular", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ASInflections).inf.Irregular(strArg(args[0]), strArg(args[1]))
		return object.NilV
	})
	cls.define("uncountable", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ASInflections).inf.Uncountable(wordList(args)...)
		return object.NilV
	})
	cls.define("acronym", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ASInflections).inf.Acronym(strArg(args[0]))
		return object.NilV
	})

	mod.smethods["inflections"] = &Method{name: "inflections", owner: mod,
		native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
			obj := &ASInflections{inf: vm.asInflections}
			if blk != nil {
				vm.callBlock(blk, []object.Value{obj})
			}
			return obj
		}}
}

// wordList flattens the arguments of inflect.uncountable, which accepts either a
// splat of words (uncountable("fish", "sheep")) or a single array
// (uncountable(%w[fish sheep])).
func wordList(args []object.Value) []string {
	if len(args) == 1 {
		if a, ok := args[0].(*object.Array); ok {
			out := make([]string, len(a.Elems))
			for i, e := range a.Elems {
				out[i] = strArg(e)
			}
			return out
		}
	}
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = strArg(a)
	}
	return out
}

// regexParts extracts the (case-insensitive, pattern) pair a rule registration
// needs from a Regexp (its /…/i flag and source) or a String (a literal pattern).
func regexParts(v object.Value) (bool, string) {
	switch t := v.(type) {
	case *Regexp:
		return strings.Contains(t.flags, "i"), t.source
	case *object.String:
		return false, t.Str()
	default:
		raise("TypeError", "wrong argument type %s (expected Regexp)", v.Inspect())
		return false, ""
	}
}

// reBackref matches a Ruby \N backreference in an inflection replacement.
var reBackref = regexp.MustCompile(`\\([0-9])`)

// replBackrefs rewrites Ruby \1 backreferences into the Go Expand ${1} syntax the
// inflector library's replacements use.
func replBackrefs(s string) string {
	return reBackref.ReplaceAllStringFunc(s, func(m string) string { return "${" + m[1:] + "}" })
}

// camelUpper reads the optional first-letter argument of camelize: :lower selects
// lowerCamelCase, anything else UpperCamelCase.
func camelUpper(v object.Value) bool {
	s, ok := v.(object.Symbol)
	return !(ok && string(s) == "lower")
}

// ---- core extensions --------------------------------------------------------

// registerActiveSupportCoreExt adds the ActiveSupport core-extension methods to
// the core classes (active_support/all). String#except/#slice are already the
// core Ruby methods, so only the ActiveSupport-specific additions are installed.
func (vm *VM) registerActiveSupportCoreExt() {
	vm.registerASString()
	vm.registerASArray()
	vm.registerASHash()
	vm.registerASInteger()
	vm.registerASObject()
	vm.registerASEnumerable()
}

func (vm *VM) registerASString() {
	d := func(name string, fn NativeFn) { vm.cString.define(name, fn) }
	str := func(self object.Value) string { return self.(*object.String).Str() }

	d("blank?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(coreext.StringBlank(str(self)))
	})
	d("present?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(coreext.StringPresent(str(self)))
	})
	d("presence", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if s, ok := coreext.StringPresence(str(self)); ok {
			return object.NewString(s)
		}
		return object.NilV
	})
	d("squish", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(coreext.Squish(str(self)))
	})
	d("truncate", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(coreext.Truncate(str(self), int(intArg(args[0])), "", ""))
	})
	d("camelize", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		upper := true
		if len(args) > 0 {
			upper = camelUpper(args[0])
		}
		return object.NewString(vm.asInflections.Camelize(str(self), upper))
	})
	d("underscore", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.Underscore(str(self)))
	})
	d("pluralize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.Pluralize(str(self)))
	})
	d("titleize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.Titleize(str(self), false))
	})
	d("parameterize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(inflector.Parameterize(str(self), "-", false))
	})
	d("classify", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.Classify(str(self)))
	})
	d("humanize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.asInflections.Humanize(str(self), true, false))
	})
	d("starts_with?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(coreext.StartsWith(str(self), strArg(args[0])))
	})
	d("ends_with?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(coreext.EndsWith(str(self), strArg(args[0])))
	})
}

func (vm *VM) registerASArray() {
	d := func(name string, fn NativeFn) { vm.cArray.define(name, fn) }
	elems := func(self object.Value) []object.Value { return self.(*object.Array).Elems }

	d("blank?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(coreext.ArrayBlank(boxSlice(elems(self))))
	})
	d("in_groups", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		n := int(intArg(args[0]))
		if len(args) > 1 && isFalse(args[1]) {
			return rubyFromGroups(coreext.InGroupsNoFill(boxSlice(elems(self)), n))
		}
		return rubyFromGroups(coreext.InGroups(boxSlice(elems(self)), n, fillArg(args)))
	})
	d("in_groups_of", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		n := int(intArg(args[0]))
		if len(args) > 1 && isFalse(args[1]) {
			return rubyFromGroups(coreext.InGroupsOfNoFill(boxSlice(elems(self)), n))
		}
		return rubyFromGroups(coreext.InGroupsOf(boxSlice(elems(self)), n, fillArg(args)))
	})
	d("to_sentence", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		strs := make([]any, len(elems(self)))
		for i, e := range elems(self) {
			strs[i] = vm.send(e, "to_s", nil, nil).(*object.String).Str()
		}
		return object.NewString(coreext.ToSentence(strs))
	})
	d("second", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rubyFrom(coreext.Second(boxSlice(elems(self))))
	})
	d("third", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rubyFrom(coreext.Third(boxSlice(elems(self))))
	})
	d("fourth", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rubyFrom(coreext.Fourth(boxSlice(elems(self))))
	})
	d("fifth", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rubyFrom(coreext.Fifth(boxSlice(elems(self))))
	})
}

func (vm *VM) registerASHash() {
	d := func(name string, fn NativeFn) { vm.cHash.define(name, fn) }
	h := func(self object.Value) *object.Hash { return self.(*object.Hash) }

	d("blank?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(coreext.HashBlank(hashToGo(h(self))))
	})
	d("deep_merge", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		other := hashArg(args[0])
		gh, gother := hashToGo(h(self)), hashToGo(other)
		return rubyHashOrdered(coreext.DeepMerge(gh, gother), mergeOrder(h(self), other))
	})
	d("deep_dup", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rubyHashOrdered(coreext.DeepDup(hashToGo(h(self))), hashGoKeys(h(self)))
	})
	d("symbolize_keys", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rubyHashOrdered(coreext.SymbolizeKeys(hashToGo(h(self))), mapKeys(hashGoKeys(h(self)), asSymKey))
	})
	d("stringify_keys", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rubyHashOrdered(coreext.StringifyKeys(hashToGo(h(self))), mapKeys(hashGoKeys(h(self)), asStrKey))
	})
	d("reverse_merge", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		other := hashArg(args[0])
		gh, gother := hashToGo(h(self)), hashToGo(other)
		return rubyHashOrdered(coreext.ReverseMerge(gh, gother), reverseMergeOrder(h(self), other))
	})
}

func (vm *VM) registerASInteger() {
	d := func(name string, fn NativeFn) { vm.cInteger.define(name, fn) }

	d("ordinalize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(coreext.Ordinalize(int(self.(object.Integer))))
	})
	d("multiple_of?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(coreext.MultipleOf(int(self.(object.Integer)), int(intArg(args[0]))))
	})
}

func (vm *VM) registerASObject() {
	d := func(name string, fn NativeFn) { vm.cObject.define(name, fn) }

	d("blank?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(coreext.Blank(vm.asBlankArg(self)))
	})
	d("present?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(coreext.Present(vm.asBlankArg(self)))
	})
	d("presence", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if coreext.Blank(vm.asBlankArg(self)) {
			return object.NilV
		}
		return self
	})
	d("try", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		// Object#try with a block and no method name yields self (unless nil).
		if len(args) == 0 {
			if blk != nil && !object.IsNil(self) {
				return vm.callBlock(blk, []object.Value{self})
			}
			return object.NilV
		}
		method := symOrStrName(args[0])
		rest := make([]any, len(args)-1)
		for i, a := range args[1:] {
			rest[i] = a
		}
		res := coreext.Try(tryRecv(self), vm.asDispatcher(blk), method, rest...)
		return rubyFrom(res)
	})
}

func (vm *VM) registerASEnumerable() {
	en := vm.consts["Enumerable"].(*RClass)
	d := func(name string, fn NativeFn) { en.define(name, fn) }

	d("index_by", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "index_by requires a block")
		}
		elems := vm.enumElems(self)
		keys := make([]any, len(elems))
		for i, e := range elems {
			keys[i] = asGo(vm.callBlock(blk, []object.Value{e}))
		}
		i := 0
		m := coreext.IndexBy(boxSlice(elems), func(any) any { k := keys[i]; i++; return k })
		return rubyHashOrdered(m, firstSeen(keys))
	})
	d("many?", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		items := boxSlice(vm.enumElems(self))
		if blk != nil {
			return object.Bool(coreext.ManyBy(items, func(it any) bool {
				return vm.callBlock(blk, []object.Value{it.(object.Value)}).Truthy()
			}))
		}
		return object.Bool(coreext.Many(items))
	})
	d("exclude?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(coreext.Exclude(vm.goSlice(vm.enumElems(self)), asGo(args[0])))
	})
	d("pluck", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		items := boxSlice(vm.enumElems(self))
		if len(args) == 1 {
			key := args[0]
			return rubyFrom(coreext.Pluck(items, func(it any) any {
				return vm.send(it.(object.Value), "[]", []object.Value{key}, nil)
			}))
		}
		return rubyFrom(coreext.Pluck(items, func(it any) any { return vm.pluckRow(it, args) }))
	})
	d("pick", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		items := boxSlice(vm.enumElems(self))
		if len(args) == 1 {
			key := args[0]
			v, ok := coreext.Pick(items, func(it any) any {
				return vm.send(it.(object.Value), "[]", []object.Value{key}, nil)
			})
			if !ok {
				return object.NilV
			}
			return v.(object.Value)
		}
		v, ok := coreext.Pick(items, func(it any) any { return vm.pluckRow(it, args) })
		if !ok {
			return object.NilV
		}
		return rubyFrom(v)
	})
}

// pluckRow reads each of keys from one element via its #[], returning the row as a
// Go slice (the multi-key form of pluck / pick).
func (vm *VM) pluckRow(it any, keys []object.Value) any {
	row := make([]any, len(keys))
	for j, k := range keys {
		row[j] = vm.send(it.(object.Value), "[]", []object.Value{k}, nil)
	}
	return row
}

// ---- seams + conversions ----------------------------------------------------

// asDispatcher builds the coreext.Dispatcher seam over rbgo's object model: it
// sends method to the receiver (a boxed rbgo value) and reports whether the
// receiver responds to it, so Object#try can skip an unknown method.
func (vm *VM) asDispatcher(blk *Proc) coreext.Dispatcher {
	return func(recv any, method string, cargs []any) (any, bool) {
		rv := recv.(object.Value)
		if !vm.respondsTo(rv, method) {
			return nil, false
		}
		rubyArgs := make([]object.Value, len(cargs))
		for i, a := range cargs {
			rubyArgs[i] = a.(object.Value)
		}
		return vm.send(rv, method, rubyArgs, blk), true
	}
}

// tryRecv maps a receiver to the value coreext.Try expects: Go nil for a Ruby nil
// (so Try short-circuits to nil), otherwise the boxed rbgo value.
func tryRecv(self object.Value) any {
	if object.IsNil(self) {
		return nil
	}
	return self
}

// asBlankArg maps a value onto the representation coreext.Blank understands: nil
// and false are blank; anything responding to empty? routes through the Blankable
// seam; everything else is present.
func (vm *VM) asBlankArg(v object.Value) any {
	switch t := v.(type) {
	case object.Nil:
		return nil
	case object.Bool:
		return bool(t)
	default:
		if vm.respondsTo(v, "empty?") {
			return asBlankable{vm: vm, v: v}
		}
		return v
	}
}

// asBlankable implements coreext.Blankable for a Ruby object that responds to
// empty? (blank? on such an object is its empty?).
type asBlankable struct {
	vm *VM
	v  object.Value
}

func (b asBlankable) IsBlank() bool { return b.vm.send(b.v, "empty?", nil, nil).Truthy() }

// symOrStrName reads a method-name argument (Symbol or String) for Object#try.
func symOrStrName(v object.Value) string {
	switch t := v.(type) {
	case object.Symbol:
		return string(t)
	case *object.String:
		return t.Str()
	default:
		raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
		return ""
	}
}

// enumElems materialises an Enumerable receiver into a slice of elements: an Array
// yields its own elements, any other Enumerable is realised through #to_a.
func (vm *VM) enumElems(self object.Value) []object.Value {
	if a, ok := self.(*object.Array); ok {
		return a.Elems
	}
	return vm.send(self, "to_a", nil, nil).(*object.Array).Elems
}

// isFalse reports whether v is the Ruby literal false (the in_groups "do not pad"
// flag).
func isFalse(v object.Value) bool { return v == object.Bool(false) }

// fillArg returns the pad value for in_groups / in_groups_of: the second argument
// when present (nil pads with Ruby nil), else Ruby nil.
func fillArg(args []object.Value) any {
	if len(args) > 1 {
		return asGo(args[1])
	}
	return nil
}

// boxSlice wraps rbgo values in a []any without converting them, for the coreext
// helpers that treat elements opaquely (they only move/compare-by-identity them).
func boxSlice(elems []object.Value) []any {
	out := make([]any, len(elems))
	for i, e := range elems {
		out[i] = e
	}
	return out
}

// goSlice converts rbgo values to their coreext-native form, for helpers that
// compare elements by value (Enumerable#exclude?).
func (vm *VM) goSlice(elems []object.Value) []any {
	out := make([]any, len(elems))
	for i, e := range elems {
		out[i] = asGo(e)
	}
	return out
}

// hashToGo converts an rbgo Hash to the map[any]any the coreext Hash helpers take,
// recursively converting keys and values.
func hashToGo(hh *object.Hash) map[any]any {
	m := make(map[any]any, hh.Len())
	for _, k := range hh.Keys {
		v, _ := hh.Get(k)
		m[asGo(k)] = asGo(v)
	}
	return m
}

// asGo converts an rbgo value to the coreext-native representation (string /
// Symbol / bool / nil / int64 / float64 / []any / map[any]any). A value with no
// native form is passed through unchanged so a round-trip returns it untouched.
func asGo(v object.Value) any {
	switch t := v.(type) {
	case object.Nil:
		return nil
	case object.Bool:
		return bool(t)
	case *object.String:
		return t.Str()
	case object.Symbol:
		return coreext.Symbol(string(t))
	case object.Integer:
		return int64(t)
	case object.Float:
		return float64(t)
	case *object.Array:
		out := make([]any, len(t.Elems))
		for i, e := range t.Elems {
			out[i] = asGo(e)
		}
		return out
	case *object.Hash:
		return hashToGo(t)
	default:
		return v
	}
}

// rubyFrom converts a coreext-native value back to an rbgo value; a value that is
// already an rbgo value (an untouched pass-through) is returned as-is.
func rubyFrom(v any) object.Value {
	switch t := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(t)
	case string:
		return object.NewString(t)
	case coreext.Symbol:
		return object.Symbol(string(t))
	case int64:
		return object.IntValue(t)
	case float64:
		return object.Float(t)
	case []any:
		out := make([]object.Value, len(t))
		for i, e := range t {
			out[i] = rubyFrom(e)
		}
		return object.NewArrayFromSlice(out)
	case map[any]any:
		return rubyHashOrdered(t, sortedGoKeys(t))
	default:
		return v.(object.Value)
	}
}

// rubyFromGroups converts a [][]any group result (in_groups / in_groups_of) into
// an rbgo Array of Arrays.
func rubyFromGroups(g [][]any) object.Value {
	out := make([]object.Value, len(g))
	for i, grp := range g {
		out[i] = rubyFrom(grp)
	}
	return object.NewArrayFromSlice(out)
}

// rubyHashOrdered rebuilds an rbgo Hash from a coreext result map, visiting keys
// in the given order so the output preserves the MRI insertion order rather than
// Go's random map iteration.
func rubyHashOrdered(m map[any]any, order []any) object.Value {
	hh := object.NewHash()
	for _, k := range order {
		hh.Set(rubyFrom(k), rubyFrom(m[k]))
	}
	return hh
}

// hashGoKeys returns the coreext-native keys of an rbgo Hash in insertion order.
func hashGoKeys(hh *object.Hash) []any {
	out := make([]any, len(hh.Keys))
	for i, k := range hh.Keys {
		out[i] = asGo(k)
	}
	return out
}

// mapKeys applies conv to every key (the symbolize/stringify key transforms),
// keeping order.
func mapKeys(keys []any, conv func(any) any) []any {
	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = conv(k)
	}
	return out
}

// mergeOrder is the MRI key order of deep_merge(h, other): h's keys, then other's
// keys not already in h.
func mergeOrder(h, other *object.Hash) []any {
	order := hashGoKeys(h)
	seen := keySet(order)
	for _, k := range hashGoKeys(other) {
		if !seen[k] {
			seen[k] = true
			order = append(order, k)
		}
	}
	return order
}

// reverseMergeOrder is the MRI key order of reverse_merge(h, other): other's keys,
// then h's keys not already in other.
func reverseMergeOrder(h, other *object.Hash) []any {
	order := hashGoKeys(other)
	seen := keySet(order)
	for _, k := range hashGoKeys(h) {
		if !seen[k] {
			seen[k] = true
			order = append(order, k)
		}
	}
	return order
}

// firstSeen returns keys deduplicated, keeping the first occurrence order (the
// Hash order Enumerable#index_by produces).
func firstSeen(keys []any) []any {
	seen := make(map[any]bool, len(keys))
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// keySet collects keys into a set for membership tests.
func keySet(keys []any) map[any]bool {
	s := make(map[any]bool, len(keys))
	for _, k := range keys {
		s[k] = true
	}
	return s
}

// sortedGoKeys returns a map's keys in a deterministic (canonical-string) order,
// so a nested map round-trips to a stable Hash order.
func sortedGoKeys(m map[any]any) []any {
	ks := make([]any, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool { return goKeyStr(ks[i]) < goKeyStr(ks[j]) })
	return ks
}

// goKeyStr renders a coreext-native key canonically (type-tagged) for sorting.
func goKeyStr(k any) string {
	switch t := k.(type) {
	case string:
		return "s:" + t
	case coreext.Symbol:
		return "y:" + string(t)
	default:
		return "o:" + rubyFrom(k).Inspect()
	}
}

// asSymKey mirrors the library's symbolize-keys transform on an rbgo-native key.
func asSymKey(k any) any {
	switch t := k.(type) {
	case string:
		return coreext.Symbol(t)
	case coreext.Symbol:
		return t
	default:
		return k
	}
}

// asStrKey mirrors the library's stringify-keys transform on an rbgo-native key.
func asStrKey(k any) any {
	switch t := k.(type) {
	case string:
		return t
	case coreext.Symbol:
		return string(t)
	default:
		return rubyFrom(k).ToS()
	}
}

// hashArg coerces a Hash argument (deep_merge / reverse_merge), raising TypeError
// otherwise.
func hashArg(v object.Value) *object.Hash {
	h, ok := v.(*object.Hash)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Hash", v.Inspect())
	}
	return h
}
