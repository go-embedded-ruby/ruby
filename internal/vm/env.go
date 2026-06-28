// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Process-environment access goes through these seams so tests drive ENV
// deterministically (and identically on every platform) instead of mutating the
// real process environment.
var (
	envGetenv   = os.Getenv
	envLookup   = os.LookupEnv
	envSetenv   = os.Setenv
	envUnsetenv = os.Unsetenv
	envEnviron  = os.Environ
)

// registerENV installs the ENV constant: a Hash-like view of the process
// environment. Reads (ENV["x"], fetch, to_hash, include?) and writes (ENV["x"]=,
// delete, merge!, replace, clear) go through Go's os package, so ENV reflects and
// mutates the real environment exactly as MRI's ENV does. Values are Strings;
// a missing key reads as nil. ENV is its own singleton object (an ordinary
// RObject), matching MRI where ENV is the sole instance of an anonymous class.
func (vm *VM) registerENV() {
	cls := newClass("", vm.cObject) // ENV's class is anonymous in MRI too
	env := &RObject{class: cls, ivars: map[string]object.Value{}}
	vm.consts["ENV"] = env

	def := func(name string, fn NativeFn) { cls.define(name, fn) }

	get := func(key string) object.Value {
		if v, ok := envLookup(key); ok {
			return object.NewString(v)
		}
		return object.NilV
	}
	def("[]", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return get(strArg(args[0]))
	})
	def("[]=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		setEnvKV(strArg(args[0]), args[1])
		return args[1]
	})
	def("store", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		setEnvKV(strArg(args[0]), args[1])
		return args[1]
	})
	// fetch(key[, default]) { |key| ... }: the value, else the default / block
	// result, else a KeyError, exactly like Hash#fetch.
	def("fetch", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		key := strArg(args[0])
		if v, ok := envLookup(key); ok {
			return object.NewString(v)
		}
		switch {
		case blk != nil:
			return vm.callBlock(blk, []object.Value{object.NewString(key)})
		case len(args) > 1:
			return args[1]
		default:
			return raise("KeyError", "key not found: %q", key)
		}
	})
	def("include?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := envLookup(strArg(args[0]))
		return object.Bool(ok)
	})
	def("key?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := envLookup(strArg(args[0]))
		return object.Bool(ok)
	})
	def("has_key?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := envLookup(strArg(args[0]))
		return object.Bool(ok)
	})
	def("delete", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		key := strArg(args[0])
		prev := get(key)
		_ = envUnsetenv(key)
		return prev
	})
	def("clear", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		for _, kv := range envEnviron() {
			if i := strings.IndexByte(kv, '='); i >= 0 {
				_ = envUnsetenv(kv[:i])
			}
		}
		return self
	})
	toHash := func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return envHash()
	}
	def("to_hash", toHash)
	def("to_h", toHash)
	def("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		h := envHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			vm.callBlock(blk, []object.Value{k, v})
		}
		return self
	})
	// replace(hash): clear, then set every pair (String values), like MRI.
	def("replace", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		for _, kv := range envEnviron() {
			if i := strings.IndexByte(kv, '='); i >= 0 {
				_ = envUnsetenv(kv[:i])
			}
		}
		mergeEnvHash(args[0])
		return self
	})
	// merge!(hash) / update(hash): set every pair, keeping existing unmentioned
	// keys.
	merge := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mergeEnvHash(args[0])
		return self
	}
	def("merge!", merge)
	def("update", merge)
	def("key", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		want := strArg(args[0])
		for _, kv := range envEnviron() {
			if i := strings.IndexByte(kv, '='); i >= 0 && kv[i+1:] == want {
				return object.NewString(kv[:i])
			}
		}
		return object.NilV
	})
	def("keys", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		h := envHash()
		return &object.Array{Elems: append([]object.Value(nil), h.Keys...)}
	})
}

// setEnvKV sets or (on a nil value) deletes an environment variable.
func setEnvKV(key string, v object.Value) {
	if _, isNil := v.(object.Nil); isNil {
		_ = envUnsetenv(key)
		return
	}
	_ = envSetenv(key, v.ToS())
}

// envHash snapshots the process environment as a Ruby Hash of String→String.
func envHash() *object.Hash {
	h := object.NewHash()
	for _, kv := range envEnviron() {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			h.Set(object.NewString(kv[:i]), object.NewString(kv[i+1:]))
		}
	}
	return h
}

// mergeEnvHash sets each pair of a Ruby Hash into the process environment.
func mergeEnvHash(v object.Value) {
	h, ok := v.(*object.Hash)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Hash", classNameOf(v))
	}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		setEnvKV(k.ToS(), val)
	}
}
