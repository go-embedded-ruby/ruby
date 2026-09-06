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
//
// The method semantics follow MRI 3.4 hash.c (the env_* functions): names and
// values are coerced with StringValue (#to_str, else TypeError "no implicit
// conversion of X into String"); a name that is empty or contains '=' is invalid
// for a write and raises Errno::EINVAL (setenv(3) fails with EINVAL); returned
// strings are frozen (env_str_new freezes); and the block-less iteration/filter
// methods (each_pair, select, reject!, keep_if, …) return a sized Enumerator.
func (vm *VM) registerENV() {
	cls := newClass("", vm.cObject) // ENV's class is anonymous in MRI too
	env := &RObject{class: cls, ivars: map[string]object.Value{}}
	vm.consts["ENV"] = env

	def := func(name string, fn NativeFn) { cls.define(name, fn) }
	alias := func(newName, oldName string) { aliasBuiltin(cls, newName, oldName) }

	// A sized Enumerator whose #size reports the current environment size,
	// matching MRI's RETURN_SIZED_ENUMERATOR(ehash, 0, 0, rb_env_size).
	sizedEnum := func(self object.Value, meth string) *Enumerator {
		return enumForSized(self, meth, func(*VM) object.Value {
			return object.IntValue(int64(envCount()))
		})
	}

	// ENV[name] -> the (frozen) value, or nil. The name is coerced with #to_str;
	// an empty or '='-bearing name simply has no value (getenv returns NULL).
	def("[]", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return envGet(vm.envStr(args[0]))
	})
	// ENV[name] = value / ENV.store(name, value): a nil value deletes the name;
	// otherwise both name and value are coerced with #to_str and the variable is
	// set (Errno::EINVAL for an empty or '='-bearing name). Returns value.
	def("[]=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.envAssign(vm.envStr(args[0]), args[1])
		return args[1]
	})
	alias("store", "[]=")
	// fetch(name[, default]) { |name| … }: the value, else the block result, else
	// the default, else a KeyError — exactly like Hash#fetch. A block given with a
	// default warns, as in MRI.
	def("fetch", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		key := vm.envStr(args[0])
		if blk != nil && len(args) > 1 {
			vm.rbWarn("warning: block supersedes default value argument")
		}
		if v, ok := envLookup(key); ok {
			return envStrV(v)
		}
		switch {
		case blk != nil:
			return vm.callBlock(blk, []object.Value{object.NewString(key)})
		case len(args) > 1:
			return args[1]
		default:
			ks := object.NewString(key)
			vm.raiseWithIvars("KeyError", "key not found: "+ks.Inspect(),
				map[string]object.Value{"@key": ks, "@receiver": vm.consts["ENV"]})
			return object.NilV
		}
	})
	// include?/has_key?/member?/key?(name): whether the (coerced) name is set. An
	// empty or '='-bearing name is simply absent (no error); a non-String without
	// #to_str raises TypeError.
	def("include?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := envLookup(vm.envStr(args[0]))
		return object.Bool(ok)
	})
	alias("has_key?", "include?")
	alias("member?", "include?")
	alias("key?", "include?")
	// key(value): the name of the first variable whose value equals value, or nil.
	def("key", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		want := vm.envStr(args[0])
		for _, kv := range envEnviron() {
			if i := strings.IndexByte(kv, '='); i >= 0 && kv[i+1:] == want {
				return envStrV(kv[:i])
			}
		}
		return object.NilV
	})
	// value?/has_value?(value): whether value is some variable's value. An
	// un-coercible argument yields nil (rb_check_string_type), not false.
	def("value?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		want, ok := vm.envCheckStr(args[0])
		if !ok {
			return object.NilV
		}
		for _, kv := range envEnviron() {
			if i := strings.IndexByte(kv, '='); i >= 0 && kv[i+1:] == want {
				return object.True
			}
		}
		return object.False
	})
	alias("has_value?", "value?")
	// delete(name) { |name| … }: deletes name and returns its former value; if the
	// name was absent, returns the block result (or nil with no block).
	def("delete", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		key := vm.envStr(args[0])
		prev := envGet(key)
		if _, isNil := prev.(object.Nil); isNil {
			if blk != nil {
				return vm.callBlock(blk, []object.Value{object.NewString(key)})
			}
			return object.NilV
		}
		_ = envUnsetenv(key)
		return prev
	})
	def("clear", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		envClearAll()
		return self
	})
	def("keys", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		var out []object.Value
		for _, kv := range envEnviron() {
			if i := strings.IndexByte(kv, '='); i >= 0 {
				out = append(out, envStrV(kv[:i]))
			}
		}
		return object.NewArrayFromSlice(out)
	})
	def("values", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		var out []object.Value
		for _, kv := range envEnviron() {
			if i := strings.IndexByte(kv, '='); i >= 0 {
				out = append(out, envStrV(kv[i+1:]))
			}
		}
		return object.NewArrayFromSlice(out)
	})
	// values_at(*names): the value for each name (nil for a missing one). Each name
	// is coerced with #to_str.
	def("values_at", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		out := make([]object.Value, len(args))
		for i, a := range args {
			out[i] = envGet(vm.envStr(a))
		}
		return object.NewArrayFromSlice(out)
	})
	toHash := func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return envHash()
	}
	def("to_hash", toHash)
	// to_h: a copy as a Hash; with a block, each [name, value] pair is mapped
	// through the block to a [key, value] pair (as Hash#to_h does).
	def("to_h", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		h := envHash()
		if blk == nil {
			return h
		}
		out := object.NewHash()
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			res := vm.callBlock(blk, []object.Value{k, v})
			pair, ok := res.(*object.Array)
			if !ok && vm.respondsToDynamic(res, "to_ary") {
				pair, ok = vm.send(res, "to_ary", nil, nil).(*object.Array)
			}
			if !ok {
				raise("TypeError", "wrong element type %s (expected array)", vm.classOf(res).name)
			}
			if len(pair.Elems) != 2 {
				raise("ArgumentError", "element has wrong array length (expected 2, was %d)", len(pair.Elems))
			}
			out.Set(pair.Elems[0], pair.Elems[1])
		}
		return out
	})
	// each_pair/each { |name, value| … }: yields every pair; returns self. Without
	// a block, a sized Enumerator.
	def("each_pair", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return sizedEnum(self, "each_pair")
		}
		for _, p := range envPairs() {
			vm.callBlock(blk, []object.Value{p[0], p[1]})
		}
		return self
	})
	alias("each", "each_pair")
	def("each_key", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return sizedEnum(self, "each_key")
		}
		for _, p := range envPairs() {
			vm.callBlock(blk, []object.Value{p[0]})
		}
		return self
	})
	def("each_value", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return sizedEnum(self, "each_value")
		}
		for _, p := range envPairs() {
			vm.callBlock(blk, []object.Value{p[1]})
		}
		return self
	})
	// select/filter { |name, value| … } -> Hash of the pairs the block keeps.
	def("select", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return sizedEnum(self, "select")
		}
		out := object.NewHash()
		for _, p := range envPairs() {
			if vm.callBlock(blk, []object.Value{p[0], p[1]}).Truthy() {
				out.Set(p[0], p[1])
			}
		}
		return out
	})
	alias("filter", "select")
	// select!/filter! { |name, value| … }: delete the pairs the block rejects;
	// returns self if anything changed, else nil.
	def("select!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return sizedEnum(self, "select!")
		}
		return envSelectBang(vm, self, blk)
	})
	alias("filter!", "select!")
	// reject { |name, value| … } -> Hash of the pairs the block does NOT keep.
	def("reject", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return sizedEnum(self, "reject")
		}
		out := object.NewHash()
		for _, p := range envPairs() {
			if !vm.callBlock(blk, []object.Value{p[0], p[1]}).Truthy() {
				out.Set(p[0], p[1])
			}
		}
		return out
	})
	// reject! { |name, value| … }: delete the pairs the block keeps; returns self
	// if anything changed, else nil.
	def("reject!", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return sizedEnum(self, "reject!")
		}
		return envRejectBang(vm, self, blk)
	})
	// keep_if / delete_if: like select!/reject! but always return self.
	def("keep_if", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return sizedEnum(self, "keep_if")
		}
		envSelectBang(vm, self, blk)
		return self
	})
	def("delete_if", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return sizedEnum(self, "delete_if")
		}
		envRejectBang(vm, self, blk)
		return self
	})
	// slice(*names) -> Hash of the present names and their values. Each name is
	// coerced with #to_str for the lookup, but the original argument object is kept
	// as the result key (matching MRI env_slice / rb_f_getenv).
	def("slice", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		out := object.NewHash()
		for _, a := range args {
			if v, ok := envLookup(vm.envStr(a)); ok {
				out.Set(a, envStrV(v))
			}
		}
		return out
	})
	// except(*names) -> the full Hash with the given names removed. Names are
	// matched by Hash equality; they are not #to_str-coerced (matching MRI).
	def("except", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		h := envHash()
		for _, a := range args {
			h.Delete(a)
		}
		return h
	})
	// assoc(name) -> [name, value] or nil. The name is coerced with #to_str
	// (TypeError otherwise); the coerced name is the pair's first element.
	def("assoc", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		key := vm.envStr(args[0])
		if v, ok := envLookup(key); ok {
			return object.NewArray(object.NewString(key), envStrV(v))
		}
		return object.NilV
	})
	// rassoc(value) -> [name, value] of the first variable with that value, or nil.
	// An un-coercible value yields nil (rb_check_string_type), not an error.
	def("rassoc", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		want, ok := vm.envCheckStr(args[0])
		if !ok {
			return object.NilV
		}
		for _, kv := range envEnviron() {
			if i := strings.IndexByte(kv, '='); i >= 0 && kv[i+1:] == want {
				return object.NewArray(envStrV(kv[:i]), object.NewString(want))
			}
		}
		return object.NilV
	})
	// merge!(*hashes) / update(*hashes) { |name, old, new| … }: set every pair,
	// keeping unmentioned keys. With a block, an already-set name yields
	// (name, old_value, new_value) and the block result becomes the value. A nil
	// value deletes the name. Returns self.
	def("merge!", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		for _, a := range args {
			h := vm.toHash(a)
			for _, k := range h.Keys {
				v, _ := h.Get(k)
				key := vm.envStr(k)
				if blk != nil {
					if old, ok := envLookup(key); ok {
						v = vm.callBlock(blk, []object.Value{object.NewString(key), envStrV(old), v})
					}
				}
				vm.envAssign(key, v)
			}
		}
		return self
	})
	alias("update", "merge!")
	// replace(hash): make ENV hold exactly hash's pairs; returns self. Following
	// MRI env_replace, every new pair is set FIRST (so a bad name or value raises
	// before anything is removed, leaving ENV unchanged), and only then are the
	// pre-existing names not present in hash deleted.
	def("replace", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := vm.toHash(args[0])
		var oldKeys []string
		for _, kv := range envEnviron() {
			if i := strings.IndexByte(kv, '='); i >= 0 {
				oldKeys = append(oldKeys, kv[:i])
			}
		}
		newKeys := map[string]bool{}
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			key := vm.envStr(k)
			vm.envAssign(key, v)
			newKeys[key] = true
		}
		for _, key := range oldKeys {
			if !newKeys[key] {
				_ = envUnsetenv(key)
			}
		}
		return self
	})
	// shift -> [name, value] of the first variable, deleting it; nil when empty.
	def("shift", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		for _, kv := range envEnviron() {
			if i := strings.IndexByte(kv, '='); i >= 0 {
				key := kv[:i]
				val := kv[i+1:]
				_ = envUnsetenv(key)
				return object.NewArray(envStrV(key), envStrV(val))
			}
		}
		return object.NilV
	})
	// invert -> Hash mapping each value to its name (a later duplicate value wins).
	def("invert", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		out := object.NewHash()
		for _, p := range envPairs() {
			out.Set(p[1], p[0])
		}
		return out
	})
	// to_a -> Array of [name, value] pairs.
	def("to_a", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		var out []object.Value
		for _, p := range envPairs() {
			out = append(out, object.NewArray(p[0], p[1]))
		}
		return object.NewArrayFromSlice(out)
	})
	size := func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(envCount()))
	}
	def("size", size)
	alias("length", "size")
	def("empty?", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(envCount() == 0)
	})
	// inspect renders ENV like a Hash of its String pairs. It uses the same
	// Hash#inspect rendering (with the "key" => value spacing MRI 4.0.5 produces in
	// a normal environment), which is what the process ENV shows there and matches
	// byte-for-byte.
	def("inspect", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(envHash().Inspect())
	})
	def("to_s", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString("ENV")
	})
	def("rehash", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NilV
	})
	// clone/dup: ENV cannot be copied (it is a wrapper over the process
	// environment). clone validates a freeze: keyword first (ArgumentError for a
	// non-boolean value or an unknown keyword), then raises TypeError like dup.
	def("clone", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			if h, ok := args[len(args)-1].(*object.Hash); ok {
				for _, k := range h.Keys {
					if sym, isSym := k.(object.Symbol); isSym && sym == object.Symbol("freeze") {
						fv, _ := h.Get(k)
						switch fv.(type) {
						case object.Bool, object.Nil:
						default:
							raise("ArgumentError", "wrong argument type %s (must be boolean or nil)", vm.classOf(fv).name)
						}
						continue
					}
					raise("ArgumentError", "unknown keyword: %s", k.Inspect())
				}
			}
		}
		return raise("TypeError", "Cannot clone ENV, use ENV.to_h to get a copy of ENV as a hash")
	})
	def("dup", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return raise("TypeError", "Cannot dup ENV, use ENV.to_h to get a copy of ENV as a hash")
	})
}

// envStr coerces v to a Go string the way MRI's StringValue does for ENV
// names/values: a String is taken directly; otherwise #to_str is called and its
// String result used; anything else raises TypeError (mirroring env_name /
// env_aset).
func (vm *VM) envStr(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return s.Str()
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	return ""
}

// envCheckStr is MRI's rb_check_string_type applied to an ENV argument: like
// envStr but returns ("", false) instead of raising when v neither is nor
// converts to a String. ENV.value?/#rassoc use it (they answer nil for an
// un-coercible argument).
func (vm *VM) envCheckStr(v object.Value) (string, bool) {
	if s, ok := v.(*object.String); ok {
		return s.Str(), true
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return s.Str(), true
		}
	}
	return "", false
}

// envAssign sets (or, on a nil value, deletes) an environment variable whose name
// has already been coerced to keyStr. A non-nil value is coerced with #to_str;
// an empty or '='-bearing name raises Errno::EINVAL, exactly as setenv(3) fails.
func (vm *VM) envAssign(keyStr string, v object.Value) {
	if _, isNil := v.(object.Nil); isNil {
		_ = envUnsetenv(keyStr)
		return
	}
	valStr := vm.envStr(v)
	if keyStr == "" || strings.ContainsRune(keyStr, '=') {
		raise("Errno::EINVAL", "Invalid argument - setenv(%s)", keyStr)
	}
	_ = envSetenv(keyStr, valStr)
}

// envSelectBang deletes each pair for which blk returns false, returning self if
// any were deleted, else nil (MRI env_select_bang, shared by keep_if).
func envSelectBang(vm *VM, self object.Value, blk *Proc) object.Value {
	del := false
	for _, p := range envPairs() {
		if !vm.callBlock(blk, []object.Value{p[0], p[1]}).Truthy() {
			_ = envUnsetenv(p[0].Str())
			del = true
		}
	}
	if !del {
		return object.NilV
	}
	return self
}

// envRejectBang deletes each pair for which blk returns true, returning self if
// any were deleted, else nil (MRI env_reject_bang, shared by delete_if).
func envRejectBang(vm *VM, self object.Value, blk *Proc) object.Value {
	del := false
	for _, p := range envPairs() {
		if vm.callBlock(blk, []object.Value{p[0], p[1]}).Truthy() {
			_ = envUnsetenv(p[0].Str())
			del = true
		}
	}
	if !del {
		return object.NilV
	}
	return self
}

// envGet returns the frozen String value for key, or nil when it is unset.
func envGet(key string) object.Value {
	if v, ok := envLookup(key); ok {
		return envStrV(v)
	}
	return object.NilV
}

// envStrV wraps an environment string as a frozen Ruby String, matching MRI's
// env_str_new (every string ENV hands back is frozen).
func envStrV(s string) *object.String {
	str := object.NewString(s)
	str.Frozen = true
	return str
}

// envCount returns the number of environment variables.
func envCount() int {
	n := 0
	for _, kv := range envEnviron() {
		if strings.IndexByte(kv, '=') >= 0 {
			n++
		}
	}
	return n
}

// envClearAll unsets every environment variable.
func envClearAll() {
	for _, kv := range envEnviron() {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			_ = envUnsetenv(kv[:i])
		}
	}
}

// envPairs snapshots the environment as ordered [frozen-name, frozen-value]
// pairs, so an iteration is unaffected by deletions made in a block.
func envPairs() [][2]*object.String {
	var out [][2]*object.String
	for _, kv := range envEnviron() {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			out = append(out, [2]*object.String{envStrV(kv[:i]), envStrV(kv[i+1:])})
		}
	}
	return out
}

// envHash snapshots the process environment as a Ruby Hash of frozen
// String→String pairs (MRI env_to_hash).
func envHash() *object.Hash {
	h := object.NewHash()
	for _, p := range envPairs() {
		h.Set(p[0], p[1])
	}
	return h
}
