// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// methodNameArg coerces a method-name argument the way MRI's rb_to_id does for
// send / public_send / __send__: a Symbol or String is taken directly, any other
// object is converted through #to_str (which must return a String), and anything
// else — nil, a number, a plain object — raises TypeError. Unlike ivarNameArg it
// imposes no '@' shape on the result.
func (vm *VM) methodNameArg(arg object.Value) string {
	switch a := arg.(type) {
	case object.Symbol:
		return string(a)
	case *object.String:
		return a.Str()
	default:
		if vm.respondsToDynamic(arg, "to_str") {
			if s, ok := vm.send(arg, "to_str", nil, nil).(*object.String); ok {
				return s.Str()
			}
		}
		raise("TypeError", "%s is not a symbol nor a string", vm.inspectStr(arg))
		return ""
	}
}
