package vm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-embedded-ruby/ruby/internal/parser"
	"github.com/go-embedded-ruby/ruby/internal/vm"
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

// TestNativeFaultBecomesRubyError covers the embedding safety net: a native
// binding that indexes past a missing argument raises a Go runtime fault, which
// callNative converts into a rescuable Ruby ArgumentError rather than crashing
// the host. NDArray.arange needs (start, stop); one argument trips it.
func TestNativeFaultBecomesRubyError(t *testing.T) {
	_, err := runProg(t, `NDArray.arange(6)`, nil)
	if err == nil {
		t.Fatal("expected an error, got none")
	}
	if re, ok := err.(vm.RubyError); !ok || re.Class != "ArgumentError" {
		t.Fatalf("got %#v, want an ArgumentError", err)
	}
	if !strings.Contains(err.Error(), "index out of range") {
		t.Fatalf("error %q should describe the underlying fault", err.Error())
	}
	// Rescuable from Ruby, and the VM keeps running afterwards.
	if got := eval(t, `p (begin; NDArray.arange(6); rescue ArgumentError; "caught"; end)`); got != "\"caught\"\n" {
		t.Fatalf("rescue got %q, want %q", got, "\"caught\"\n")
	}
}
