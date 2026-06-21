package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// runSrc runs a Ruby program through a fresh VM and returns its stdout trimmed.
func runSrc(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	if _, err := New(&buf).Run(iseq); err != nil {
		t.Fatalf("run: %v", err)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// register installs a compiled body for the test and removes it afterwards so it
// cannot leak into the package-global registry seen by other tests.
func register(t *testing.T, key string, fn CompiledMethod) {
	t.Helper()
	RegisterCompiled(key, fn)
	t.Cleanup(func() { delete(compiledRegistry, key) })
}

// TestAOTDispatchPrefersCompiled proves invoke() takes the registered compiled
// body in preference to the interpreted ISeq: the Ruby body returns 1, the
// compiled body returns a sentinel, and the sentinel is what runs.
func TestAOTDispatchPrefersCompiled(t *testing.T) {
	register(t, "Object#sentinel", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(999)
	})
	if got := runSrc(t, "def sentinel = 1\np sentinel"); got != "999" {
		t.Errorf("compiled body not preferred: got %q, want 999", got)
	}
}

// TestAOTDispatchRealCodegen wires the actual AOT-generated fib body (e2eFib,
// emitted by aotgen) into normal method dispatch and confirms it computes the
// reference result — real lowered Go running through the interpreter's seam.
func TestAOTDispatchRealCodegen(t *testing.T) {
	register(t, "Object#aotfib", (*VM).e2eFib)
	if got := runSrc(t, "def aotfib(n) = n < 2 ? n : aotfib(n - 1) + aotfib(n - 2)\np aotfib(10)"); got != "55" {
		t.Errorf("compiled fib through dispatch = %q, want 55", got)
	}
}

// TestAOTDispatchDeoptOnRedef checks a redefinition drops the compiled body: the
// first definition runs compiled (sentinel), the redefinition is interpreted.
func TestAOTDispatchDeoptOnRedef(t *testing.T) {
	register(t, "Object#g", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(999)
	})
	if got := runSrc(t, "def g = 1\np g\ndef g = 2\np g"); got != "999\n2" {
		t.Errorf("redefinition should deopt to interpreter: got %q, want \"999\\n2\"", got)
	}
}

// TestAOTDispatchUnregistered confirms a method with no registered body is simply
// interpreted (the registry lookup misses).
func TestAOTDispatchUnregistered(t *testing.T) {
	if got := runSrc(t, "def h = 7\np h"); got != "7" {
		t.Errorf("unregistered method = %q, want 7", got)
	}
}
