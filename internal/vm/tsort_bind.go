// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	tsort "github.com/go-ruby-tsort/tsort"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-tsort/tsort — the pure-Go, MRI-4.0.5
// faithful port of Ruby's TSort stdlib — into rbgo. The library owns Tarjan's
// algorithm: the topological sort, the strongly-connected-component grouping and
// their ordering; this file is the thin shell that drives it from Ruby.
//
// MRI's TSort is a mixin: a class includes TSort and supplies tsort_each_node /
// tsort_each_child; the module's #tsort, #strongly_connected_components and
// #each_strongly_connected_component then work. The library describes a graph
// functionally with a NodesFunc and a ChildrenFunc, so each TSort instance
// method turns the includer's two callbacks into those functions — collecting
// the yielded nodes through a synthesized Go block (vm.send with a native Proc),
// exactly as the Enumerator helpers iterate. Node identity uses setKey (Ruby
// hash/eql?), and the Cyclic error renders nodes with Ruby #inspect, so both the
// grouping and the "topological sort failed" message match MRI.
//
// The module also offers the singleton form TSort.tsort(each_node, each_child)
// and friends, where the two callbacks are Procs/lambdas the caller supplies.
// rbgo had no prior TSort implementation, so this is a pure addition.

// registerTSort records the require "tsort" feature hook. Nothing is installed
// eagerly: the hook (run once by doRequire on the first `require "tsort"`)
// creates the TSort module, its TSort::Cyclic error and the instance/singleton
// methods, mirroring MRI where lib/tsort.rb defines them only when loaded. The
// featureHooks map is already created by registerPrime (which runs first), so
// this only records the hook.
func (vm *VM) registerTSort() {
	vm.featureHooks["tsort"] = vm.installTSort
}

// installTSort builds the TSort module — the body MRI's lib/tsort.rb runs on
// load: the TSort::Cyclic exception, the mixin instance methods (tsort,
// tsort_each, strongly_connected_components, each_strongly_connected_component,
// each_strongly_connected_component_from) and the like-named singleton methods.
func (vm *VM) installTSort() {
	mod := newClass("TSort", nil)
	mod.isModule = true
	vm.consts["TSort"] = mod

	std := vm.consts["StandardError"].(*RClass)
	cyclic := newClass("TSort::Cyclic", std)
	mod.consts["Cyclic"] = cyclic
	vm.consts["TSort::Cyclic"] = cyclic

	// --- mixin instance methods (self supplies tsort_each_node/child) ---

	// nodesFromSelf yields the includer's nodes via self.tsort_each_node, building
	// the NodesFunc the library consumes plus the key->value recovery table.
	d := func(name string, fn NativeFn) { mod.define(name, fn) }

	d("tsort", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		nodes, children, vals := tsortFuncs(vm, self)
		return tsortResult(vm, nodes, children, vals)
	})
	// tsort_each { |node| ... } yields the sorted nodes one at a time (or returns
	// the sorted Array via tsort when given no block — MRI returns an Enumerator,
	// but the Array is the documented value the block iterates).
	d("tsort_each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		nodes, children, vals := tsortFuncs(vm, self)
		arr := tsortResult(vm, nodes, children, vals).(*object.Array)
		if blk == nil {
			return enumFor(self, "tsort_each")
		}
		for _, n := range arr.Elems {
			vm.callBlock(blk, []object.Value{n})
		}
		return object.NilV
	})
	d("strongly_connected_components", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		nodes, children, vals := tsortFuncs(vm, self)
		return sccArray(tsort.StronglyConnectedComponentsWith(nodes, children, tsortOpts(vals)), vals)
	})
	d("each_strongly_connected_component", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each_strongly_connected_component")
		}
		nodes, children, vals := tsortFuncs(vm, self)
		tsort.EachStronglyConnectedComponentWith(nodes, children, tsortOpts(vals), func(comp []any) {
			vm.callBlock(blk, []object.Value{compArray(comp, vals)})
		})
		return object.NilV
	})
	d("each_strongly_connected_component_from", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each_strongly_connected_component_from", args...)
		}
		_, children, vals := tsortFuncs(vm, self)
		start := setKey(args[0])
		vals[start] = args[0]
		tsort.EachStronglyConnectedComponentFromWith(start, children, tsortOpts(vals), func(comp []any) {
			vm.callBlock(blk, []object.Value{compArray(comp, vals)})
		})
		return object.NilV
	})

	// --- singleton methods (callbacks supplied as Procs) ---
	sm := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	sm("tsort", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		nodes, children, vals := tsortProcFuncs(vm, args[0], args[1])
		return tsortResult(vm, nodes, children, vals)
	})
	sm("strongly_connected_components", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		nodes, children, vals := tsortProcFuncs(vm, args[0], args[1])
		return sccArray(tsort.StronglyConnectedComponentsWith(nodes, children, tsortOpts(vals)), vals)
	})
	sm("each_strongly_connected_component", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		nodes, children, vals := tsortProcFuncs(vm, args[0], args[1])
		tsort.EachStronglyConnectedComponentWith(nodes, children, tsortOpts(vals), func(comp []any) {
			vm.callBlock(blk, []object.Value{compArray(comp, vals)})
		})
		return object.NilV
	})
	sm("each_strongly_connected_component_from", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		children, vals := tsortChildProc(vm, args[1])
		start := setKey(args[0])
		vals[start] = args[0]
		tsort.EachStronglyConnectedComponentFromWith(start, children, tsortOpts(vals), func(comp []any) {
			vm.callBlock(blk, []object.Value{compArray(comp, vals)})
		})
		return object.NilV
	})
}

// tsortVals records, for each canonical node key, the original Ruby value, so
// results and Cyclic messages render the Ruby objects (not the keys).
type tsortVals map[any]object.Value

// tsortOpts builds the library Options for a run: Identity is the canonical key
// (already applied by the callers, so it is the identity here) and Inspect
// renders a node via the recovered Ruby value's #inspect, so a Cyclic message
// matches MRI byte-for-byte.
func tsortOpts(vals tsortVals) *tsort.Options {
	return &tsort.Options{
		Identity: func(node any) any { return node },
		// Every node the library inspects has been yielded through collectNode and
		// so is recorded in vals, so the lookup always hits.
		Inspect: func(node any) string { return vals[node].Inspect() },
	}
}

// tsortFuncs turns an includer's tsort_each_node / tsort_each_child into the
// library's NodesFunc / ChildrenFunc, sharing a key->value table. Each callback
// is driven by sending the Ruby method with a synthesized Go block that records
// the yielded node under its canonical key.
func tsortFuncs(vm *VM, self object.Value) (tsort.NodesFunc, tsort.ChildrenFunc, tsortVals) {
	vals := tsortVals{}
	nodes := func(yield func(any)) {
		vm.send(self, "tsort_each_node", nil, collectNode(vals, yield))
	}
	children := func(node any, yield func(any)) {
		vm.send(self, "tsort_each_child", []object.Value{vals[node]}, collectNode(vals, yield))
	}
	return nodes, children, vals
}

// tsortProcFuncs is the singleton form: the two callbacks are Procs the caller
// supplies (each responding to #call with a block, MRI's TSort.tsort(each_node,
// each_child)). They are invoked with a synthesized collecting block.
func tsortProcFuncs(vm *VM, eachNode, eachChild object.Value) (tsort.NodesFunc, tsort.ChildrenFunc, tsortVals) {
	children, vals := tsortChildProc(vm, eachChild)
	nodes := func(yield func(any)) {
		vm.send(eachNode, "call", nil, collectNode(vals, yield))
	}
	return nodes, children, vals
}

// tsortChildProc wraps a single each_child Proc into a ChildrenFunc sharing a
// fresh key->value table (also used by the _from singleton form, which has no
// node enumerator).
func tsortChildProc(vm *VM, eachChild object.Value) (tsort.ChildrenFunc, tsortVals) {
	vals := tsortVals{}
	children := func(node any, yield func(any)) {
		vm.send(eachChild, "call", []object.Value{vals[node]}, collectNode(vals, yield))
	}
	return children, vals
}

// collectNode builds a native block that records each yielded Ruby value under
// its canonical key and forwards the key to the library's yield.
func collectNode(vals tsortVals, yield func(any)) *Proc {
	return &Proc{native: func(_ *VM, args []object.Value) object.Value {
		v := args[0]
		k := setKey(v)
		vals[k] = v
		yield(k)
		return object.NilV
	}}
}

// tsortResult runs the topological sort and boxes the result, raising
// TSort::Cyclic (with MRI's exact message) when the graph has a cycle.
func tsortResult(vm *VM, nodes tsort.NodesFunc, children tsort.ChildrenFunc, vals tsortVals) object.Value {
	sorted, err := tsort.TSortWith(nodes, children, tsortOpts(vals))
	if err != nil {
		raise("TSort::Cyclic", "%s", err.Error())
	}
	return compArray(sorted, vals)
}

// compArray boxes one component / sorted slice of canonical keys back into a
// Ruby Array of the original node values.
func compArray(keys []any, vals tsortVals) object.Value {
	out := make([]object.Value, len(keys))
	for i, k := range keys {
		out[i] = vals[k]
	}
	return &object.Array{Elems: out}
}

// sccArray boxes the slice-of-components result into a Ruby Array of Arrays.
func sccArray(comps [][]any, vals tsortVals) object.Value {
	out := make([]object.Value, len(comps))
	for i, c := range comps {
		out[i] = compArray(c, vals)
	}
	return &object.Array{Elems: out}
}
