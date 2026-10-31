// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/vm"
)

// skipOnCoreBuild is skipOnCore for the external test package: it skips a test
// that exercises something the rbgo_core build leaves out on purpose. The
// default build's lanes still run it.
func skipOnCoreBuild(t *testing.T, why string) {
	t.Helper()
	if vm.CoreBuild {
		t.Skip("not on the rbgo_core build: " + why)
	}
}
