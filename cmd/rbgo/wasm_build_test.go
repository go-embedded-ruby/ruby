package main

import (
	"os"
	"os/exec"
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
// the output is a wasm binary with the front-end dropped, and RUNS it in a JS host
// so the baked program's JS.log lines are witnessed on the host's console.
//
// The run matters more than the build. A module that links is not a module that
// executes, and a compile-only check is what let issue #682 through: the wasip1
// lane proved `internal/vm` compiled for a wasm target for three days while the
// browser target did not even do that. Before this, that last clause said the
// JS.log line "ran in a headless browser when one is available" — no version of
// this function ever ran anything. The claim is now the code's, not the comment's.
//
// Like the other build integration tests it shells out to the Go toolchain, so it
// is gated behind RBGO_BUILD_IT=1 — which the ci.yml build-integration lane sets.
// node is a hard requirement of the gate rather than a reason to skip: a check
// that quietly steps aside when its host is missing is how the guard this test
// replaces came to be unreachable.
func TestClosedWasmBuildIntegration(t *testing.T) {
	if os.Getenv("RBGO_BUILD_IT") == "" {
		t.Skip("set RBGO_BUILD_IT=1 to run the closed-world wasm build integration test (needs the Go toolchain and node)")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("RBGO_BUILD_IT=1 needs node on PATH to run the js/wasm module it builds: %v", err)
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

	// Run it. JS.log is `console.log` (internal/vm/jsbridge_wasm.go), so a JS host
	// running the module to completion prints the baked program's two lines. The
	// glue is Go's own lib/wasm/wasm_exec_node.js from the toolchain that built the
	// module, which is what `go run` for GOOS=js uses itself.
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	glue := filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", "wasm_exec_node.js")
	if _, err := os.Stat(glue); err != nil {
		t.Fatalf("no wasm_exec_node.js in this toolchain (%s): %v", glue, err)
	}
	run := exec.Command(node, glue, wasmOut)
	out, err := run.CombinedOutput()
	// A non-zero exit is EXPECTED here, and only for one reason. closed_main_wasm.go
	// ends in select{} on purpose, to keep the runtime alive for the JS event and
	// animation callbacks a baked program may have registered; this program
	// registers none, so once it returns every goroutine is parked and Go's
	// deadlock detector aborts the module. The assertion is therefore on the REASON:
	// that exact abort is the documented design, and any other failure — a trap, a
	// link error, a Ruby exception — still fails the test.
	if err != nil && !strings.Contains(string(out), "all goroutines are asleep - deadlock!") {
		t.Fatalf("running the closed wasm module under node failed for an unexpected reason: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "closed wasm ruby ran") {
		t.Errorf("the baked program's JS.log did not reach the JS console; node said:\n%s", out)
	}
	// JS.document is undefined outside a browser, and the program prints it. What
	// is asserted is that the bridge REACHED the host object and reported what it
	// found there, rather than trapping: a missing global must come back as a value.
	if !strings.Contains(string(out), "document is: ") {
		t.Errorf("the JS bridge did not report a value for the absent `document` global; node said:\n%s", out)
	}
}
