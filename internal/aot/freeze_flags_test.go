package aot

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
)

// TestWriteInstrFlags covers freeze.go:138 — writeInstr's `in.Flags != 0`
// branch. The Flags field was added for method-visibility sends (an explicit
// receiver sets FlagSendExplicit); a frozen program with such a send must emit
// the Flags key in the composite literal. The existing freeze suite never
// freezes an Instr with Flags set, so drive writeInstr directly.
func TestWriteInstrFlags(t *testing.T) {
	var b strings.Builder
	writeInstr(&b, bytecode.Instr{Op: bytecode.OpSend, A: 1, Flags: bytecode.FlagSendExplicit})
	got := b.String()
	if !strings.Contains(got, "Flags: 1") {
		t.Errorf("writeInstr omitted Flags: got %q, want it to contain %q", got, "Flags: 1")
	}

	// A zero-Flags instruction must NOT emit the key (the branch is skipped).
	var z strings.Builder
	writeInstr(&z, bytecode.Instr{Op: bytecode.OpSend, A: 1})
	if strings.Contains(z.String(), "Flags") {
		t.Errorf("writeInstr emitted Flags for a zero-Flags instr: %q", z.String())
	}
}
