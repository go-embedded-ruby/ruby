// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"math/big"

	scanflib "github.com/go-ruby-scanf/scanf"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-scanf/scanf — the pure-Go, MRI-4.0.5
// faithful port of Ruby's `scanf` stdlib — into rbgo. The library owns the
// directive parser and the input→value conversion (the inverse of sprintf):
// given an input string and a scanf format it returns the converted Go values
// (int / *big.Int / float64 / string), stopping at the first non-match, at end
// of input, or when the format is exhausted, exactly as MRI does. This file is
// the thin shell that maps the library's []any result onto rbgo's value model
// (Integer / Float / String) and installs the Ruby-visible surface that
// `require "scanf"` adds: String#scanf (and its block form), IO#scanf and the
// top-level Kernel#scanf. rbgo had no prior scanf implementation, so this is a
// pure addition (no inline scanf existed to replace).
//
// MRI's scanf has no binary (%b) directive — only %d/%u/%i, %x/%X, %o, the
// float forms %a/%e/%f/%g, %s, %c, %[...]/%[^...] sets and %% — and the library
// mirrors that, so this binding adds nothing of its own: a "%b" in the format is
// just the literal characters '%' and 'b' to be matched, the same as MRI.

// registerScanf records the require "scanf" feature hook. Nothing is installed
// eagerly: the hook (run once by doRequire on the first `require "scanf"`) adds
// String#scanf, IO#scanf and Kernel#scanf, mirroring MRI where lib/scanf.rb
// defines them only when loaded. Before then `"".respond_to?(:scanf)` is false.
// The featureHooks map is already created by registerPrime (which runs first),
// so this only records the hook.
func (vm *VM) registerScanf() {
	vm.featureHooks["scanf"] = vm.installScanf
}

// installScanf installs the core extensions `require "scanf"` adds — the body
// MRI's lib/scanf.rb runs on load:
//
//   - String#scanf(format)            — scan self once, returning the values.
//   - String#scanf(format){ |grp| }   — scan self repeatedly, yielding each
//     group and collecting the block results.
//   - IO#scanf(format) (and the block form) — scan the stream's remaining
//     buffered input.
//   - Kernel#scanf(format) — IO#scanf on $stdin (the top-level form).
func (vm *VM) installScanf() {
	vm.cString.define("scanf", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.doScanf(strArg(self), strArg(args[0]), blk)
	})

	scanfIO := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		o := self.(*IOObj)
		o.pipeRefresh()
		input := string(o.buf[min(o.pos, len(o.buf)):])
		o.pos = len(o.buf) // scanf consumes the stream's remaining input
		return vm.doScanf(input, strArg(args[0]), blk)
	}
	if cIO, ok := vm.consts["IO"].(*RClass); ok {
		cIO.define("scanf", scanfIO)
	}
	if cStringIO, ok := vm.consts["StringIO"].(*RClass); ok {
		cStringIO.define("scanf", scanfIO)
	}

	// Kernel#scanf(format) reads from $stdin — the top-level `scanf("%d")` form.
	vm.cObject.define("scanf", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		stdin, ok := vm.globals["$stdin"].(*IOObj)
		if !ok {
			return &object.Array{}
		}
		return scanfIO(vm, stdin, args, blk)
	})
}

// doScanf is the shared core: with no block it runs the format once (the
// library's Scan) and returns the converted values as a Ruby Array; with a block
// it runs the format repeatedly (the library's ScanAll), yields each group as a
// single Array argument (so {|a,b|} auto-splats and {|grp|} gets the whole
// group, as in MRI) and returns the Array of block results.
func (vm *VM) doScanf(input, format string, blk *Proc) object.Value {
	if blk == nil {
		vals, _ := scanflib.Scan(input, format)
		return scanfValues(vals)
	}
	groups, _ := scanflib.ScanAll(input, format)
	results := make([]object.Value, 0, len(groups))
	for _, g := range groups {
		results = append(results, vm.callBlock(blk, []object.Value{scanfValues(g)}))
	}
	return &object.Array{Elems: results}
}

// scanfValues maps a library result group ([]any of int / *big.Int / float64 /
// string) onto a Ruby Array of Integer / Float / String.
func scanfValues(vals []any) object.Value {
	elems := make([]object.Value, 0, len(vals))
	for _, v := range vals {
		elems = append(elems, scanfValue(v))
	}
	return &object.Array{Elems: elems}
}

// scanfValue maps one library value onto its Ruby counterpart. The library
// emits int and *big.Int for integers (small vs arbitrary precision), float64
// for floats and string for strings; the default arm is a defensive fallback
// for any other Go value, rendered with Go's %v, so the conversion is total.
func scanfValue(v any) object.Value {
	switch n := v.(type) {
	case int:
		return object.NormInt(big.NewInt(int64(n)))
	case *big.Int:
		return object.NormInt(n)
	case float64:
		return object.Float(n)
	case string:
		return object.NewString(n)
	default:
		return object.NewString(fmt.Sprintf("%v", n))
	}
}
