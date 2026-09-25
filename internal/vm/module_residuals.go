// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerModuleResiduals installs the remaining Module/Class reflection surface
// toward MRI 3.4/4.0: a proper Module#const_get / #const_defined? (String or
// Symbol name, "A::B" scoped paths, a leading "::" toplevel qualifier, the
// inherit flag, #to_str coercion and the #const_missing hook), the default
// Module#const_missing (raising a NameError that carries the constant name),
// Module#included_modules and Module#remove_class_variable. const_get and
// const_defined? are (re)defined here, replacing the single-name versions that
// used to live in builtins.go.
func (vm *VM) registerModuleResiduals() {
	// Module#const_get(name, inherit=true): resolve a constant by Symbol or String
	// name, honouring scoped paths and the inherit flag, and routing an
	// unresolved name through #const_missing on the module where it was sought.
	vm.cModule.define("const_get", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		inherit := len(args) < 2 || args[1].Truthy()
		mod := self.(*RClass)
		v := vm.moduleConstGet(mod, args[0], inherit)
		if mod.deprecatedConsts != nil {
			if s, ok := args[0].(*object.String); ok && mod.deprecatedConsts[s.Str()] {
				vm.warnDeprecatedConst(mod, s.Str())
			} else if sym, ok := args[0].(object.Symbol); ok && mod.deprecatedConsts[string(sym)] {
				vm.warnDeprecatedConst(mod, string(sym))
			}
		}
		return v
	})

	// Module#const_defined?(name, inherit=true): report whether name resolves,
	// without triggering an autoload require or calling #const_missing.
	vm.cModule.define("const_defined?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		inherit := len(args) < 2 || args[1].Truthy()
		return object.Bool(vm.moduleConstDefined(self.(*RClass), args[0], inherit))
	})

	// Module#const_source_location(name, inherit=true) answers where a constant
	// was defined: [file, line], [] for a constant defined in C — MRI's
	// rb_const_location_from returns rb_ary_new() when the entry's file is nil —
	// or nil when the name resolves to no constant in the search path.
	//
	// The SEARCH is MRI's exactly (ruby/ruby v3_4_0 object.c
	// rb_mod_const_source_location → variable.c rb_const_source_location /
	// rb_const_source_location_at → rb_const_location): a String may carry a
	// "A::B" path and a leading "::" toplevel qualifier, every segment but the
	// last is resolved with const_get and must name a class or module, and the
	// inherit flag is consumed by the FIRST segment only (`recur = Qfalse` at the
	// foot of the loop), so "ClassA::CS_CONST10" reads ClassA's OWN table. With
	// inherit the walk covers the whole ancestor chain — Object included, which
	// is how a class receiver reports a toplevel constant — and a module
	// receiver, whose chain never reaches Object, falls back to it explicitly.
	// Without inherit only the receiver's own table is read, and Object is
	// excluded unless it IS the receiver.
	//
	// rbgo records no definition site for a constant yet: the single place a
	// Ruby-level assignment lands is vm.assignConstIn, and that file belongs to
	// another wave. So every constant that RESOLVES answers [] — exactly right
	// for the ones this VM defines in Go, which is what MRI's "defined in C
	// code" means, and a known gap for the rest. What is decided here is the
	// nil/[]/NameError contract and the search itself, which no location table
	// would change.
	vm.cModule.define("const_source_location", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 || len(args) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
		}
		mod := self.(*RClass)
		recur := len(args) < 2 || args[1].Truthy()
		segs, topLevel, orig := vm.constPathSegs(args[0])
		if topLevel {
			mod = vm.cObject
		}
		// Every segment but the last names the module to look in next, and only
		// the FIRST of them is resolved with inherit (MRI sets recur = Qfalse at
		// the foot of its loop). constPathSegs never yields an empty list, so the
		// last segment always exists and always answers.
		for i, seg := range segs[:len(segs)-1] {
			if !constNameWellFormed(seg) {
				raise("NameError", "wrong constant name %s", seg)
			}
			v, ok := vm.constSegGet(mod, seg, recur, i == 0)
			if !ok {
				v = vm.constMissing(mod, seg) // raises NameError by default
			}
			nc, isCls := v.(*RClass)
			if !isCls {
				raise("TypeError", "%s does not refer to class/module", orig)
			}
			mod, recur = nc, false
		}
		last := segs[len(segs)-1]
		if !constNameWellFormed(last) {
			raise("NameError", "wrong constant name %s", last)
		}
		return vm.constLocation(mod, last, !recur, recur)
	})

	// Module#const_missing(sym): the default hook, raising a NameError naming the
	// missing constant. The toplevel (Object) form omits the "Object::" qualifier,
	// matching MRI. The NameError carries the name so NameError#name returns it.
	vm.cModule.define("const_missing", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := self.(*RClass)
		name := nameArg(args[0])
		var msg string
		if mod == vm.cObject || mod.name == "" {
			msg = "uninitialized constant " + name
		} else {
			msg = "uninitialized constant " + mod.name + "::" + name
		}
		return vm.raiseNameError(msg, name)
	})

	// Module#included_modules: the modules (not classes) in the receiver's ancestor
	// chain — its includes and prepends, and those of its ancestors — excluding the
	// receiver itself. Order follows the ancestor chain.
	vm.cModule.define("included_modules", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		me := self.(*RClass)
		var out []object.Value
		for _, c := range vm.ancestors(me) {
			if c != me && c.isModule {
				out = append(out, c)
			}
		}
		return object.NewArrayFromSlice(out)
	})

	// Module#initialize_copy(orig): the table copy behind Module#dup / #clone.
	// MRI's rb_mod_init_copy (ruby/ruby v3_4_0 class.c) gives the copy orig's
	// superclass, a fresh method table whose entries are re-owned by the copy
	// (clone_method_i), copies of the constant, class-variable and
	// instance-variable tables (copy_tables), and a cloned singleton class
	// (rb_singleton_class_clone) — but NOT orig's name: a copy is anonymous until
	// something binds it to a constant, which is why `Named.dup.name` is nil.
	//
	// rbgo had no Module case at all in dupValue, so Object#dup returned the
	// receiver ITSELF for a class or module: `m.dup.equal?(m)` was true and every
	// later const_set or def landed on the original.
	vm.cModule.define("initialize_copy", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		dst := self.(*RClass)
		src, ok := args[0].(*RClass)
		if !ok {
			raise("TypeError", "initialize_copy should take same class object")
		}
		if dst == src { // copying from itself does nothing, as Kernel#initialize_copy
			return dst
		}
		// class_init_copy_check (class.c): BasicObject and any singleton class
		// refuse to be copied. A singleton class is tied to the one object it is
		// attached to, so a second one would be a contradiction.
		if src == vm.cBasicObject {
			raise("TypeError", "can't copy the root class")
		}
		if src.isSingleton {
			raise("TypeError", "can't copy singleton class")
		}
		if dst.frozen {
			vm.raiseFrozen(dst)
		}
		copyModuleTables(dst, src)
		return dst
	})

	// Module#dup / #clone. They allocate a bare module or class of the receiver's
	// kind and run the ordinary copy hooks on it, so a user #initialize_copy
	// override still governs what is carried over. dup leaves the copy unfrozen;
	// clone copies the frozen state and honours clone(freeze: true/false).
	// Reference: ruby/ruby v3_4_0 object.c rb_obj_dup / rb_obj_clone2.
	allocCopy := func(src *RClass) *RClass {
		cp := newClass("", nil)
		cp.isModule = src.isModule
		return cp
	}
	vm.cModule.define("dup", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 0 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
		}
		src := self.(*RClass)
		cp := allocCopy(src)
		vm.send(cp, "initialize_dup", []object.Value{src}, nil)
		return cp
	})
	vm.cModule.define("clone", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		src := self.(*RClass)
		freezeVal, freezeGiven := cloneFreezeArg(vm, args)
		cp := allocCopy(src)
		if freezeGiven {
			kw := object.NewHash()
			kw.Set(object.Symbol("freeze"), freezeVal)
			vm.send(cp, "initialize_clone", []object.Value{src, kw}, nil)
		} else {
			vm.send(cp, "initialize_clone", []object.Value{src}, nil)
		}
		doFreeze := src.frozen
		if b, ok := freezeVal.(object.Bool); ok {
			doFreeze = bool(b)
		}
		if doFreeze {
			vm.send(cp, "freeze", nil, nil)
		}
		return cp
	})

	// Module#remove_class_variable(sym): remove a class variable defined DIRECTLY
	// on the receiver (never one inherited or mixed in) and return its value,
	// raising NameError when the name is malformed or the variable is absent.
	vm.cModule.define("remove_class_variable", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := cvarNameArg(args[0])
		cls := self.(*RClass)
		if v, ok := cls.cvars[name]; ok {
			delete(cls.cvars, name)
			return v
		}
		return raise("NameError", "class variable %s not defined for %s", name, cls.ToS())
	})
}

// moduleConstGet implements Module#const_get for cls: it parses arg into a scope
// (receiver or the top level) and one or more path segments, resolves each
// segment in turn, and — on a miss — invokes #const_missing on the module where
// the segment was looked up (whose default raises NameError).
func (vm *VM) moduleConstGet(cls *RClass, arg object.Value, inherit bool) object.Value {
	segs, topLevel, orig := vm.constPathSegs(arg)
	mod := cls
	if topLevel {
		mod = vm.cObject
	}
	var result object.Value
	for i, seg := range segs {
		if !constNameWellFormed(seg) {
			raise("NameError", "wrong constant name %s", seg)
		}
		v, ok := vm.constSegGet(mod, seg, inherit, i == 0)
		if !ok {
			return vm.constMissing(mod, seg)
		}
		if i < len(segs)-1 {
			nc, isCls := v.(*RClass)
			if !isCls {
				raise("TypeError", "%s does not refer to class/module", orig)
			}
			mod = nc
		}
		result = v
	}
	return result
}

// moduleConstDefined implements Module#const_defined? for cls, mirroring
// moduleConstGet's path handling but reporting presence (never triggering an
// autoload require or #const_missing).
func (vm *VM) moduleConstDefined(cls *RClass, arg object.Value, inherit bool) bool {
	segs, topLevel, orig := vm.constPathSegs(arg)
	mod := cls
	if topLevel {
		mod = vm.cObject
	}
	for i, seg := range segs {
		if !constNameWellFormed(seg) {
			raise("NameError", "wrong constant name %s", seg)
		}
		v, ok := vm.constSegDefined(mod, seg, inherit, i == 0)
		if !ok {
			return false
		}
		if i < len(segs)-1 {
			nc, isCls := v.(*RClass)
			if !isCls {
				raise("TypeError", "%s does not refer to class/module", orig)
			}
			mod = nc
		}
	}
	return true
}

// constPathSegs coerces a const_get / const_defined? name argument (Symbol,
// String, or an object with #to_str) into its path segments, reporting whether a
// leading "::" selected the top level. A Symbol may not carry a scope path or
// qualifier — MRI treats "::" inside a Symbol name as a malformed constant name.
//
// It also returns the name as given, for the errors that report the WHOLE path:
// MRI's wrong_name names the offending SEGMENT when a segment is not a constant
// name (`name = part` in rb_mod_const_get and its siblings) and the whole string
// only for a path that is malformed as a path — a bare "::", a trailing one, or
// two in a row — where there is no one segment to blame.
func (vm *VM) constPathSegs(arg object.Value) (segs []string, topLevel bool, orig string) {
	name, isSym := vm.constArgToString(arg)
	orig = name
	if isSym {
		if strings.Contains(name, "::") {
			raise("NameError", "wrong constant name %s", name)
		}
		return []string{name}, false, orig
	}
	rest := name
	if strings.HasPrefix(rest, "::") {
		topLevel = true
		rest = rest[2:]
	}
	segs = strings.Split(rest, "::")
	// An empty segment — a trailing "::", successive "::::" or a bare "::" — is a
	// malformed name that MRI rejects up front, before resolving any earlier
	// segment. (A non-empty but mis-capitalised segment is checked lazily, only
	// once resolution reaches it.)
	for _, s := range segs {
		if s == "" {
			raise("NameError", "wrong constant name %s", orig)
		}
	}
	return segs, topLevel, orig
}

// constArgToString coerces a constant-name argument to its text, reporting
// whether the source was a Symbol. A non-String/Symbol is converted with #to_str
// (a missing or non-String #to_str raises TypeError), matching MRI.
func (vm *VM) constArgToString(v object.Value) (string, bool) {
	switch n := v.(type) {
	case object.Symbol:
		return string(n), true
	case *object.String:
		return n.Str(), false
	default:
		if vm.respondsToDynamic(v, "to_str") {
			r := vm.send(v, "to_str", nil, nil)
			if s, ok := r.(*object.String); ok {
				return s.Str(), false
			}
			raise("TypeError", "can't convert %s to String (%s#to_str gives %s)",
				vm.classOf(v).name, vm.classOf(v).name, vm.classOf(r).name)
		}
		raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
		return "", false
	}
}

// constSegGet resolves one path segment in mod for const_get: with inherit it
// searches mod's ancestors (triggering a pending autoload), then — for the first
// segment of a non-Object receiver — the top level; without inherit only mod's
// own table is consulted.
func (vm *VM) constSegGet(mod *RClass, name string, inherit, isFirst bool) (object.Value, bool) {
	if !inherit {
		v, ok := mod.consts[name]
		return v, ok
	}
	if v, ok := vm.constInAncestors(mod, name); ok {
		return v, true
	}
	if vm.autoloadInAncestors(mod, name) {
		if v, ok := vm.constInAncestors(mod, name); ok {
			return v, true
		}
	}
	if isFirst && mod != vm.cObject {
		if v, ok := vm.cObject.consts[name]; ok {
			return v, true
		}
	}
	return object.NilVal(), false
}

// constSegDefined is constSegGet for const_defined?: it reports presence
// (including a pending, not-yet-run autoload) without requiring the autoload
// file.
func (vm *VM) constSegDefined(mod *RClass, name string, inherit, isFirst bool) (object.Value, bool) {
	if !inherit {
		if v, ok := mod.consts[name]; ok {
			return v, true
		}
		return object.NilVal(), vm.autoloadVisible(mod, name)
	}
	if v, ok := vm.constInAncestors(mod, name); ok {
		return v, true
	}
	if vm.autoloadPendingInAncestors(mod, name) {
		return object.NilVal(), true
	}
	if isFirst && mod != vm.cObject {
		if v, ok := vm.cObject.consts[name]; ok {
			return v, true
		}
		if vm.autoloadVisible(vm.cObject, name) {
			return object.NilVal(), true
		}
	}
	return object.NilVal(), false
}

// constMissing invokes #const_missing on mod with the missing constant's name,
// resolving a user override (a private one included) before the default. Its
// result is const_get's result when overridden; the default raises NameError.
func (vm *VM) constMissing(mod *RClass, name string) object.Value {
	return vm.send(mod, "const_missing", []object.Value{object.Symbol(name)}, nil)
}

// raiseNameError raises a NameError whose exception object carries name in its
// @name ivar, so NameError#name reports the offending constant/name (MRI).
func (vm *VM) raiseNameError(msg, name string) object.Value {
	obj := vm.buildException("NameError", msg)
	setIvar(obj, "@name", object.Symbol(name))
	panic(RubyError{Class: "NameError", Message: msg, Obj: obj})
}

// autoloadPendingInAncestors reports whether a pending (not-yet-run) autoload of
// name is registered on mod or an ancestor, without running it — the presence
// check const_defined? needs. It mirrors autoloadInAncestors' Object-skipping.
func (vm *VM) autoloadPendingInAncestors(mod *RClass, name string) bool {
	for _, c := range vm.ancestors(mod) {
		if (c == vm.cObject || c == vm.cBasicObject) && mod != vm.cObject && mod != vm.cBasicObject {
			continue
		}
		if vm.autoloadVisible(c, name) {
			return true
		}
	}
	return false
}

// hasAutoload reports whether c has a pending autoload registered for name.
func hasAutoload(c *RClass, name string) bool {
	if c == nil || c.autoloads == nil {
		return false
	}
	_, ok := c.autoloads[name]
	return ok
}

// constNameWellFormed reports whether s is a well-formed constant name, as MRI
// decides it in rb_enc_symname_type (ruby/ruby v3_4_0 symbol.c): a leading
// character rb_sym_constant_char_p calls a constant character, followed by
// is_identchar characters up to the end of the string. Used to reject "name",
// "__X__", "X=" or "X?" in const_get / const_defined? / const_set / autoload.
//
// The two rules are NOT "letters, digits and underscores", which is what rbgo
// had:
//
//   - The leading character is ASCII-uppercase, or — for a multi-byte one —
//     uppercase or TITLECASE in its encoding (rb_sym_constant_char_p falls
//     through to the titlecase ctype for Unicode). So "Ǆx" (U+01C4) and "ǅx"
//     (U+01C5, titlecase) are constants while "ǆx" (U+01C6) is not.
//   - Every LATER byte passes is_identchar, which is
//     `ISALNUM(*p) || *p == '_' || !ISASCII(*p)`: any non-ASCII byte is an
//     identifier byte, with no character test at all. That is why "A€" and
//     "A\u00A0B" are constant names in MRI, and why a name in a non-UTF-8
//     ASCII-compatible encoding — ruby/spec sets one with
//     "CS_CONSTλ".encode("euc-jp") — is one too. Testing the decoded RUNE, as
//     rbgo did, rejected all three: bytes that are not valid UTF-8 decode to
//     U+FFFD, which is not a letter.
//
// Known divergence: MRI raises EncodingError for a string whose bytes are
// invalid in its OWN encoding ("X\xFF" tagged UTF-8). rbgo sees only the bytes
// here — EUC-JP text is invalid UTF-8 too — so it cannot tell the two apart and
// accepts both.
func constNameWellFormed(s string) bool {
	if s == "" {
		return false
	}
	lead, size := utf8.DecodeRuneInString(s)
	switch {
	case size == 1 && s[0] < utf8.RuneSelf:
		if s[0] < 'A' || s[0] > 'Z' {
			return false
		}
	case lead == utf8.RuneError && size == 1:
		// A byte that starts no UTF-8 character: the name is in some other
		// ASCII-compatible encoding, whose multi-byte characters are neither
		// uppercase nor titlecase for rb_sym_constant_char_p.
		return false
	case !unicode.IsUpper(lead) && !unicode.IsTitle(lead):
		return false
	}
	for i := size; i < len(s); i++ {
		c := s[i]
		if c >= utf8.RuneSelf || c == '_' || ('0' <= c && c <= '9') ||
			('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') {
			continue
		}
		return false
	}
	return true
}

// isConstantPath reports whether s reads as a constant path — "A", "A::B", or a
// leading-"::" form — every segment of which is a well-formed constant name. An
// empty segment ("A::B::", "A::::B") or a segment that is not a constant ("a::B",
// "A=") makes the whole string something other than a constant path. It is the
// test Module#set_temporary_name uses to refuse a name that could be mistaken
// for a real one. Reference: ruby/ruby v3_4_0 variable.c is_constant_path.
func isConstantPath(s string) bool {
	if s == "" {
		return false
	}
	for len(s) > 0 {
		if strings.HasPrefix(s, "::") {
			s = s[2:]
		}
		seg := s
		if i := strings.IndexByte(s, ':'); i >= 0 {
			seg, s = s[:i], s[i:]
		} else {
			s = ""
		}
		if seg == "" || !constNameWellFormed(seg) {
			return false
		}
	}
	return true
}

// copyModuleTables gives dst the contents of src, as MRI's rb_mod_init_copy does
// through copy_tables + clone_method_i (ruby/ruby v3_4_0 class.c): the
// superclass and mixin chain, a method table whose entries are re-owned by dst
// so `super` still walks from it, the singleton (class-method) table, and the
// constant / class-variable / instance-variable tables. Each map is REBUILT, not
// aliased — that independence is the whole point of a copy.
//
// Deliberately not carried over: the name (a copy is anonymous — MRI never
// copies the classpath, so Named.dup.name is nil), the lexical parent that goes
// with it, the frozen flag (Module#clone sets it separately, Module#dup must
// not), the singleton-class identity (isSingleton / metaOf / attached / meta:
// the copy gets its own, minted lazily from the smethods copied here), and the
// transient class-body state (defaultVis, funcMode) which belongs to a body
// being executed rather than to the module.
func copyModuleTables(dst, src *RClass) {
	dst.super = src.super
	dst.isModule = src.isModule
	dst.methods = make(map[string]*Method, len(src.methods))
	for n, m := range src.methods {
		cp := *m
		cp.owner = dst
		dst.methods[n] = &cp
	}
	dst.smethods = make(map[string]*Method, len(src.smethods))
	for n, m := range src.smethods {
		cp := *m
		cp.owner = dst
		dst.smethods[n] = &cp
	}
	// A class that has been `extend`ed carries the mixin on its METACLASS rather
	// than in the singleton method table, and `class << self` constants live
	// there too. MRI copies the whole thing with rb_singleton_class_clone
	// (class.c), which is what keeps Struct.new(:a).extend(M).dup.hello working.
	if src.meta != nil {
		dm := dst.metaClass()
		dm.methods = dst.smethods // keep the metaclass aliasing the COPY's table
		dm.includes = append([]*RClass(nil), src.meta.includes...)
		dm.prepends = append([]*RClass(nil), src.meta.prepends...)
		dm.consts = copyValueTable(src.meta.consts)
		dm.cvars = copyValueTable(src.meta.cvars)
		dm.ivars = copyValueTable(src.meta.ivars)
		dm.visOverrides = copyVisTable(src.meta.visOverrides)
	}
	dst.consts = copyValueTable(src.consts)
	dst.cvars = copyValueTable(src.cvars)
	dst.ivars = copyValueTable(src.ivars)
	dst.includes = append([]*RClass(nil), src.includes...)
	dst.prepends = append([]*RClass(nil), src.prepends...)
	dst.autoloads = copyStringTable(src.autoloads)
	dst.deprecatedConsts = copyBoolTable(src.deprecatedConsts)
	dst.privateConsts = copyBoolTable(src.privateConsts)
	dst.visOverrides = copyVisTable(src.visOverrides)
	dst.svisOverrides = copyVisTable(src.svisOverrides)
	// A Struct/Data subclass keeps its member layout: MRI carries the allocator
	// over with RCLASS_SET_ALLOCATOR, which is what makes Struct.new(:a).dup.new(1)
	// still build a two-slot Struct rather than a bare object.
	dst.structDef = src.structDef
	dst.dataDef = src.dataDef
	bumpMethodSerial()
}

// copyValueTable returns a fresh map with the same entries. It takes no nil
// guard: newClass is the only *RClass constructor and it always creates the
// consts, cvars and ivars tables, which are the only maps handed to it.
func copyValueTable(src map[string]object.Value) map[string]object.Value {
	out := make(map[string]object.Value, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// copyStringTable is copyValueTable for the autoload path table.
func copyStringTable(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// copyBoolTable is copyValueTable for the deprecated/private constant sets.
func copyBoolTable(src map[string]bool) map[string]bool {
	if src == nil {
		return nil
	}
	out := make(map[string]bool, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// copyVisTable is copyValueTable for the per-receiver visibility overrides.
func copyVisTable(src map[string]visibility) map[string]visibility {
	if src == nil {
		return nil
	}
	out := make(map[string]visibility, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// cloneFreezeArg reads the freeze: keyword of Module#clone, returning its value
// and whether it was given. MRI accepts only true, false or nil there
// (object.c rb_obj_clone2), and rejects any other keyword outright.
func cloneFreezeArg(vm *VM, args []object.Value) (object.Value, bool) {
	if len(args) == 0 {
		return object.NilV, false
	}
	h, ok := args[len(args)-1].(*object.Hash)
	if !ok || len(args) != 1 {
		// clone takes no positional argument, only the freeze: keyword, which
		// arrives as a single trailing Hash.
		raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
	}
	var val object.Value = object.NilV
	given := false
	for _, k := range h.Keys {
		if sym, isSym := k.(object.Symbol); isSym && sym == object.Symbol("freeze") {
			val, _ = h.Get(k)
			given = true
			continue
		}
		raise("ArgumentError", "unknown keyword: %s", k.Inspect())
	}
	if given {
		switch val.(type) {
		case object.Bool, object.Nil:
		default:
			raise("ArgumentError", "unexpected value for freeze: %s", vm.classOf(val).name)
		}
	}
	return val, given
}

// constLocation is MRI's rb_const_location (ruby/ruby v3_4_0 variable.c): search
// cls, then — when the search may leave the receiver and cls is a MODULE, whose
// ancestor chain never reaches Object — search Object as well. Object itself is
// never excluded from its own search. exclude/recurse are the pair
// const_source_location (false, true) and const_source_location_at (true, false)
// pass. It answers Ruby nil when nothing is found.
func (vm *VM) constLocation(cls *RClass, name string, exclude, recurse bool) object.Value {
	if cls == vm.cObject {
		exclude = false
	}
	if loc, ok := vm.constLocationFrom(cls, name, recurse); ok {
		return loc
	}
	if exclude || !cls.isModule {
		return object.NilV
	}
	if loc, ok := vm.constLocationFrom(vm.cObject, name, recurse); ok {
		return loc
	}
	return object.NilV
}

// constLocationFrom is MRI's rb_const_location_from: walk cls (and, with
// recurse, its ancestors) for an entry named name. A pending autoload counts as
// an entry — MRI reserves the name with an undefined value, and
// rb_const_location_from finds that entry like any other.
//
// MRI's loop carries one more guard, `if (exclude && klass == rb_cObject) goto
// not_found`. It is not reproduced because neither entry point can reach it:
// exclude is only ever set together with recurse == false, which leaves cls the
// one class walked, and a cls that IS Object has exclude cleared by
// rb_const_location before the walk. Keeping the arm would be a branch no
// argument list can take.
//
// The location itself is always the empty array here: see the note on
// Module#const_source_location. The second result distinguishes "found" from
// "no such constant", which is the difference between [] and nil.
func (vm *VM) constLocationFrom(cls *RClass, name string, recurse bool) (object.Value, bool) {
	chain := []*RClass{cls}
	if recurse {
		chain = vm.ancestors(cls)
	}
	for _, k := range chain {
		if constEntryPresent(k, name) {
			return object.NewArrayFromSlice(nil), true
		}
	}
	return nil, false
}

// constEntryPresent reports whether k's own constant table holds an entry for
// name — a defined constant or a still-pending autoload, which MRI stores as an
// entry with an undefined value.
func constEntryPresent(k *RClass, name string) bool {
	if _, ok := k.consts[name]; ok {
		return true
	}
	_, pending := k.autoloads[name]
	return pending
}
