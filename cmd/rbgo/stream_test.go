package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestRunSendsWarningsToTheRealStderr is the descriptor-level half of #667.
//
// The VM-level test (internal/vm/diagnostic_stream_test.go) proves that a split
// pair of writers stays split; this one proves the CLI hands it the right pair.
// It replaces os.Stdout and os.Stderr with two real pipes before calling run(),
// so the assertion is about actual file descriptors — the same thing
// `rbgo prog.rb 2>/dev/null` and `rbgo prog.rb 2>&1 >/dev/null` observe from a
// shell, which is how the defect was found and the only way to see it: the
// warning text was present under the old wiring too, just on descriptor 1.
func TestRunSendsWarningsToTheRealStderr(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "warntest.rb")
	// MRI 4.0.5 on this program prints "data\n" on fd 1 and
	// "warntest.rb:1: warning: given block not used\n" on fd 2 (array.c
	// rb_ary_index's rb_warn), and nothing of either on the other.
	program := "x = [1, 2].index(1) { :blk }\nputs \"data\"\n"
	if err := os.WriteFile(script, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	// Drain both pipes concurrently: a pipe holds only a buffer's worth, and a
	// blocked write would deadlock the run rather than fail the test.
	outCh, errCh := make(chan string, 1), make(chan string, 1)
	go func() { b, _ := io.ReadAll(outR); outCh <- string(b) }()
	go func() { b, _ := io.ReadAll(errR); errCh <- string(b) }()

	runErr := run(program, script)

	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = savedOut, savedErr
	gotOut, gotErr := <-outCh, <-errCh

	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	if want := "data\n"; gotOut != want {
		t.Errorf("fd 1 = %q, want %q (a warning here is the #667 defect)", gotOut, want)
	}
	// The script path is absolute in the label because that is the name run() was
	// given, so match on the suffix rather than the temp directory.
	if want := "warntest.rb:1: warning: given block not used\n"; len(gotErr) < len(want) || gotErr[len(gotErr)-len(want):] != want {
		t.Errorf("fd 2 = %q, want it to end with %q", gotErr, want)
	}
}
