// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
	pathname "github.com/go-ruby-pathname/pathname"
)

// registerPathname backs the LEXICAL surface of the prelude's Pathname class with
// github.com/go-ruby-pathname/pathname — a pure-Go (cgo-free) reimplementation of
// the pure path-manipulation methods of Ruby's pathname standard library, matching
// MRI 4.0.5. The path algebra (cleanpath, basename, dirname, extname, join/+,
// split, each_filename, ascend/descend, sub_ext, relative_path_from, absolute?)
// lives in the library; the prelude's Pathname keeps the @path state, Comparable,
// freeze and — out of the library's scope — the filesystem delegations
// (read/write/exist?/file?/directory?/open/each_line), which forward host-side to
// the File class.
//
// The binding is exposed as native class-level helpers named __lex_* on the
// Pathname class. They operate on plain path strings (and arrays of strings), so
// the Ruby side never has to carry a Go *Pathname per object: it wraps the string
// result back into a Ruby Pathname itself. registerPathname runs after the prelude
// so it reopens the prelude-defined class rather than creating it.
func (vm *VM) registerPathname() {
	cls, ok := vm.consts["Pathname"].(*RClass)
	if !ok {
		// The prelude always defines Pathname; if a host stripped it, there is
		// nothing to back, so leave the lexical helpers unbound.
		return
	}

	sm := func(name string, fn NativeFn) { cls.smethods[name] = &Method{name: name, owner: cls, native: fn} }

	// __lex_cleanpath(path) -> cleaned path string. Collapses "." / ".." / redundant
	// separators lexically (no filesystem access), as MRI's Pathname#cleanpath.
	sm("__lex_cleanpath", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(pathname.New(strArg(args[0])).Cleanpath().String())
	})

	// __lex_basename(path, suffix) -> last component string. suffix "" keeps the full
	// component; ".*" strips any extension; any other suffix is stripped when it
	// matches the trailing characters, as MRI's Pathname#basename.
	sm("__lex_basename", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathname.New(strArg(args[0]))
		suffix := strArg(args[1])
		var r *pathname.Pathname
		if suffix == "" {
			r = p.Basename()
		} else {
			r = p.BasenameSuffix(suffix)
		}
		return object.NewString(r.String())
	})

	// __lex_dirname(path) -> all but the last component (the Pathname#dirname /
	// #parent string), "." or "/" at the edges.
	sm("__lex_dirname", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(pathname.New(strArg(args[0])).Dirname().String())
	})

	// __lex_extname(path) -> the extension of the final component (".txt", or "" when
	// none), matching MRI's Pathname#extname (and its File.extname edge cases).
	sm("__lex_extname", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(pathname.New(strArg(args[0])).Extname())
	})

	// __lex_plus(base, rel) -> base + rel under MRI's Pathname#+ append rule (an
	// absolute rel resets to the root, otherwise a single "/" joins them).
	sm("__lex_plus", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(pathname.New(strArg(args[0])).PlusString(strArg(args[1])).String())
	})

	// __lex_sub_ext(path, repl) -> path with its extension replaced by repl (MRI's
	// Pathname#sub_ext); when the path has no extension, repl is appended.
	sm("__lex_sub_ext", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(pathname.New(strArg(args[0])).SubExt(strArg(args[1])).String())
	})

	// __lex_absolute?(path) -> whether the path starts at the root ("/"), as MRI's
	// Pathname#absolute?.
	sm("__lex_absolute?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(pathname.New(strArg(args[0])).Absolute())
	})

	// __lex_filenames(path) -> the non-empty "/"-separated components, the array
	// Pathname#each_filename yields over.
	sm("__lex_filenames", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return stringArray(pathname.New(strArg(args[0])).Filenames())
	})

	// __lex_ascend_paths(path) -> the path then each parent up to the root (or the
	// first relative component), the sequence Pathname#ascend yields (and #descend
	// yields reversed).
	sm("__lex_ascend_paths", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var out []string
		pathname.New(strArg(args[0])).Ascend(func(p *pathname.Pathname) { out = append(out, p.String()) })
		return stringArray(out)
	})

	// __lex_relative_path_from(path, base) -> path expressed relative to base using
	// only lexical components (MRI's Pathname#relative_path_from). An incompatible
	// base (mixing absolute/relative, or a ".." that escapes a relative base) raises
	// ArgumentError with MRI's exact message.
	sm("__lex_relative_path_from", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		res, err := pathname.New(strArg(args[0])).RelativePathFrom(pathname.New(strArg(args[1])))
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return object.NewString(res.String())
	})
}

// stringArray wraps a slice of Go strings into a Ruby Array of String objects.
func stringArray(ss []string) *object.Array {
	elems := make([]object.Value, len(ss))
	for i, s := range ss {
		elems[i] = object.NewString(s)
	}
	return &object.Array{Elems: elems}
}
