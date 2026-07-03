// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// envSeams drives the ENV constant against an in-memory environment so every
// branch is exercised deterministically and identically on every platform
// (Linux/macOS/Windows), never touching or depending on the real process
// environment. It returns the installed VM and the backing map.
func envSeams(t *testing.T) (*VM, map[string]string) {
	t.Helper()
	store := map[string]string{}
	origGet, origLook, origSet, origUnset, origEnv :=
		envGetenv, envLookup, envSetenv, envUnsetenv, envEnviron
	t.Cleanup(func() {
		envGetenv, envLookup, envSetenv, envUnsetenv, envEnviron =
			origGet, origLook, origSet, origUnset, origEnv
	})
	// order preserves a stable iteration for the each/keys assertions.
	var order []string
	envGetenv = func(k string) string { return store[k] }
	envLookup = func(k string) (string, bool) { v, ok := store[k]; return v, ok }
	envSetenv = func(k, v string) error {
		if _, ok := store[k]; !ok {
			order = append(order, k)
		}
		store[k] = v
		return nil
	}
	envUnsetenv = func(k string) error {
		delete(store, k)
		for i, kk := range order {
			if kk == k {
				order = append(order[:i], order[i+1:]...)
				break
			}
		}
		return nil
	}
	envEnviron = func() []string {
		out := make([]string, 0, len(order))
		for _, k := range order {
			out = append(out, k+"="+store[k])
		}
		return out
	}
	return New(nil), store
}

// callEnv invokes an ENV instance method by name through its class method table.
func callEnv(t *testing.T, vm *VM, name string, args []object.Value, blk *Proc) object.Value {
	t.Helper()
	env := object.Kind[*RObject](vm.consts["ENV"])
	m := lookupMethod(env.class, name)
	if m == nil || m.native == nil {
		t.Fatalf("ENV#%s not found", name)
	}
	return m.native(vm, object.Wrap(env), args, blk)
}

func TestENVReadWrite(t *testing.T) {
	vm, store := envSeams(t)
	s := func(x string) object.Value { return object.Wrap(object.NewString(x)) }

	// []= sets and returns the assigned value; [] reads it back.
	if got := callEnv(t, vm, "[]=", []object.Value{s("A"), s("1")}, nil); got.ToS() != "1" {
		t.Fatalf("[]= returned %v", got)
	}
	if got := callEnv(t, vm, "[]", []object.Value{s("A")}, nil); got.ToS() != "1" {
		t.Fatalf("[] returned %v", got)
	}
	// store is an alias of []=.
	callEnv(t, vm, "store", []object.Value{s("B"), s("2")}, nil)
	if store["B"] != "2" {
		t.Fatalf("store did not write B")
	}
	// Missing key reads as nil.
	if _, ok := object.AsNilOK(callEnv(t, vm, "[]", []object.Value{s("MISSING")}, nil)); !ok {
		t.Fatalf("missing key not nil")
	}
	// []= with nil deletes the key, returning nil.
	if got := callEnv(t, vm, "[]=", []object.Value{s("A"), object.NilVal()}, nil); got != object.NilV {
		t.Fatalf("[]=nil returned %v", got)
	}
	if _, ok := store["A"]; ok {
		t.Fatalf("[]=nil did not delete A")
	}
}

func TestENVPredicates(t *testing.T) {
	vm, _ := envSeams(t)
	s := func(x string) object.Value { return object.Wrap(object.NewString(x)) }
	callEnv(t, vm, "[]=", []object.Value{s("K"), s("v")}, nil)

	for _, name := range []string{"include?", "key?", "has_key?"} {
		if callEnv(t, vm, name, []object.Value{s("K")}, nil) != object.True {
			t.Fatalf("%s(K) not true", name)
		}
		if callEnv(t, vm, name, []object.Value{s("NOPE")}, nil) != object.False {
			t.Fatalf("%s(NOPE) not false", name)
		}
	}
}

func TestENVFetch(t *testing.T) {
	vm, _ := envSeams(t)
	s := func(x string) object.Value { return object.Wrap(object.NewString(x)) }
	callEnv(t, vm, "[]=", []object.Value{s("K"), s("v")}, nil)

	// present key
	if got := callEnv(t, vm, "fetch", []object.Value{s("K")}, nil); got.ToS() != "v" {
		t.Fatalf("fetch present: %v", got)
	}
	// default argument
	if got := callEnv(t, vm, "fetch", []object.Value{s("NOPE"), s("d")}, nil); got.ToS() != "d" {
		t.Fatalf("fetch default: %v", got)
	}
	// block form (rbgo-level, exercises callBlock) over the in-memory env.
	if got := runSrc(t, `p ENV.fetch("__NOPE_GER__"){|k| "blk:#{k}"}`); got != "\"blk:__NOPE_GER__\"" {
		t.Fatalf("fetch block: %q", got)
	}
	// missing, no default, no block -> KeyError, matching MRI.
	c := catchRaise(func() { callEnv(t, vm, "fetch", []object.Value{s("NOPE")}, nil) })
	if c != "KeyError" {
		t.Fatalf("fetch KeyError: got %q", c)
	}
}

func TestENVDeleteAndClear(t *testing.T) {
	vm, store := envSeams(t)
	s := func(x string) object.Value { return object.Wrap(object.NewString(x)) }
	callEnv(t, vm, "[]=", []object.Value{s("D"), s("x")}, nil)
	callEnv(t, vm, "[]=", []object.Value{s("E"), s("y")}, nil)

	// delete returns the previous value...
	if got := callEnv(t, vm, "delete", []object.Value{s("D")}, nil); got.ToS() != "x" {
		t.Fatalf("delete returned %v", got)
	}
	// ...and nil for an absent key.
	if got := callEnv(t, vm, "delete", []object.Value{s("ABSENT")}, nil); got != object.NilV {
		t.Fatalf("delete absent returned %v", got)
	}
	// clear empties the whole environment and returns self.
	self := callEnv(t, vm, "clear", nil, nil)
	if _, ok := object.KindOK[*RObject](self); !ok {
		t.Fatalf("clear did not return self object")
	}
	if len(store) != 0 {
		t.Fatalf("clear left %d keys", len(store))
	}
}

func TestENVHashViews(t *testing.T) {
	vm, _ := envSeams(t)
	s := func(x string) object.Value { return object.Wrap(object.NewString(x)) }
	callEnv(t, vm, "[]=", []object.Value{s("H1"), s("a")}, nil)
	callEnv(t, vm, "[]=", []object.Value{s("H2"), s("b")}, nil)

	for _, name := range []string{"to_hash", "to_h"} {
		h, ok := object.KindOK[*object.Hash](callEnv(t, vm, name, nil, nil))
		if !ok {
			t.Fatalf("%s not a Hash", name)
		}
		if v, _ := h.Get(s("H1")); v.ToS() != "a" {
			t.Fatalf("%s missing H1", name)
		}
	}

	keys := object.Kind[*object.Array](callEnv(t, vm, "keys", nil, nil))
	if len(keys.Elems) != 2 {
		t.Fatalf("keys = %v", keys.Elems)
	}
}

func TestENVEach(t *testing.T) {
	vm, _ := envSeams(t)

	// success path (block invoked per pair) via rbgo so callBlock runs for real,
	// over the deterministic in-memory env installed by envSeams.
	got := runSrc(t, `
ENV.clear
ENV["EA"]="1"; ENV["EB"]="2"
acc=[]; ENV.each{|k,v| acc << "#{k}=#{v}"}
p acc.sort`)
	if got != `["EA=1", "EB=2"]` {
		t.Fatalf("each: %q", got)
	}

	// no-block path raises LocalJumpError.
	c := catchRaise(func() { callEnv(t, vm, "each", nil, nil) })
	if c != "LocalJumpError" {
		t.Fatalf("each no-block: got %q", c)
	}
}

func TestENVMergeReplaceKey(t *testing.T) {
	vm, store := envSeams(t)
	s := func(x string) object.Value { return object.Wrap(object.NewString(x)) }
	callEnv(t, vm, "[]=", []object.Value{s("OLD"), s("keep")}, nil)

	h := object.NewHash()
	h.Set(s("M1"), s("a"))
	h.Set(s("M2"), s("b"))

	// merge! keeps existing keys and adds the new ones; returns self.
	for _, name := range []string{"merge!", "update"} {
		self := callEnv(t, vm, name, []object.Value{object.Wrap(h)}, nil)
		if _, ok := object.KindOK[*RObject](self); !ok {
			t.Fatalf("%s did not return self", name)
		}
	}
	if store["OLD"] != "keep" || store["M1"] != "a" {
		t.Fatalf("merge! state: %v", store)
	}

	// key(value) returns the matching key, else nil.
	if got := callEnv(t, vm, "key", []object.Value{s("a")}, nil); got.ToS() != "M1" {
		t.Fatalf("key(a) = %v", got)
	}
	if got := callEnv(t, vm, "key", []object.Value{s("__nope__")}, nil); got != object.NilV {
		t.Fatalf("key(nope) = %v", got)
	}

	// replace clears, then sets only the new pairs; returns self.
	r := object.NewHash()
	r.Set(s("R1"), s("z"))
	self := callEnv(t, vm, "replace", []object.Value{object.Wrap(r)}, nil)
	if _, ok := object.KindOK[*RObject](self); !ok {
		t.Fatalf("replace did not return self")
	}
	if _, ok := store["OLD"]; ok {
		t.Fatalf("replace did not clear OLD")
	}
	if store["R1"] != "z" {
		t.Fatalf("replace did not set R1")
	}
}

func TestENVMergeTypeError(t *testing.T) {
	vm, _ := envSeams(t)
	// mergeEnvHash on a non-Hash argument raises TypeError, matching MRI.
	c := catchRaise(func() {
		callEnv(t, vm, "merge!", []object.Value{object.IntValue(int64(object.Integer(5)))}, nil)
	})
	if c != "TypeError" {
		t.Fatalf("merge! non-hash: got %q", c)
	}
}
