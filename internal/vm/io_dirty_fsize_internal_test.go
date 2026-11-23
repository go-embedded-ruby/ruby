// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin || linux

package vm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// fsizeChildEnv marks the copy of the test binary that runs the limited flush.
const fsizeChildEnv = "RBGO_FSIZE_CHILD"

// TestAFlushPastTheFileSizeLimitRaises: the file opened, and the write itself
// was refused. With RLIMIT_FSIZE below the span's offset, write(2) answers
// EFBIG (the Go runtime ignores SIGXFSZ, so the process survives it). This is
// TestAFlushThatCannotWriteRaises for platforms with no /dev/full: the error
// must be raised, not dropped while Close reports success.
//
// The limit is set in a CHILD copy of the test binary, never in this one.
// RLIMIT_FSIZE is per process: lowered here, it bound every file the other
// tests and the testing package wrote meanwhile, and the arm64 lane failed
// with "can't write .../testlog.txt: file too large" after every test had
// passed (go-embedded-ruby/ruby#982's first run).
func TestAFlushPastTheFileSizeLimitRaises(t *testing.T) {
	if os.Getenv(fsizeChildEnv) == "1" {
		fsizeChild()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAFlushPastTheFileSizeLimitRaises$")
	cmd.Env = append(os.Environ(), fsizeChildEnv+"=1", "TMPDIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	switch got := string(out); {
	case strings.Contains(got, "fsize: skip"):
		t.Skipf("child could not arrange the limit:\n%s", got)
	case !strings.Contains(got, "fsize: raised Errno::EFBIG\n"):
		t.Errorf("a flush past RLIMIT_FSIZE did not raise Errno::EFBIG; child said:\n%s", got)
	}
}

// fsizeChild runs in the child: it lowers its own RLIMIT_FSIZE, flushes, and
// prints the class raised. Nothing else runs in this process.
func fsizeChild() {
	path := filepath.Join(os.TempDir(), "f")
	o := &IOObj{isStr: true, writable: true, dirty: []ioSpan{{8, 12}}, path: path, buf: []byte("0123456789ab")}
	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		os.Stdout.WriteString("fsize: skip getrlimit: " + err.Error() + "\n")
		return
	}
	limited := syscall.Rlimit{Cur: 4, Max: old.Max}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		os.Stdout.WriteString("fsize: skip setrlimit: " + err.Error() + "\n")
		return
	}
	os.Stdout.WriteString("fsize: raised " + catchRaise(func() { ioFlush(o) }) + "\n")
}
