// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerOpen3 installs a loadable shell for the Open3 module (require
// "open3"). Several Puppet providers require open3 at load but only spawn a
// subprocess (Open3.popen3/capture2/capture3) when that provider actually
// manages a resource on a matching platform. Spawning and wiring three pipes to
// a child is the subprocess subsystem, planned separately; for now the module
// exists so the require succeeds and the spawning entry points raise
// NotImplementedError if ever invoked.
func (vm *VM) registerOpen3() {
	mod := newClass("Open3", nil)
	mod.isModule = true
	vm.consts["Open3"] = object.Wrap(mod)

	notImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "Open3.%s not yet supported (subprocess spawning pending)", what)
		}
	}
	for _, m := range []string{
		"popen3", "popen2", "popen2e",
		"capture3", "capture2", "capture2e",
		"pipeline", "pipeline_r", "pipeline_w", "pipeline_rw", "pipeline_start",
	} {
		mod.smethods[m] = &Method{name: m, owner: mod, native: notImpl(m)}
	}
}
