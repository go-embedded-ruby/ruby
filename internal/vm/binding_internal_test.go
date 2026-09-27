package vm

import (
	"bytes"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestBindingSourceLocationFile drives Binding#source_location for a file-backed
// binding: the black-box harness compiles from a bare string (no File), so the
// non-nil branch — [file, line] — is asserted here against a Binding carrying an
// explicit file and line.
//
// It used to pin line 0, which was rbgo's own limitation rather than a fact about
// Ruby: bind_location returns INT2FIX(bind->first_lineno) (proc.c
// ruby_4_0:805-815), and `def m; binding; end; p m.source_location` on ruby 4.0.5
// reports the capture LINE. The frame's live pc now supplies it.
func TestBindingSourceLocationFile(t *testing.T) {
	vm := New(&bytes.Buffer{})
	m := lookupMethod(vm.consts["Binding"].(*RClass), "source_location")
	if m == nil || m.native == nil {
		t.Fatal("Binding#source_location not registered")
	}
	got := m.native(vm, &Binding{file: "app.rb", line: 12}, nil, nil)
	arr, ok := got.(*object.Array)
	if !ok || len(arr.Elems) != 2 {
		t.Fatalf("source_location = %v, want a 2-element array", got)
	}
	if s, ok := arr.Elems[0].(*object.String); !ok || s.Str() != "app.rb" {
		t.Errorf("file = %v, want \"app.rb\"", arr.Elems[0])
	}
	if arr.Elems[1] != object.IntValue(12) {
		t.Errorf("line = %v, want 12", arr.Elems[1])
	}
}

// TestFrameBindingWalksOutward covers frameBinding's two structural cases in
// package: a frame whose scope exec has NOT published is skipped, and an index
// with no such frame below it falls back to the top-level binding. MRI reaches the
// same two answers through rb_vm_get_binding_creatable_next_cfp, which walks
// outward past a frame that cannot make a binding (vm_core.h ruby_4_0:1993).
func TestFrameBindingWalksOutward(t *testing.T) {
	vm := New(&bytes.Buffer{})

	// Nothing on the stack: the only scope that exists is the top level's.
	b := vm.frameBinding(-1)
	if b == nil || b.self != vm.main || b.file != toplevelBindingFile {
		t.Fatalf("frameBinding(-1) = %+v, want the top-level binding", b)
	}
	if got := vm.callerBinding(); got == nil {
		t.Fatal("callerBinding must never be nil")
	}

	// Two frames, only the OUTER one carrying a scope: the walk must skip the inner
	// one and answer with the outer env, not with the top-level fallback.
	outerEnv := &Env{slots: []object.Value{object.IntValue(7)}}
	vm.frameCode = []frameCode{
		{iseq: &bytecode.ISeq{File: "outer.rb", Locals: []string{"a"}}, env: outerEnv, self: vm.main, definee: vm.cObject},
		{iseq: &bytecode.ISeq{File: "inner.rb"}},
	}
	b = vm.frameBinding(1)
	if b.env != outerEnv {
		t.Errorf("env = %p, want the outer frame's %p", b.env, outerEnv)
	}
	if b.file != "outer.rb" || len(b.names) != 1 || b.names[0] != "a" {
		t.Errorf("binding = {file:%q names:%v}, want outer.rb with [a]", b.file, b.names)
	}

	// An index past the end clamps rather than panicking: myFrame can legitimately
	// outrun frameCode when a thread body has run through the Run boundary.
	if got := vm.frameBinding(99); got.file != "outer.rb" {
		t.Errorf("frameBinding(99) file = %q, want the clamped top frame's outer.rb", got.file)
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
