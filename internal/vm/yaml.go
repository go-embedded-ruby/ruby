// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerYAML installs the YAML / Psych standard library (require "yaml"). The
// module, its constant / error tree, and the dump / load / safe_load / load_file
// methods are pure-Go: the Psych-compatible emitter and loader live in the
// github.com/go-ruby-yaml/yaml library and rbgo binds them here, mapping its own
// object graph to and from that library's value model (see yaml_bind.go). The
// low-level node API (parse / parse_stream / dump_tags) still raises
// NotImplementedError as Puppet does not call it.
func (vm *VM) registerYAML() {
	psych := newClass("Psych", nil)
	psych.isModule = true
	vm.consts["Psych"] = psych
	// In MRI, YAML is an alias of Psych.
	vm.consts["YAML"] = psych

	std := vm.consts["StandardError"].(*RClass)
	psych.consts["Exception"] = newClass("Psych::Exception", std)
	psych.consts["SyntaxError"] = newClass("Psych::SyntaxError", psych.consts["Exception"].(*RClass))
	psych.consts["DisallowedClass"] = newClass("Psych::DisallowedClass", psych.consts["Exception"].(*RClass))
	psych.consts["VERSION"] = object.NewString("5.0.0")

	nodes := newClass("Psych::Nodes", nil)
	nodes.isModule = true
	psych.consts["Nodes"] = nodes

	notImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "YAML %s not yet supported (pure-Go YAML pending)", what)
		}
	}
	// The low-level node API (parse / parse_stream) and dump_tags are not used by
	// Puppet's local persistence and remain unimplemented.
	for _, m := range []string{"parse", "parse_stream", "dump_tags"} {
		psych.smethods[m] = &Method{name: m, owner: psych, native: notImpl(m)}
	}

	// YAML.load(source[, ...]) / Psych.load is RESTRICTED, and does not share an
	// implementation with unsafe_load. Psych 4 inverted the two: psych.rb:369 is
	// `def self.load yaml, permitted_classes: [Symbol], ...` delegating straight
	// to safe_load, which is the change Psych made precisely because `load` is
	// the call people habitually write. Binding it to the unrestricted loader was
	// right for Psych 3 and wrong from Psych 4 on. See psychLoadArgs. Leading
	// keyword/positional options Psych accepts (filename, symbolize_names, …) are
	// tolerated and ignored.
	loadFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return yamlSafeLoad(vm, yamlSourceArg(args[0]), psychLoadArgs(args[1:]))
	}
	psych.smethods["load"] = &Method{name: "load", owner: psych, native: loadFn}

	// YAML.unsafe_load is the unrestricted entry point (psych.rb:272), and the one
	// a caller that genuinely wants arbitrary classes back must now name.
	unsafeLoadFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return yamlLoad(vm, yamlSourceArg(args[0]))
	}
	psych.smethods["unsafe_load"] = &Method{name: "unsafe_load", owner: psych, native: unsafeLoadFn}

	// YAML.safe_load(source[, permitted_classes: [...], permitted_symbols: [...]])
	// restricts which classes a document may materialise. Psych's signature is
	// `permitted_classes: []`, so the DEFAULT is the empty allow-list: a bare
	// safe_load refuses every tagged class, Symbol and Time, and raises
	// Psych::DisallowedClass. See psychSafeArgs.
	safeLoadFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return yamlSafeLoad(vm, yamlSourceArg(args[0]), psychSafeArgs(args[1:]))
	}
	psych.smethods["safe_load"] = &Method{name: "safe_load", owner: psych, native: safeLoadFn}

	// YAML.load_file(path[, ...]) reads the file and parses its contents. It
	// delegates to load (psych.rb:716), so it carries load's restriction and its
	// keyword defaults -- measured: load_file over ":s\n" with
	// permitted_classes: [G] raises Psych::DisallowedClass under MRI, exactly as
	// load does, because the explicit list replaces the [Symbol] default.
	loadFileFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return yamlSafeLoad(vm, yamlReadFile(args[0]), psychLoadArgs(args[1:]))
	}
	psych.smethods["load_file"] = &Method{name: "load_file", owner: psych, native: loadFileFn}

	// YAML.unsafe_load_file(path) is unsafe_load over a file (psych.rb:694). It
	// is added here with the restriction, not before it: without it a caller that
	// legitimately wants objects back from a file -- which is what load_file used
	// to give them -- would have no spelling at all to migrate to.
	unsafeLoadFileFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return yamlLoad(vm, yamlReadFile(args[0]))
	}
	psych.smethods["unsafe_load_file"] = &Method{name: "unsafe_load_file", owner: psych, native: unsafeLoadFileFn}

	// YAML.safe_load_file(path[, permitted_classes: [...]]) is safe_load over a file.
	safeLoadFileFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return yamlSafeLoad(vm, yamlReadFile(args[0]), psychSafeArgs(args[1:]))
	}
	psych.smethods["safe_load_file"] = &Method{name: "safe_load_file", owner: psych, native: safeLoadFileFn}

	// YAML.dump(obj[, io]) serialises a tree of Ruby values to a Psych-compatible
	// document. With a second IO argument it writes the document there and returns
	// the IO (as Psych does), so Puppet::Util::Yaml.dump(structure, fh) persists
	// state and run-summary files; with one argument it returns the String. A value
	// outside the supported shapes raises TypeError, which the report YAML
	// indirector rescues and logs, leaving puppet apply otherwise clean.
	psych.smethods["dump"] = &Method{name: "dump", owner: psych,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
			}
			doc := object.NewString(yamlDump(vm, args[0]))
			if len(args) > 1 && args[1] != object.NilV {
				vm.send(args[1], "write", []object.Value{doc}, nil)
				return args[1]
			}
			return doc
		}}

	// Object#to_yaml (Psych installs this on Object) returns YAML.dump(self). It is
	// what Puppet's report store terminus calls (`fh.print to_yaml`), so every
	// object — including the full Puppet::Transaction::Report graph — serialises.
	vm.cObject.methods["to_yaml"] = &Method{name: "to_yaml", owner: vm.cObject,
		native: func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NewString(yamlDump(vm, self))
		}}
}

// yamlSourceArg coerces YAML.load's first argument to a string: a String yields
// its contents, and any other value its to_s, so an IO-ish or symbol argument
// does not crash the loader (Puppet always passes a String).
func yamlSourceArg(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// yamlReadFile reads the file named by v (coerced to a path string), raising
// Errno::ENOENT when it cannot be opened (matching Psych.load_file).
func yamlReadFile(v object.Value) string {
	path := strArg(v)
	data, err := fuReadFile(path)
	if err != nil {
		raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", path)
	}
	return string(data)
}

// psychSafeOpts is the resolved safe_load restriction: the set of class names a
// document may materialise, and optionally the set of Symbol names it may name.
//
// Psych's restriction has no "unset" state, which is why this type has no nil
// case for classes. Psych.safe_load's signature is `permitted_classes: []`
// (psych.rb:323 in Ruby 4.0.5), and Psych::ClassLoader::Restricted
// (psych/class_loader.rb:77-103) seeds @classes from exactly that list and from
// nothing else — so "no keyword" and "the empty list" are the SAME policy in
// MRI, and that policy is "refuse everything". Representing it as a nil slice
// meaning "permit all" (which this binding and the engine's
// WithPermittedClasses both did) makes the most restrictive setting the API can
// express the most permissive one it has.
type psychSafeOpts struct {
	// classes is the permitted_classes: allow-list, always non-nil. An empty map
	// is a real policy -- deny every class -- not the absence of one.
	classes map[string]bool
	// symbols narrows WHICH Symbol names may be interned, mirroring
	// permitted_symbols:. It is nil when no narrowing applies, because
	// Restricted#symbolize (class_loader.rb:84-92) short-circuits on
	// `@symbols.empty?`: unlike permitted_classes, an EMPTY permitted_symbols
	// list means "any name", not "no name". Narrowing is additional to the class
	// check, never a substitute for it -- MRI's symbolize still routes through
	// find("Symbol"), so permitted_symbols without Symbol in permitted_classes
	// refuses every symbol (measured against MRI 4.0.5).
	symbols map[string]bool
}

// permits reports whether a class name may materialise.
func (o psychSafeOpts) permits(name string) bool { return o.classes[name] }

// checkClass raises Psych::DisallowedClass naming name unless it is permitted,
// with MRI's message (psych/exception.rb:23-27).
func (o psychSafeOpts) checkClass(name string) {
	if !o.permits(name) {
		raise("Psych::DisallowedClass", "Tried to load unspecified class: %s", name)
	}
}

// checkSymbol applies MRI's two-part symbol rule: the name must pass the
// permitted_symbols narrowing (when one is in force), and Symbol itself must be
// a permitted class. Either refusal raises DisallowedClass naming "Symbol",
// exactly as Restricted#symbolize and Restricted#find both do.
func (o psychSafeOpts) checkSymbol(name string) {
	if o.symbols != nil && !o.symbols[name] {
		raise("Psych::DisallowedClass", "Tried to load unspecified class: Symbol")
	}
	o.checkClass("Symbol")
}

// psychSafeArgs resolves safe_load's trailing keyword hash into the restriction
// it describes. The zero keyword case is NOT "permit everything": it is
// permitted_classes: [], Psych's own default, which permits no class at all.
//
// A permitted_classes: that is not an Array (nil, or a bare class) fails CLOSED
// here -- the allow-list stays empty -- where MRI raises NoMethodError from
// `permitted_classes.map(&:to_s)`. Both refuse the document; only the exception
// class differs.
func psychSafeArgs(rest []object.Value) psychSafeOpts {
	o := psychSafeOpts{classes: map[string]bool{}}
	if len(rest) == 0 {
		return o
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	if !ok {
		return o
	}
	for _, name := range psychNameList(h, "permitted_classes") {
		o.classes[name] = true
	}
	// An empty permitted_symbols narrows nothing (see psychSafeOpts.symbols), so
	// the map is only built when at least one name is given.
	if names := psychNameList(h, "permitted_symbols"); len(names) > 0 {
		o.symbols = map[string]bool{}
		for _, name := range names {
			o.symbols[name] = true
		}
	}
	return o
}

// psychLoadArgs is psychSafeArgs with Psych.load's own default allow-list.
//
// psych.rb:369 declares `def self.load yaml, permitted_classes: [Symbol], ...`
// and delegates to safe_load, so load differs from safe_load ONLY in that
// default. An explicit permitted_classes REPLACES it rather than adding to it,
// because it is an ordinary keyword default -- measured against MRI 4.0.5:
//
//	load(":s")                           => :s
//	load(":s", permitted_classes: [G])   => Psych::DisallowedClass
//	load(":s", permitted_classes: [G, Symbol]) => :s
//
// which is why the seeding below is conditional on the keyword being ABSENT
// rather than on the resulting list being empty. Absent and empty are different
// policies here: absent means "use load's default", empty means "deny
// everything", and psychSafeOpts exists to keep them distinguishable.
func psychLoadArgs(rest []object.Value) psychSafeOpts {
	o := psychSafeArgs(rest)
	if !psychHasKeyword(rest, "permitted_classes") {
		o.classes["Symbol"] = true
	}
	return o
}

// psychHasKeyword reports whether the trailing keyword hash carries key at all,
// which is not the same question as whether its value is empty.
func psychHasKeyword(rest []object.Value, key string) bool {
	if len(rest) == 0 {
		return false
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	if !ok {
		return false
	}
	_, ok = h.Get(object.Symbol(key))
	return ok
}

// psychNameList reads one keyword from h as a list of names. A Class / Module
// element contributes its name and any other element its to_s, matching how
// Psych accepts `[Date]` and `["Date"]` alike (`permitted_classes.map(&:to_s)`).
func psychNameList(h *object.Hash, key string) []string {
	val, ok := h.Get(object.Symbol(key))
	if !ok {
		return nil
	}
	arr, ok := val.(*object.Array)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(arr.Elems))
	for _, el := range arr.Elems {
		if c, ok := el.(*RClass); ok {
			names = append(names, c.ToS())
			continue
		}
		names = append(names, el.ToS())
	}
	return names
}
