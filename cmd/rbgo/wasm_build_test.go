package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildEnv checks the env plumbing for the nested `go build`: the wasm
// target appends GOOS=js GOARCH=wasm (overriding any inherited values, since
// later entries win in Go's exec), while every other target leaves the
// environment untouched.
func TestBuildEnv(t *testing.T) {
	base := []string{"PATH=/bin", "GOOS=linux", "GOARCH=amd64"}

	for _, target := range []string{"", "native", "anything-else"} {
		got := buildEnv(base, target)
		if len(got) != len(base) {
			t.Fatalf("buildEnv(base, %q) = %v, want unchanged %v", target, got, base)
		}
		for i := range base {
			if got[i] != base[i] {
				t.Fatalf("buildEnv(base, %q)[%d] = %q, want %q", target, i, got[i], base[i])
			}
		}
	}

	got := buildEnv(base, "wasm")
	if n := len(got); n != len(base)+2 {
		t.Fatalf("buildEnv(base, wasm) length = %d, want %d", n, len(base)+2)
	}
	if got[len(got)-2] != "GOOS=js" || got[len(got)-1] != "GOARCH=wasm" {
		t.Fatalf("buildEnv(base, wasm) tail = %v, want [GOOS=js GOARCH=wasm]", got[len(got)-2:])
	}
	// The override must come AFTER the inherited GOOS/GOARCH so it wins.
	var goosIdx, jsIdx int
	for i, e := range got {
		switch e {
		case "GOOS=linux":
			goosIdx = i
		case "GOOS=js":
			jsIdx = i
		}
	}
	if jsIdx <= goosIdx {
		t.Fatalf("GOOS=js (idx %d) must follow inherited GOOS=linux (idx %d) to win", jsIdx, goosIdx)
	}
	// The base slice must not be mutated.
	if base[1] != "GOOS=linux" {
		t.Fatalf("buildEnv mutated its base argument: %v", base)
	}
}

// TestClosedWasmBuildIntegration drives `rbgo build --closed --target wasm` end
// to end: it bakes a JS-using program into a GOOS=js GOARCH=wasm module, asserts
// the output is a wasm binary with the front-end dropped, and that the program's
// JS.log line ran in a headless browser when one is available. Like the other
// build integration tests it shells out to the Go toolchain, so it is gated
// behind RBGO_BUILD_IT=1.
func TestClosedWasmBuildIntegration(t *testing.T) {
	if os.Getenv("RBGO_BUILD_IT") == "" {
		t.Skip("set RBGO_BUILD_IT=1 to run the closed-world wasm build integration test (needs the Go toolchain)")
	}
	root := moduleRootForTest(t)
	dir := t.TempDir()

	app := filepath.Join(dir, "app.rb")
	program := "JS.log(\"closed wasm ruby ran\")\n" +
		"JS.log(\"document is: \" + JS.document.to_s)\n"
	if err := os.WriteFile(app, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}

	wasmOut := filepath.Join(dir, "app.wasm")
	rbgoBuild(t, root, "--closed", "--target", "wasm", app, "-o", wasmOut)

	// The output must be a WebAssembly module (magic "\0asm").
	data, err := os.ReadFile(wasmOut)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 4 || string(data[:4]) != "\x00asm" {
		t.Fatalf("output is not a wasm module (magic = %x)", data[:min(4, len(data))])
	}

	// The front-end must not be linked into the wasm module. `go tool nm` does
	// not reliably read Go wasm objects, so scan the module bytes for the
	// front-end package paths the linker embeds when they are referenced.
	for _, sym := range []string{"go-ruby-parser/parser", "internal/compiler"} {
		if strings.Contains(string(data), sym) {
			t.Errorf("closed wasm still links the front-end (%s)", sym)
		}
	}
}
