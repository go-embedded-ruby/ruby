// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"sort"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerModuleReflect adds the visibility-filtered instance-method listings
// Module#public_instance_methods / #private_instance_methods /
// #protected_instance_methods, matching MRI 3.4. Each takes an optional
// include-super flag (default true) and returns the names, as Symbols, of the
// methods with the requested access level.
func (vm *VM) registerModuleReflect() {
	listWithVis := func(want visibility) NativeFn {
		return func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			all := len(args) == 0 || args[0].Truthy()
			return object.NewArrayFromSlice(vm.methodNamesByVis(self.(*RClass), all, want))
		}
	}
	vm.cModule.define("public_instance_methods", listWithVis(visPublic))
	vm.cModule.define("private_instance_methods", listWithVis(visPrivate))
	vm.cModule.define("protected_instance_methods", listWithVis(visProtected))

	// Module#undefined_instance_methods: the names of the methods the receiver
	// itself has undefined with Module#undef_method — the "undefined" method
	// entries MRI keeps on the receiver, which hide an inherited or included
	// definition without touching the class that provides it. An ancestor's own
	// undefined entries are NOT reported: only the receiver's table is read.
	// Reference: ruby/ruby v3_4_0 vm_method.c rb_mod_undefined_instance_methods.
	vm.cModule.define("undefined_instance_methods", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		cls := self.(*RClass)
		var names []string
		for n, m := range cls.methods {
			if m.undefined {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		out := make([]object.Value, len(names))
		for i, n := range names {
			out[i] = object.Symbol(n)
		}
		return object.NewArrayFromSlice(out)
	})
}

// methodNamesByVis returns, as sorted Symbols, the names of self's instance
// methods whose effective visibility (honouring any per-class override) matches
// want. With all true the receiver's ancestors are included; otherwise only the
// methods introduced directly on the receiver are considered. The nearest
// definition of a name wins, so a re-visibilised override in a derived class
// shadows the inherited access level, and an `undef` tombstone hides the name.
func (vm *VM) methodNamesByVis(c *RClass, all bool, want visibility) []object.Value {
	return vm.methodNamesMatching(c, all, func(v visibility) bool { return v == want })
}

// methodNamesMatching returns, as sorted Symbols, the names of self's instance
// methods whose effective visibility (honouring any per-class override) satisfies
// keep. It is the shared walk behind public/private/protected_instance_methods
// (each a single-visibility match) and instance_methods (which keeps everything
// except private). With all true the receiver's ancestors are included; the
// nearest definition of a name wins and an `undef` tombstone hides the name.
func (vm *VM) methodNamesMatching(c *RClass, all bool, keep func(visibility) bool) []object.Value {
	seen := map[string]bool{}
	undef := map[string]bool{}
	var names []string
	classes := []*RClass{c}
	if all {
		classes = vm.ancestors(c)
	}
	for _, k := range classes {
		for n, m := range k.methods {
			if seen[n] || undef[n] {
				continue
			}
			if m.undefined {
				undef[n] = true
				continue
			}
			seen[n] = true
			if keep(instanceVisibility(c, n, m)) {
				names = append(names, n)
			}
		}
	}
	// A visibility override is an entry of c's OWN in MRI: `private :foo` naming an
	// inherited foo installs a ZSUPER method entry on c, so c.instance_methods(false)
	// and the per-level private/protected/public listings report it. Reference:
	// ruby/ruby v3_4_0 vm_method.c rb_export_method →
	// rb_add_method(klass, name, VM_METHOD_TYPE_ZSUPER, 0, visi).
	for n, vis := range c.visOverrides {
		if seen[n] || undef[n] {
			continue
		}
		seen[n] = true
		if keep(vis) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	out := make([]object.Value, len(names))
	for i, n := range names {
		out[i] = object.Symbol(n)
	}
	return out
}
