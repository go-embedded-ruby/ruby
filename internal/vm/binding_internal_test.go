package vm

import "testing"

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
