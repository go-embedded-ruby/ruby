// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerGetoptLong installs a loadable shell for GetoptLong (require
// "getoptlong"). Puppet references the argument-kind constants and constructs a
// GetoptLong at load of its settings, but the actual option scanning runs only
// when a CLI application parses argv. The argument-kind constants are real; the
// scanning methods (#each / #get / #set_options) raise NotImplementedError until
// the option-parser round.
func (vm *VM) registerGetoptLong() {
	c := newClass("GetoptLong", vm.cObject)
	vm.consts["GetoptLong"] = c
	c.consts["NO_ARGUMENT"] = object.Integer(0)
	c.consts["REQUIRED_ARGUMENT"] = object.Integer(1)
	c.consts["OPTIONAL_ARGUMENT"] = object.Integer(2)
	c.consts["Error"] = newClass("GetoptLong::Error", vm.consts["StandardError"].(*RClass))
	c.consts["InvalidOption"] = newClass("GetoptLong::InvalidOption", c.consts["Error"].(*RClass))
	c.consts["MissingArgument"] = newClass("GetoptLong::MissingArgument", c.consts["Error"].(*RClass))

	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &RObject{class: c, ivars: map[string]object.Value{}}
		}}
	notImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "GetoptLong#%s not yet supported (option parsing pending)", what)
		}
	}
	for _, m := range []string{"each", "get", "get_option", "set_options", "each_option"} {
		c.define(m, notImpl(m))
	}
}
