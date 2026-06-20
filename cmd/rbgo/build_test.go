package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildIntegration drives `rbgo build` end to end: it AOT-compiles a program,
// links a native binary, runs it, and checks both the output and that the
// compiled method symbols are actually in the binary. It shells out to the Go
// toolchain, so it is gated behind RBGO_BUILD_IT=1 (kept out of the per-arch /
// qemu CI jobs, which cannot reliably run nested go builds).
func TestBuildIntegration(t *testing.T) {
	if os.Getenv("RBGO_BUILD_IT") == "" {
		t.Skip("set RBGO_BUILD_IT=1 to run the rbgo build integration test (needs the Go toolchain)")
	}

	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	root := filepath.Dir(strings.TrimSpace(string(gomod)))

	dir := t.TempDir()
	app := filepath.Join(dir, "app.rb")
	program := "def fib(n) = n < 2 ? n : fib(n - 1) + fib(n - 2)\n" +
		"def square(x) = x * x\n" +
		"puts fib(10)\n" +
		"puts square(9)\n"
	if err := os.WriteFile(app, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "app_aot")

	build := exec.Command("go", "run", "./cmd/rbgo", "build", "-o", bin, app)
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("rbgo build failed: %v\n%s", err, out)
	}

	out, err := exec.Command(bin, app).CombinedOutput()
	if err != nil {
		t.Fatalf("running AOT binary: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "55\n81" {
		t.Errorf("AOT binary output = %q, want \"55\\n81\"", got)
	}

	// The compiled method bodies must be linked in, not silently interpreted.
	nm, err := exec.Command("go", "tool", "nm", bin).Output()
	if err != nil {
		t.Fatalf("go tool nm: %v", err)
	}
	if !strings.Contains(string(nm), "(*VM).aotm0") {
		t.Error("compiled method symbol (*VM).aotm0 not found in the binary")
	}
}
