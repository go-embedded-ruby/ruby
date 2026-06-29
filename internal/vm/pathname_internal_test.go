// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"testing"
)

// TestRegisterPathnameNoClass covers the guard in registerPathname for a host
// that stripped the prelude-defined Pathname class: there is nothing to back, so
// registerPathname must return without panicking (and without recreating the
// class). The prelude always defines Pathname, so this branch is only reachable
// by deleting the constant first.
func TestRegisterPathnameNoClass(t *testing.T) {
	vm := New(&bytes.Buffer{})
	delete(vm.consts, "Pathname")
	vm.registerPathname() // must be a no-op, not a panic
	if _, ok := vm.consts["Pathname"]; ok {
		t.Fatal("registerPathname re-created Pathname when the class was absent")
	}
}
