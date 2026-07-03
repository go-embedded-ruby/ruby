package vm

import (
	"bytes"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestRubyPlatformFor covers the arch/os normalisations of rubyPlatformFor on
// any host (the live build target only exercises one combination).
func TestRubyPlatformFor(t *testing.T) {
	cases := []struct{ arch, os, want string }{
		{"amd64", "linux", "x86_64-linux"},
		{"386", "linux", "i686-linux"},
		{"arm64", "darwin", "arm64-darwin"},
		{"wasm", "js", "wasm-wasm"},
		{"wasm", "wasip1", "wasm-wasm"},
		{"riscv64", "linux", "riscv64-linux"},
	}
	for _, c := range cases {
		if got := rubyPlatformFor(c.arch, c.os); got != c.want {
			t.Errorf("rubyPlatformFor(%q,%q)=%q want %q", c.arch, c.os, got, c.want)
		}
	}
}

// TestRunAtExitOneRepanic checks that a non-RubyError control-flow signal raised
// inside an at_exit hook is re-panicked (not swallowed) by runAtExitOne.
func TestRunAtExitOneRepanic(t *testing.T) {
	vm := New(&bytes.Buffer{})
	blk := &Proc{native: func(_ *VM, _ []object.Value) object.Value {
		panic(throwSignal{tag: object.NilVal(), value: object.NilVal()})
	}}
	defer func() {
		r := recover()
		if _, ok := r.(throwSignal); !ok {
			t.Fatalf("expected throwSignal to propagate, got %v", r)
		}
	}()
	vm.runAtExitOne(blk)
	t.Fatal("runAtExitOne should have re-panicked the throwSignal")
}
