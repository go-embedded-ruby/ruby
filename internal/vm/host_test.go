package vm_test

import (
	"bytes"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

// runProg parses, compiles and runs src on a fresh VM seeded with the given
// constants — the embedding path a host (CLI / wasm) drives. It returns the
// captured stdout and any error.
func runProg(t *testing.T, src string, seed map[string]object.Value) (string, error) {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	m := vm.New(&buf)
	for k, v := range seed {
		m.SetConst(k, v)
	}
	_, err = m.Run(iseq)
	return buf.String(), err
}

// TestSetConst covers the embedding-host seam: a constant installed with
// SetConst is visible to a subsequently-run program as a bare constant — this
// is how the wasm playground hands INPUT to Ruby image code.
func TestSetConst(t *testing.T) {
	out, err := runProg(t, `puts SEED.upcase`, map[string]object.Value{
		"SEED": object.NewString("hi"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "HI\n" {
		t.Fatalf("got %q, want %q", out, "HI\n")
	}
}
