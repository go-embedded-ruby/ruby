package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestClosedBuildIntegration drives `rbgo build --closed` end to end: it bakes a
// program into a standalone binary that runs with no source file, loads the
// prelude from frozen bytecode, and links no lexer/parser/compiler. It asserts
// the output matches MRI, the front-end symbols are gone, the binary is smaller
// than the open build, and that eval raises at runtime. Like the open build test
// it shells out to the Go toolchain, so it is gated behind RBGO_BUILD_IT=1.
func TestClosedBuildIntegration(t *testing.T) {
	if os.Getenv("RBGO_BUILD_IT") == "" {
		t.Skip("set RBGO_BUILD_IT=1 to run the closed-world build integration test (needs the Go toolchain)")
	}
	root := moduleRootForTest(t)
	dir := t.TempDir()

	// A program that leans on the frozen prelude (Comparable/Enumerable), an
	// AOT-eligible method, and core types — all must work with no front-end.
	app := filepath.Join(dir, "app.rb")
	program := "def fib(n) = n < 2 ? n : fib(n - 1) + fib(n - 2)\n" +
		"puts [3, 1, 2].sort.map { |x| x * 10 }.sum\n" +
		"puts (1..5).select(&:even?).inspect\n" +
		"puts fib(10)\n" +
		"puts \"hello\".upcase\n"
	if err := os.WriteFile(app, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}

	closedBin := filepath.Join(dir, "app_closed")
	rbgoBuild(t, root, "-o", closedBin, "--closed", app)

	// Runs with NO source-file argument — the program is embedded.
	out, err := exec.Command(closedBin).CombinedOutput()
	if err != nil {
		t.Fatalf("running closed binary: %v\n%s", err, out)
	}
	const want = "60\n[2, 4]\n55\nHELLO"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("closed binary output = %q, want %q", got, want)
	}

	// The front-end must not be linked.
	nm, err := exec.Command("go", "tool", "nm", closedBin).Output()
	if err != nil {
		t.Fatalf("go tool nm: %v", err)
	}
	for _, sym := range []string{"go-ruby-parser/parser", "internal/compiler"} {
		if strings.Contains(string(nm), sym) {
			t.Errorf("closed binary still links the front-end (%s)", sym)
		}
	}

	// Dropping the front-end should make the closed binary smaller than the open one.
	openBin := filepath.Join(dir, "app_open")
	rbgoBuild(t, root, "-o", openBin, app)
	if cs, os_ := sizeOf(t, closedBin), sizeOf(t, openBin); cs >= os_ {
		t.Errorf("closed binary (%d) is not smaller than open (%d)", cs, os_)
	}

	// A standalone binary that cannot take arguments is not a usable program
	// (#708). Its arguments are its OWN: there is no interpreter command line in
	// front of them, so "-W0" here is a datum and not a switch, and the exit
	// status proves the program saw all three.
	//
	// The program deliberately carries a string literal. On origin/main
	// (87e418bc) `rbgo build --closed` fails outright for a program whose frozen
	// bytecode holds NO object value — `p ARGV` is enough — because
	// aot.FreezeISeq emits the internal/object import unconditionally and the
	// nested go build then rejects it as unused. That is a separate defect, in
	// internal/aot rather than here; this test must not depend on it.
	argvRb := filepath.Join(dir, "argv.rb")
	argvProg := "puts \"argv=#{ARGV.inspect} zero=#{$0.empty? ? \"(empty)\" : \"(set)\"}\"\n" +
		"exit ARGV.length\n"
	if err := os.WriteFile(argvRb, []byte(argvProg), 0o644); err != nil {
		t.Fatal(err)
	}
	argvBin := filepath.Join(dir, "argv_closed")
	rbgoBuild(t, root, "-o", argvBin, "--closed", argvRb)

	argvCmd := exec.Command(argvBin, "-W0", "alpha", "beta")
	argvOut, argvErr := argvCmd.CombinedOutput()
	// Measured: three arguments, so `exit 3`.
	if code := argvCmd.ProcessState.ExitCode(); code != 3 {
		t.Errorf("closed binary exit status = %d, want 3 (it saw %d arguments); err=%v out=%s",
			code, 3, argvErr, argvOut)
	}
	// $0 was the EMPTY string in a closed binary before this: nothing set it.
	const wantArgv = "argv=[\"-W0\", \"alpha\", \"beta\"] zero=(set)\n"
	if string(argvOut) != wantArgv {
		t.Errorf("closed binary with arguments printed %q, want %q", argvOut, wantArgv)
	}

	// eval in a closed binary raises NotImplementedError (front-end dropped).
	ev := filepath.Join(dir, "ev.rb")
	if err := os.WriteFile(ev, []byte("puts eval(\"1 + 1\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evBin := filepath.Join(dir, "ev_closed")
	rbgoBuild(t, root, "-o", evBin, "--closed", ev)
	evOut, err := exec.Command(evBin).CombinedOutput()
	if err == nil {
		t.Errorf("eval in closed binary should have failed; output:\n%s", evOut)
	}
	if !strings.Contains(string(evOut), "NotImplementedError") {
		t.Errorf("closed eval error = %q, want NotImplementedError", evOut)
	}
}

func moduleRootForTest(t *testing.T) string {
	t.Helper()
	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	return filepath.Dir(strings.TrimSpace(string(gomod)))
}

func rbgoBuild(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "./cmd/rbgo", "build"}, args...)...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rbgo build %v failed: %v\n%s", args, err, out)
	}
}

func sizeOf(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}
