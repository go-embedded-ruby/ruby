// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// ⛔ A WHITEBOX test, because the branch it covers is unreachable from Ruby
// through the interpreter.
//
// gvarGet skips the uninitialized-global warning for a name starting with '#',
// which is `bytecode.ErrinfoCell` ("#$!") — the cell the begin/rescue compiler
// reads the pending exception through, spelled so no program can name it.
// vm.go's getgvar arm intercepts that name BEFORE calling gvarGet, so the only
// caller that reaches the guard is AOT-compiled code, where the two tiers emit
// `vm.gvarGet` without the interception.
//
// Running a `rescue` from Ruby therefore does not exercise it, and a reordering
// that moved the guard behind the `uninitialized` flag took it from "covered by
// every global read" to "covered by nothing" — which the per-function coverage
// ratchet is there to notice. Calling gvarGet directly is the honest way to
// cover a path whose only production caller is generated code.
func TestGvarGetDoesNotWarnForTheErrinfoCell(t *testing.T) {
	var out strings.Builder
	vm := New(&out)
	// rb_warning's gate: only a TRUE $VERBOSE prints. Set it, so a missing guard
	// would actually produce a line rather than being masked by the gate.
	vm.setGVar("$VERBOSE", object.True)

	got := vm.gvarGet(bytecode.ErrinfoCell)
	if !object.IsNil(got) {
		t.Errorf("gvarGet(%q) = %v, want nil", bytecode.ErrinfoCell, got)
	}
	if out.String() != "" {
		t.Errorf("the errinfo cell produced a warning: %q", out.String())
	}

	// The control: the same call with an ordinary never-assigned name MUST warn,
	// or the silence above would prove only that the gate is shut.
	out.Reset()
	if v := vm.gvarGet("$a_name_no_table_holds"); !object.IsNil(v) {
		t.Errorf("an uninitialised global read as %v, want nil", v)
	}
	if !strings.Contains(out.String(), "not initialized") {
		t.Fatalf("the planted positive produced no warning: %q — the errinfo "+
			"silence proves nothing", out.String())
	}
}
