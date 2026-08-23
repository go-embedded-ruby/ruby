package vm

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// TestRunsAProgramOnThisHost is the smallest claim an interpreter can make: a
// program with an observable effect has one. Parse, compile, run, and look for
// the string the program printed — no fixture, no golden file, nothing that can
// pass while the VM produces nothing.
//
// It is written to be run on every host the project targets, and the wasip1
// lane in ci.yml runs exactly this test under wazero. That is deliberate: a
// build gate proves the module links, not that it interprets. This one costs a
// couple of hundred milliseconds and would have caught, in CI, a wasm build
// that loads and then quietly does nothing.
//
// (It came out of a false alarm worth recording: rbgo appeared not to run
// programs at all under wasip1 — silent CLI, and this very test hanging inside
// vm.New. The stall was in the host runtime, wasmer 7.2.1, whose stopping point
// moved between identical runs; the same .wasm file passes under wazero in
// 0.07s. Hence the runtime named in CI.)
func TestRunsAProgramOnThisHost(t *testing.T) {
	prog, err := parser.Parse("puts 'HELLO-FROM-VM'\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(iseq.Insns) == 0 {
		t.Fatal("compiled to zero instructions")
	}

	var out strings.Builder
	m := New(&out)
	if _, err := m.Run(iseq); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "HELLO-FROM-VM") {
		t.Fatalf("the program produced no output: %q (%d instructions)", got, len(iseq.Insns))
	}
}
