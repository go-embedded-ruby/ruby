// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	augeas "github.com/go-ruby-augeas/augeas"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// augStrArg reads the i-th argument as a string, treating a missing argument or
// nil as the empty string (so Augeas.open(nil, nil) works like the gem).
func augStrArg(args []object.Value, i int) string {
	if i >= len(args) || object.IsNil(args[i]) {
		return ""
	}
	return args[i].ToS()
}

// augIntArg reads the i-th argument as an int, treating a missing argument or a
// non-integer as zero.
func augIntArg(args []object.Value, i int) int {
	if i < len(args) {
		if n, ok := args[i].(object.Integer); ok {
			return int(n)
		}
	}
	return 0
}

// augPathArg reads the mandatory first path argument, raising ArgumentError when
// it is missing.
func augPathArg(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return args[0].ToS()
}

// augRaise turns a non-nil engine error into an Augeas::Error.
func augRaise(err error) {
	if err != nil {
		raise("Augeas::Error", "%s", err.Error())
	}
}

// augLens resolves a built-in lens by name, raising Augeas::Error when the name
// is unknown.
func augLens(name string) augeas.Lens {
	l, ok := augeas.LensByName(name)
	if !ok {
		raise("Augeas::Error", "no such lens: %s", name)
	}
	return l
}

// registerAugeasMethods installs the Augeas instance surface.
func (vm *VM) registerAugeasMethods(cls *RClass) {
	self := func(v object.Value) *augeas.Augeas { return v.(*AugeasObj).a }

	// get(path) — the value at path, or nil when the node is absent / has none.
	cls.define("get", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if val, ok := self(v).Get(augPathArg(args)); ok {
			return object.NewString(val)
		}
		return object.NilV
	})

	// exists?(path) — whether a node matches path.
	cls.define("exists?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Exists(augPathArg(args)))
	})

	// set(path, value) — set (creating the node); Augeas::Error on a bad path.
	cls.define("set", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		augRaise(self(v).Set(args[0].ToS(), args[1].ToS()))
		return object.NilV
	})

	// setm(base, sub, value) — set every base/sub node; returns the count.
	cls.define("setm", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 3)", len(args))
		}
		n, err := self(v).Setm(args[0].ToS(), args[1].ToS(), args[2].ToS())
		augRaise(err)
		return object.IntValue(int64(n))
	})

	// insert(path, label, before=true) — insert a sibling; Augeas::Error on a bad
	// path.
	cls.define("insert", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
		}
		before := true
		if len(args) > 2 {
			before = args[2].Truthy()
		}
		augRaise(self(v).Insert(args[0].ToS(), args[1].ToS(), before))
		return object.NilV
	})

	// rm(path) — remove every matching node; returns the count removed.
	cls.define("rm", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Rm(augPathArg(args))))
	})

	// mv(src, dst) — move a subtree; Augeas::Error on a bad path.
	cls.define("mv", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		augRaise(self(v).Mv(args[0].ToS(), args[1].ToS()))
		return object.NilV
	})

	// match(path) — the paths of every matching node.
	cls.define("match", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		paths := self(v).Match(augPathArg(args))
		out := make([]object.Value, len(paths))
		for i, p := range paths {
			out[i] = object.NewString(p)
		}
		return object.NewArrayFromSlice(out)
	})

	// defvar(name, expr) — bind a variable to a path expression; returns the
	// number of nodes it matches. Augeas::Error on a bad expression.
	cls.define("defvar", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		n, err := self(v).Defvar(args[0].ToS(), args[1].ToS())
		augRaise(err)
		return object.IntValue(int64(n))
	})

	// defnode(name, expr, value) — bind a variable, creating the node if the
	// expression matches nothing; returns whether a node was created.
	cls.define("defnode", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 3)", len(args))
		}
		_, created := self(v).Defnode(args[0].ToS(), args[1].ToS(), args[2].ToS())
		return object.Bool(created)
	})

	// label(path) — the final label of the node at path, or nil.
	cls.define("label", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if lbl, ok := self(v).Label(augPathArg(args)); ok {
			return object.NewString(lbl)
		}
		return object.NilV
	})

	// text_store(lens, path, text) — parse text with the named built-in lens and
	// store the resulting tree at path. Augeas::Error on an unknown lens or a
	// parse failure.
	cls.define("text_store", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 3)", len(args))
		}
		augRaise(self(v).TextStore(augLens(args[0].ToS()), args[1].ToS(), args[2].ToS()))
		return object.NilV
	})

	// text_retrieve(lens, path) — serialise the subtree at path with the named
	// built-in lens back to text. Augeas::Error on an unknown lens or a failure.
	cls.define("text_retrieve", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		out, err := self(v).TextRetrieve(augLens(args[0].ToS()), args[1].ToS(), nil)
		augRaise(err)
		return object.NewString(out)
	})

	// error — the last engine error message, or nil.
	cls.define("error", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).Error(); err != nil {
			return object.NewString(err.Error())
		}
		return object.NilV
	})

	// root / load_path / flags — the construction parameters recorded on the
	// handle.
	cls.define("root", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Root())
	})
	cls.define("load_path", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).LoadPath())
	})
	cls.define("flags", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Flags()))
	})
}
