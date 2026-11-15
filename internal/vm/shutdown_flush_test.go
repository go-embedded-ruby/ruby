// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ⛔ SILENT DATA LOSS: a file written and not closed was EMPTY on disk.
//
// MRI closes every open IO at interpreter shutdown — rb_ec_teardown reaches
// rb_objspace_call_finalizer, whose fptr_finalize fflushes each stream — so the
// ordinary short script
//
//	f = File.open(path, "w"); f.write("never closed")
//
// leaves its bytes on disk. rbgo buffers a file write in IOObj.buf and nothing
// flushed it at shutdown, so the program exited 0, the file existed, and it held
// 0 bytes. Measured against MRI 4.0.7 through cmd/oracle: 12 bytes against 0.
//
// ⛔ And the DISCRIMINATOR that said which defect it was: adding `f.flush` made
// rbgo write the 12 bytes, so the write was real and the buffer was the whole
// story — a fix aimed at the write path would have been aimed at nothing.
//
// Each test below asserts the FILE'S CONTENT read back from disk, never stdout:
// the program's own output was already correct while the data was being lost.
func TestAnUnclosedFileReachesDiskAtShutdown(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "unclosed.txt")
	runSrc(t, fmt.Sprintf(`f = File.open(%q, "w"); f.write("never closed")`, p))
	if got := readBack(t, p); got != "never closed" {
		t.Errorf("an unclosed write must survive shutdown:\ngot  %q\nwant %q", got, "never closed")
	}
}

// ⛔ THE ORDERING TRAP. "Close everything at the end" is right for exit, abort
// and falling off the end, and WRONG for exit!: process.c rb_f_exit_bang calls
// _exit(2), which bypasses the libc exit handlers and the object-space finalizer
// alike, so MRI loses the buffer there. A fix that flushed unconditionally would
// have handed exit! a flush MRI does not perform.
//
// Measured against MRI 4.0.7, 12 bytes written and never closed:
//
//	exit                    → 12 bytes
//	abort                   → 12 bytes
//	falling off the end     → 12 bytes
//	exit!                   → 0 bytes
//
// The last row is the NEGATIVE control, and it is the reason this test takes all
// four exits rather than the common one.
func TestEachExitPathFlushesExceptTheImmediateOne(t *testing.T) {
	for _, tc := range []struct {
		name string
		tail string
		want string
	}{
		{"exit", "exit 0", "payload"},
		{"exit_with_status", "exit 3", "payload"},
		{"abort", `begin; abort "stop"; rescue SystemExit; end; exit 1`, "payload"},
		{"falls_off_the_end", "", "payload"},
		{"at_exit_handler_runs_first", `at_exit { f.write("+handler") }`, "payload+handler"},

		// ⛔ exit! must keep LOSING it, as MRI does.
		{"exit_bang", "exit! 4", ""},
		{"process_exit_bang", "Process.exit! 4", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "x.txt")
			runSrc(t, fmt.Sprintf("f = File.open(%q, \"w\")\nf.write(\"payload\")\n%s\n", p, tc.tail))
			if got := readBack(t, p); got != tc.want {
				t.Errorf("%s:\ngot  %q\nwant %q", tc.name, got, tc.want)
			}
		})
	}
}

// ⛔ AN UNCAUGHT EXCEPTION IS A PROGRAM END TOO, and it was the second
// divergence the differential found. MRI's ruby_cleanup tears the interpreter
// down — rb_ec_teardown runs the at_exit handlers, then the object-space
// finalizer flushes every IO — and only THEN does error_handle report the
// exception. Run returned the error having done neither, so this program lost
// its handler and its bytes both.
//
// Measured against MRI 4.0.7:
//
//	at_exit { puts "handler ran" }; puts "before"; raise "boom"
//	  MRI   exit 1, stdout "before\nhandler ran"
//	  rbgo  exit 1, stdout "before"
//
// The error is still RETURNED: an uncaught exception is what happened, and the
// host decides what to print. Only the teardown was missing.
func TestAnUncaughtExceptionStillTearsTheInterpreterDown(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.txt")
	_, out, err := runSrcErr(t, fmt.Sprintf(`
f = File.open(%q, "w")
f.write("payload")
at_exit { puts "handler ran" }
raise "boom"
`, p))
	if err == nil {
		t.Fatal("an uncaught raise must still be reported to the host")
	}
	if got := readBack(t, p); got != "payload" {
		t.Errorf("an uncaught exception lost the file's bytes:\ngot  %q\nwant %q", got, "payload")
	}
	if !strings.Contains(out, "handler ran") {
		t.Errorf("at_exit must run on an uncaught exception, as MRI does; stdout was %q", out)
	}
}

// ⛔ A MECHANISM WIRED AT ONLY SOME OF ITS DOORS is this repository's commonest
// defect shape — three of six socket open classes, one of two exit! spellings,
// the interpreter's missing safepoint. A writable path-backed stream can reach
// Ruby four ways, and this test takes them ONE BY ONE in Ruby's own vocabulary
// rather than trusting the four trackOpenFile call sites to be all of them.
//
// A failure here names the door: whichever subtest is red is the constructor
// that hands out an untracked stream.
func TestEveryWayToGetAWritableFileFlushesAtShutdown(t *testing.T) {
	t.Run("File.open_w", func(t *testing.T) {
		assertSurvives(t, `f = File.open(%q, "w"); f.write("payload")`, "payload")
	})
	t.Run("File.new_w", func(t *testing.T) {
		assertSurvives(t, `f = File.new(%q, "w"); f.write("payload")`, "payload")
	})
	t.Run("File.open_append", func(t *testing.T) {
		// The file does not exist yet, so "a" creates it: the whole content is the
		// appended bytes.
		assertSurvives(t, `f = File.open(%q, "a"); f.write("payload")`, "payload")
	})
	t.Run("File.open_r_plus", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "x.txt")
		if err := os.WriteFile(p, []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
		runSrc(t, fmt.Sprintf(`f = File.open(%q, "r+"); f.write("AB")`, p))
		if got := readBack(t, p); got != "AB23456789" {
			t.Errorf("r+ write lost:\ngot  %q\nwant %q", got, "AB23456789")
		}
	})
	// ⛔ THE DOOR THIS MECHANISM WAS MISSING when it was first written. IO.for_fd
	// (and IO.new(fd), and File.new(fd)) build a SECOND IOObj over the descriptor,
	// sharing the first one's path and buffer; the program writes through that
	// second one, so tracking IO.sysopen's own stream left the file empty. Found
	// by the differential against MRI 4.0.7, not by reading the call sites.
	t.Run("IO.sysopen_then_for_fd", func(t *testing.T) {
		assertSurvives(t, `fd = IO.sysopen(%q, "w"); f = IO.for_fd(fd); f.write("payload")`, "payload")
	})
	t.Run("IO#pwrite", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "x.txt")
		if err := os.WriteFile(p, []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
		// "r+", not "r": #pwrite on a read-only stream raises IOError "not opened
		// for writing" — rb_io_pwrite calls rb_io_check_writable — and rbgo and MRI
		// 4.0.7 agree on that message, so it is not a door, it is a refusal. This
		// test asserted "r" at first and was wrong where the code was right.
		runSrc(t, fmt.Sprintf(`f = File.open(%q, "r+"); f.pwrite("AB", 0)`, p))
		if got := readBack(t, p); got != "AB23456789" {
			t.Errorf("pwrite lost:\ngot  %q\nwant %q", got, "AB23456789")
		}
	})
	t.Run("IO#reopen", func(t *testing.T) {
		dir := t.TempDir()
		first := filepath.Join(dir, "first.txt")
		second := filepath.Join(dir, "second.txt")
		if err := os.WriteFile(second, []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
		runSrc(t, fmt.Sprintf(
			"f = File.open(%q, \"w\")\nf.write(\"ignored\")\nf.reopen(%q, \"r+\")\nf.write(\"AB\")\n",
			first, second))
		// ⛔ The stream that must be flushed is the REOPENED one, never the
		// throwaway the reopen built to read the new path: that throwaway holds the
		// same path and the file's PRE-REOPEN bytes, so flushing it would write
		// "0123456789" back over the "AB".
		if got := readBack(t, second); got != "AB23456789" {
			t.Errorf("write after reopen lost (or the throwaway was flushed over it):\ngot  %q\nwant %q",
				got, "AB23456789")
		}
	})
}

// ⛔ THE CONTROL the four doors above cannot provide: that teardown does not
// write where the program did not ask it to. A stream explicitly closed, then
// the file changed from outside, must keep the OUTSIDE content — a teardown that
// re-flushed every tracked stream regardless of IOObj.closed would put the stale
// buffer back and this is the only test that would notice.
func TestShutdownDoesNotReFlushAClosedStream(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.txt")
	runSrc(t, fmt.Sprintf(`
f = File.open(%q, "w")
f.write("from ruby")
f.close
File.write(%q, "written after the close")
`, p, p))
	if got := readBack(t, p); got != "written after the close" {
		t.Errorf("teardown re-flushed a closed stream:\ngot  %q\nwant %q", got, "written after the close")
	}
}

// trackOpenFile compacts the closed entries out at the growth point rather than
// untracking at each of the five places that set IOObj.closed. This drives that
// branch directly: a stream that is closed before the next track must not keep
// its slot, or a long-running program that opens and closes files forever grows
// the slice forever.
func TestTrackOpenFileDropsClosedStreamsInsteadOfGrowing(t *testing.T) {
	vm := New(os.Stdout)
	for i := range 64 {
		o := &IOObj{isStr: true, writable: true, path: fmt.Sprintf("/tmp/none-%d", i)}
		vm.trackOpenFile(o)
		o.closed = true // as IO#close does, before the next open
	}
	if n := len(vm.openFiles); n > 2 {
		t.Errorf("64 opened-and-closed streams left %d tracked; the slice must not grow with them", n)
	}

	// Not writable, and no path: nothing to lose, so nothing to retain. A read-only
	// sweep over thousands of files must not pin their buffers.
	before := len(vm.openFiles)
	vm.trackOpenFile(&IOObj{isStr: true, path: "/tmp/read-only"})
	vm.trackOpenFile(&IOObj{isStr: true, writable: true})
	vm.trackOpenFile(nil)
	if after := len(vm.openFiles); after != before {
		t.Errorf("a stream with nothing to flush was tracked: %d → %d", before, after)
	}

	// Idempotent: #pwrite and #reopen both land on a stream already tracked.
	o := &IOObj{isStr: true, writable: true, path: "/tmp/twice"}
	vm.trackOpenFile(o)
	n := len(vm.openFiles)
	vm.trackOpenFile(o)
	if len(vm.openFiles) != n {
		t.Errorf("tracking the same stream twice added an entry: %d → %d", n, len(vm.openFiles))
	}
}

// ⛔ A TEARDOWN FLUSH MUST NOT RAISE. This function first re-raised the first
// Errno a flush produced, on the reasoning that a silent teardown failure is
// worse than a loud one. Two existing tests refuted it within the hour:
//
//	Dir.mktmpdir { |d| IO.for_fd(File.open(File.join(d,"f"),"w").fileno,"w").write("foo") }
//
// The block's exit removes the directory, so the teardown flush — which re-opens
// the path, having no descriptor — gets ENOENT, and a program that had ended
// cleanly came back as `Errno::ENOENT @ rb_sysopen`. MRI ignores a failing
// fflush at shutdown, and by then no rescue can run anyway.
//
// So: an unwritable stream must cost nothing but its own bytes, and the streams
// after it must still be flushed — the half of the original reasoning that was
// right, and the only part a test can see.
func TestATeardownFlushNeitherRaisesNorSkipsTheRest(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.txt")
	vm := New(os.Stdout)
	vm.openFiles = []*IOObj{
		{isStr: true, writable: true, wbufDirty: true, path: filepath.Join(dir, "no-such-dir", "bad.txt"), buf: []byte("lost")},
		{isStr: true, writable: true, wbufDirty: true, path: good, buf: []byte("kept")},
	}
	if r := func() (r any) {
		defer func() { r = recover() }()
		vm.flushOpenFiles()
		return nil
	}(); r != nil {
		t.Errorf("teardown must not raise, got %#v", r)
	}
	if got := readBack(t, good); got != "kept" {
		t.Errorf("the stream after the failing one was skipped:\ngot  %q\nwant %q", got, "kept")
	}
}

// And the same through Ruby, which is the shape the two existing tests have: the
// directory is gone by the time teardown runs, and the program must still end
// cleanly.
func TestAFileWhoseDirectoryIsGoneDoesNotBreakTheExit(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// runSrc t.Fatals on a run error, so it is itself the assertion here.
	runSrc(t, fmt.Sprintf(`
f = File.open(%q, "w")
f.write("payload")
Dir.rmdir(%q) rescue nil
File.unlink(%q) rescue nil
Dir.rmdir(%q)
puts "ended cleanly"
`, filepath.Join(sub, "f.txt"), sub, filepath.Join(sub, "f.txt"), sub))
}

// assertSurvives runs src — a format holding exactly one %q for the path — with a
// fresh temp file and checks the file's content afterwards.
func assertSurvives(t *testing.T, src, want string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "x.txt")
	runSrc(t, fmt.Sprintf(src, p))
	if got := readBack(t, p); got != want {
		t.Errorf("content lost at shutdown:\ngot  %q\nwant %q", got, want)
	}
}

// readBack reads the file the Ruby program wrote. A missing file reads as "" so a
// caller expecting the empty content (exit!) is not told a different story from
// one expecting a truncated file.
func readBack(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read back %s: %v", p, err)
	}
	return string(b)
}
