// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	getoptlong "github.com/go-ruby-getoptlong/getoptlong"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// GetoptLong binds github.com/go-ruby-getoptlong/getoptlong — a pure-Go (cgo-free)
// faithful port of the option-scanning core of Ruby's GetoptLong (require
// "getoptlong", the getoptlong 0.2.1 bundled with MRI 4.0.5) — into rbgo,
// replacing the former NotImplementedError stubs on #each / #get / #get_option /
// #set_options / #each_option. The whole scanning ENGINE (long/short/abbreviation
// matching, =-joined and separate arguments, bundled short flags, the `--`
// terminator, the three ordering modes, optional-argument lookahead, and the full
// error taxonomy — InvalidOption / MissingArgument / NeedlessArgument /
// AmbiguousOption — with MRI-identical messages) lives in the library and is
// validated against MRI 4.0.5. This file is the thin host wrapper.
//
// Unlike MRI's GetoptLong, the library operates on an explicit argument slice it
// owns (Parser.Args) rather than mutating the global ARGV. rbgo reproduces MRI's
// global-ARGV semantics here: at construction (and on set_options) the parser is
// seeded from rbgo's ARGV / $* array, and after every scanning step the remaining
// arguments the library leaves in Parser.Args are written back into that very same
// ARGV Array object — so a program reading ARGV (or $*) after parsing sees the
// non-option operands, exactly as MRI leaves them.
type GetoptLong struct {
	p *getoptlong.Parser
	// vm.consts["ARGV"] is the global argument Array ($* / ARGV); the parser scans
	// a snapshot of it and the leftover is written back into this same object.
	argv *object.Array
	// invalid/missing/needless/ambiguous are the Ruby GetoptLong::* error classes a
	// scanning error is raised through, so the raised object carries the right
	// nested class for `rescue GetoptLong::InvalidOption` and friends.
	invalid, missing, needless, ambiguous *RClass
	// seeded records whether the parser has been bound to ARGV yet; the first
	// scanning call seeds Parser.Args from ARGV (MRI scans ARGV lazily, so a program
	// may replace ARGV between GetoptLong.new and the first #get).
	seeded bool
}

func (g *GetoptLong) ToS() string     { return "#<GetoptLong>" }
func (g *GetoptLong) Inspect() string { return g.ToS() }
func (g *GetoptLong) Truthy() bool    { return true }

// golOf returns the receiver as a *GetoptLong.
func golOf(v object.Value) *GetoptLong { return object.Kind[*GetoptLong](v) }

// registerGetoptLong installs the native GetoptLong class backed by the
// go-ruby-getoptlong library. The argument-kind constants (NO_ARGUMENT /
// REQUIRED_ARGUMENT / OPTIONAL_ARGUMENT) and the ordering constants (REQUIRE_ORDER
// / PERMUTE / RETURN_IN_ORDER) name the library's matching enums; the error class
// tree (Error < StandardError, with InvalidOption / MissingArgument /
// NeedlessArgument / AmbiguousOption under it) is the Ruby surface a CLI's recover
// path rescues. GetoptLong.new(*specs) builds a fresh library Parser from the
// option specs; the scanning methods forward to it and mirror Parser.Args back
// into ARGV.
func (vm *VM) registerGetoptLong() {
	c := newClass("GetoptLong", vm.cObject)
	vm.cGetoptLong = c
	vm.consts["GetoptLong"] = c
	c.consts["NO_ARGUMENT"] = object.IntValue(int64(getoptlong.NoArgument))
	c.consts["REQUIRED_ARGUMENT"] = object.IntValue(int64(getoptlong.RequiredArgument))
	c.consts["OPTIONAL_ARGUMENT"] = object.IntValue(int64(getoptlong.OptionalArgument))
	c.consts["REQUIRE_ORDER"] = object.IntValue(int64(getoptlong.RequireOrder))
	c.consts["PERMUTE"] = object.IntValue(int64(getoptlong.Permute))
	c.consts["RETURN_IN_ORDER"] = object.IntValue(int64(getoptlong.ReturnInOrder))
	errClass := newClass("GetoptLong::Error", object.Kind[*RClass](vm.consts["StandardError"]))
	c.consts["Error"] = errClass
	c.consts["AmbiguousOption"] = newClass("GetoptLong::AmbiguousOption", errClass)
	c.consts["NeedlessArgument"] = newClass("GetoptLong::NeedlessArgument", errClass)
	c.consts["MissingArgument"] = newClass("GetoptLong::MissingArgument", errClass)
	c.consts["InvalidOption"] = newClass("GetoptLong::InvalidOption", errClass)

	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			g := &GetoptLong{
				p:         getoptlong.NewParser(),
				argv:      vm.argvArray(),
				invalid:   object.Kind[*RClass](c.consts["InvalidOption"]),
				missing:   object.Kind[*RClass](c.consts["MissingArgument"]),
				needless:  object.Kind[*RClass](c.consts["NeedlessArgument"]),
				ambiguous: object.Kind[*RClass](c.consts["AmbiguousOption"]),
			}
			// GetoptLong.new(spec, spec, ...): each spec is an array of name/alias
			// strings followed by the argument-kind constant. set_options does the
			// same conversion and validation, so reuse it (it raises ArgumentError on
			// a malformed spec, exactly like MRI's GetoptLong.new).
			vm.golSetOptions(g, args)
			return g
		}}

	d := func(name string, fn NativeFn) { c.define(name, fn) }

	d("set_options", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		g := golOf(v)
		vm.golSetOptions(g, args)
		return v
	})

	// get / get_option scan one option: ["--name", arg] (arg is "" for a flag or an
	// absent optional argument), or nil/[nil, nil] at the end of input. The library
	// leaves the operands in Parser.Args, which is mirrored back into ARGV after the
	// step (MRI consumes from ARGV as it scans).
	getFn := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		g := golOf(v)
		vm.golSeed(g)
		name, arg, ok, err := g.p.GetNext()
		vm.golSyncArgv(g)
		if err != nil {
			vm.golRaise(g, err)
		}
		if !ok {
			return object.NewArray(object.NilV, object.NilV)
		}
		return object.NewArray(object.NewString(name), object.NewString(arg))
	}
	d("get", getFn)
	d("get_option", getFn)

	// each / each_option iterate every option, yielding [name, arg] to the block,
	// then return self. A scanning error stops the iteration and raises the matching
	// GetoptLong::* exception (after writing the partial leftover back into ARGV).
	eachFn := func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		g := golOf(v)
		vm.golSeed(g)
		err := g.p.Each(func(name, arg string) {
			vm.golSyncArgv(g)
			if blk != nil {
				vm.callBlock(blk, []object.Value{object.NewString(name), object.NewString(arg)})
			}
		})
		vm.golSyncArgv(g)
		if err != nil {
			vm.golRaise(g, err)
		}
		return v
	}
	d("each", eachFn)
	d("each_option", eachFn)

	// ordering= selects the scanning mode (REQUIRE_ORDER / PERMUTE / RETURN_IN_ORDER);
	// ordering reads it back. An out-of-range value raises ArgumentError, as in MRI.
	d("ordering=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		g := golOf(v)
		if err := g.p.SetOrdering(getoptlong.Ordering(intArg(args[0]))); err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return args[0]
	})
	d("ordering", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(golOf(v).p.Ordering()))
	})

	// quiet= / quiet? suppress / report the stderr error reporting; a quiet parser
	// still raises the exception, it only stops writing the message.
	d("quiet=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		golOf(v).p.SetQuiet(args[0].Truthy())
		return args[0]
	})
	quietFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(golOf(v).p.Quiet())
	}
	d("quiet", quietFn)
	d("quiet?", quietFn)

	// error / error? report the error that terminated processing (the Ruby exception
	// class, or nil); error_message reports its message.
	d("error", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		g := golOf(v)
		if e := g.p.Err(); e != nil {
			return vm.golErrClass(g, e.Kind)
		}
		return object.NilV
	})
	d("error?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(golOf(v).p.Err() != nil)
	})
	d("error_message", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if msg := golOf(v).p.ErrorMessage(); msg != "" {
			return object.NewString(msg)
		}
		return object.NilV
	})

	// terminated? reports whether the scan has finished (input exhausted, a `--`
	// terminator was hit, or an error stopped it).
	d("terminated?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(golOf(v).p.Terminated())
	})
}

// argvArray returns the global argument Array (the object bound to ARGV and $*),
// the slice GetoptLong scans and mutates.
func (vm *VM) argvArray() *object.Array {
	if a, ok := object.KindOK[*object.Array](vm.consts["ARGV"]); ok {
		return a
	}
	// ARGV is always installed as an Array at boot; fall back to an empty one so a
	// host that re-bound it never panics here.
	return object.NewArray()
}

// golSetOptions converts each Ruby spec array (name/alias strings followed by the
// argument-kind constant) into a library Option and installs them, raising
// ArgumentError on a malformed spec — MRI's GetoptLong.new / #set_options surface.
func (vm *VM) golSetOptions(g *GetoptLong, specs []object.Value) {
	opts := make([]getoptlong.Option, 0, len(specs))
	for _, s := range specs {
		arr, ok := object.KindOK[*object.Array](s)
		if !ok {
			raise("ArgumentError", "the option list contains non-array argument")
		}
		var names []string
		flag := getoptlong.NoArgument
		for _, e := range arr.Elems {
			{
				__sw62 := e
				switch {
				case object.IsKind[*object.String](__sw62):
					t := object.Kind[*object.String](__sw62)
					_ = t
					names = append(names, t.Str())
				case object.IsInt(__sw62):
					t := object.AsInteger(__sw62)
					_ = t
					flag = getoptlong.ArgumentFlag(t)
				default:
					t := __sw62
					_ = t
					raise("ArgumentError", "the option list contains an invalid element")
				}
			}
		}
		opts = append(opts, getoptlong.Option{Names: names, Flag: flag})
	}
	if err := g.p.SetOptions(opts...); err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
}

// golSeed binds the parser's working slice to a snapshot of ARGV on the first
// scanning call (MRI scans ARGV lazily, so a program may replace ARGV between
// GetoptLong.new and the first #get / #each). Subsequent calls keep the parser's
// own progressively-consumed Args.
func (vm *VM) golSeed(g *GetoptLong) {
	if g.seeded {
		return
	}
	g.argv = vm.argvArray()
	g.p.Args = stringsOf(g.argv)
	g.seeded = true
}

// golSyncArgv writes the parser's remaining arguments back into the ARGV Array
// object in place, so a program reading ARGV / $* after (or during) scanning sees
// the leftover operands — MRI's destructive scan of the global ARGV.
func (vm *VM) golSyncArgv(g *GetoptLong) {
	setArgv(g.argv, g.p.Args)
}

// golErrClass maps a library ErrorKind to the Ruby GetoptLong::* class.
func (vm *VM) golErrClass(g *GetoptLong, kind getoptlong.ErrorKind) *RClass {
	switch kind {
	case getoptlong.MissingArgument:
		return g.missing
	case getoptlong.NeedlessArgument:
		return g.needless
	case getoptlong.AmbiguousOption:
		return g.ambiguous
	default: // InvalidOption
		return g.invalid
	}
}

// golRaise turns a library scanning error into the matching Ruby GetoptLong::*
// exception, constructed through the nested class so the raised object carries the
// right class (for `rescue GetoptLong::InvalidOption`) and MRI-identical message.
// GetNext / Each only ever fail with a *getoptlong.Error (each of the four kinds),
// so the error always maps to a concrete GetoptLong::* class.
func (vm *VM) golRaise(g *GetoptLong, err error) {
	e := err.(*getoptlong.Error)
	cls := vm.golErrClass(g, e.Kind)
	exc := vm.send(cls, "new", []object.Value{object.NewString(e.Message)}, nil)
	panic(vm.excError(vm.captureBacktrace(exc)))
}
