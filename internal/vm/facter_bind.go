// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"sort"
	"strings"

	facter "github.com/go-ruby-facter/facter"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// facterNameArg reads the single fact-name argument common to Facter.value /
// Facter[] / Facter.fact / Facter.add, raising ArgumentError when it is missing.
func facterNameArg(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return nameArg(args[0])
}

// facterIntArg reads an integer argument (has_weight n), treating a non-integer
// as zero.
func facterIntArg(v object.Value) int {
	if i, ok := v.(object.Integer); ok {
		return int(i)
	}
	return 0
}

// facterValue resolves a fact name through the per-VM adapter, returning nil for
// an absent fact and the converted value otherwise.
func (vm *VM) facterValue(name string) object.Value {
	v, ok := vm.facterFacter.Value(name)
	if !ok {
		return object.NilV
	}
	return facterValueToRuby(v)
}

// facterFactValue resolves a Fact handle's value, nil when it does not resolve.
func facterFactValue(ft *facter.Fact) object.Value {
	v, ok := ft.Value()
	if !ok {
		return object.NilV
	}
	return facterValueToRuby(v)
}

// facterRegister turns a configured FacterResolution into a go-ruby-facter
// resolution: has_weight and confines become Options, and the setcode block runs
// INLINE on the VM goroutine under the GVL (no Timeout, so the adapter never runs
// the Ruby block on a side goroutine). A string setcode runs through the adapter's
// execution seam. It returns the Fact handle for the registered fact.
func (vm *VM) facterRegister(name string, res *FacterResolution) *facter.Fact {
	opts := facter.Options{Confine: res.confines}
	if res.hasWeight {
		opts.Weight = res.weight
		opts.HasWeight = true
	}
	code := res.code
	command := res.command
	hasCmd := res.hasCmd
	return vm.facterFacter.Add(name, opts, func(ctx *facter.ResolutionContext) (any, bool) {
		switch {
		case code != nil:
			return rubyToGoValue(vm.callBlock(code, nil)), true
		case hasCmd:
			out, ok := ctx.Execute(command)
			if !ok {
				return nil, false
			}
			return strings.TrimRight(out, "\n"), true
		}
		return nil, false
	})
}

// addConfine appends a confine to the resolution for each supported Ruby form:
//   - confine fact => value      (a Hash of fact/value guards)
//   - confine(:fact) { |v| … }   (a predicate over the fact's value)
//   - confine { … }              (a bare boolean predicate)
//
// Predicate blocks run INLINE under the GVL during resolution gating.
func (r *FacterResolution) addConfine(vm *VM, args []object.Value, blk *Proc) {
	for _, a := range args {
		if h, ok := a.(*object.Hash); ok {
			for _, k := range h.Keys {
				v, _ := h.Get(k)
				r.confines = append(r.confines, facter.ConfineFact(k.ToS(), rubyToGoValue(v)))
			}
			return
		}
	}
	if blk == nil {
		return
	}
	if len(args) > 0 {
		name := nameArg(args[0])
		code := blk
		r.confines = append(r.confines, facter.ConfineFactFunc(name, func(v any) bool {
			return vm.callBlock(code, []object.Value{facterValueToRuby(v)}).Truthy()
		}))
		return
	}
	code := blk
	r.confines = append(r.confines, facter.ConfineBlock(func(*facter.Facter) bool {
		return vm.callBlock(code, nil).Truthy()
	}))
}

// facterValueToRuby maps a fact value — the shapes go-facter emits (string, bool,
// integral and float numbers, []string / []any lists, and string- or any-valued
// maps for structured facts like os/networking) — to its Ruby counterpart,
// recursing through containers with deterministically ordered Hash keys.
func facterValueToRuby(v any) object.Value {
	switch x := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(x)
	case string:
		return object.NewString(x)
	case int:
		return object.IntValue(int64(x))
	case int64:
		return object.IntValue(x)
	case uint64:
		return object.IntValue(int64(x))
	case float64:
		return object.Float(x)
	case []string:
		elems := make([]object.Value, len(x))
		for i, s := range x {
			elems[i] = object.NewString(s)
		}
		return object.NewArrayFromSlice(elems)
	case []any:
		elems := make([]object.Value, len(x))
		for i, e := range x {
			elems[i] = facterValueToRuby(e)
		}
		return object.NewArrayFromSlice(elems)
	case map[string]string:
		h := object.NewHash()
		for _, k := range facterSortedKeys(x) {
			h.Set(object.NewString(k), object.NewString(x[k]))
		}
		return h
	case map[string]any:
		h := object.NewHash()
		for _, k := range facterSortedKeys(x) {
			h.Set(object.NewString(k), facterValueToRuby(x[k]))
		}
		return h
	}
	return object.NewString(fmt.Sprint(v))
}

// facterSortedKeys returns a map's keys in sorted order so structured facts
// convert to Ruby Hashes deterministically.
func facterSortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
