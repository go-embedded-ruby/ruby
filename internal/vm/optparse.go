// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerOptParse installs a loadable shell for the optparse standard library
// (require "optparse"). A complete OptionParser is a substantial parser in its
// own right; for now the class and its error tree exist so `require "optparse"`
// completes and load-time references resolve (Puppet requires optparse at load
// but only builds and runs a parser from method bodies). The declaration surface
// that merely records options without parsing — on / on_tail / on_head /
// separator / banner= — is real and chainable, while the methods that actually
// consume argv (parse / parse! / order / order! / permute / permute!) raise
// NotImplementedError until the real parser lands.
func (vm *VM) registerOptParse() {
	std := vm.consts["StandardError"].(*RClass)

	op := newClass("OptionParser", vm.cObject)
	vm.consts["OptionParser"] = op
	// MRI also exposes the short alias OptParse for OptionParser.
	vm.consts["OptParse"] = op

	// Error tree: ParseError < StandardError, with the concrete subclasses raised
	// by a real parse (so `rescue OptionParser::InvalidOption` resolves).
	parseErr := newClass("OptionParser::ParseError", std)
	op.consts["ParseError"] = parseErr
	for _, name := range []string{
		"InvalidOption", "MissingArgument", "InvalidArgument",
		"AmbiguousOption", "AmbiguousArgument", "NeedlessArgument",
	} {
		op.consts[name] = newClass("OptionParser::"+name, parseErr)
	}

	op.smethods["new"] = &Method{name: "new", owner: op,
		native: func(_ *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			o := &RObject{class: op, ivars: map[string]object.Value{}}
			if len(args) > 0 {
				o.ivars["@banner"] = object.NewString(args[0].ToS())
			}
			// A block form (OptionParser.new { |opts| ... }) yields the parser, as in
			// MRI, so option declarations inside the block run.
			if blk != nil {
				vm.callBlock(blk, []object.Value{o})
			}
			return o
		}}

	// Declaration methods: record nothing meaningful yet but accept any arguments
	// and a block, returning self so chaining works. on captures the block so a
	// future real parser can invoke it.
	for _, m := range []string{"on", "on_tail", "on_head", "separator", "accept", "reject"} {
		op.define(m, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return self
		})
	}
	op.define("banner", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@banner")
	})
	op.define("banner=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*RObject).ivars["@banner"] = args[0]
		return args[0]
	})

	// Parsing methods need real argv handling — the large subsystem — so they raise
	// until that lands.
	notImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "OptionParser#%s not yet supported (real argv parser pending)", what)
		}
	}
	for _, m := range []string{"parse", "parse!", "order", "order!", "permute", "permute!", "to_a", "help"} {
		op.define(m, notImpl(m))
	}
}
