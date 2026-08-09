package vm

import (
	"bytes"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestBindingSourceLocationFile drives Binding#source_location for a file-backed
// binding: the black-box harness compiles from a bare string (no File), so the
// non-nil branch — [file, line] with the VM's line-0 convention — is asserted
// here against a Binding carrying an explicit file.
func TestBindingSourceLocationFile(t *testing.T) {
	vm := New(&bytes.Buffer{})
	m := lookupMethod(vm.consts["Binding"].(*RClass), "source_location")
	if m == nil || m.native == nil {
		t.Fatal("Binding#source_location not registered")
	}
	got := m.native(vm, &Binding{file: "app.rb"}, nil, nil)
	arr, ok := got.(*object.Array)
	if !ok || len(arr.Elems) != 2 {
		t.Fatalf("source_location = %v, want a 2-element array", got)
	}
	if s, ok := arr.Elems[0].(*object.String); !ok || s.Str() != "app.rb" {
		t.Errorf("file = %v, want \"app.rb\"", arr.Elems[0])
	}
	if arr.Elems[1] != object.IntValue(0) {
		t.Errorf("line = %v, want 0", arr.Elems[1])
	}
}

// TestBindingDisplayMarkers exercises the Binding display/predicate markers
// directly: Inspect and Truthy are not reachable through normal Ruby flow with
// observable output (inspect prints via ToS, and a Binding is always truthy),
// so they are asserted here in-package.
func TestBindingDisplayMarkers(t *testing.T) {
	b := &Binding{}
	if got := b.Inspect(); got != "#<Binding>" {
		t.Errorf("Inspect = %q, want %q", got, "#<Binding>")
	}
	if got := b.ToS(); got != "#<Binding>" {
		t.Errorf("ToS = %q, want %q", got, "#<Binding>")
	}
	if !b.Truthy() {
		t.Error("a Binding must be truthy")
	}
}
