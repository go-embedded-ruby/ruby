// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !wasm

package vm

import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// runSpawn compiles and runs src with runCaptured replaced by a deterministic
// stub, so the process-execution paths are exercised identically on every OS
// (no real subprocess). It returns captured stdout; a runtime error is surfaced
// as a t.Fatal unless wantErr matches the panic's Ruby class.
func runSpawn(t *testing.T, src string, fake func([]string) (string, int)) string {
	t.Helper()
	orig := runCaptured
	runCaptured = fake
	defer func() { runCaptured = orig }()

	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	if _, err := New(&buf).Run(iseq); err != nil {
		t.Fatalf("run: %v\nsrc: %s", err, src)
	}
	return buf.String()
}

// echoFake records the commands it was asked to run and echoes a fixed output.
func TestSpawnPipeAndWaitpid(t *testing.T) {
	var got []string
	fake := func(cmd []string) (string, int) {
		got = append(got, strings.Join(cmd, " "))
		return "hi from rbgo\n", 0
	}
	src := `r, w = IO.pipe
pid = Process.spawn("/bin/echo hi from rbgo", :out => w, :err => w)
w.close
out = r.read
r.close
_, st = Process.waitpid2(pid)
puts "OUT=#{out.inspect} EXIT=#{st.exitstatus} SUCCESS=#{st.success?} PID=#{st.pid == pid}"`
	if out := runSpawn(t, src, fake); out != "OUT=\"hi from rbgo\\n\" EXIT=0 SUCCESS=true PID=true\n" {
		t.Fatalf("got %q", out)
	}
	if len(got) != 1 || got[0] != "/bin/echo hi from rbgo" {
		t.Fatalf("commands run: %v", got)
	}
}

// TestSpawnForkExec covers the Kernel.fork + Kernel.exec idiom (Puppet's
// execute path): the block's STDOUT redirection lands on the pipe and is
// restored afterwards so the parent's later writes reach real stdout.
func TestSpawnForkExec(t *testing.T) {
	fake := func(cmd []string) (string, int) { return "child-out\n", 3 }
	src := `r, w = IO.pipe
pid = Kernel.fork do
  STDOUT.reopen(w)
  Kernel.exec("/bin/echo", "child-out")
end
w.close
_, st = Process.waitpid2(pid)
puts "parent OUT=#{r.read.inspect} EXIT=#{st.exitstatus}"`
	if out := runSpawn(t, src, fake); out != "parent OUT=\"child-out\\n\" EXIT=3\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnExecTopLevel covers Kernel.exec called outside any fork: it runs the
// command, writes its output to stdout, and unwinds the program.
func TestSpawnExecTopLevel(t *testing.T) {
	fake := func(cmd []string) (string, int) { return "top\n", 0 }
	// exec terminates the run; the trailing puts must NOT appear.
	out := runSpawn(t, `Kernel.exec("/bin/echo top"); puts "after"`, fake)
	if out != "top\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnForkNormalReturn covers a fork block that returns without exec: it is
// treated as a child that exited 0.
func TestSpawnForkNormalReturn(t *testing.T) {
	fake := func(cmd []string) (string, int) { return "", 0 }
	out := runSpawn(t, `pid = Kernel.fork { 41 + 1 }
_, st = Process.waitpid2(pid)
puts st.exitstatus`, fake)
	if out != "0\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnForkBlockRaises covers runForkBlock re-propagating a non-exec panic:
// a Ruby exception raised inside the fork block escapes fork, as in MRI.
func TestSpawnForkBlockRaises(t *testing.T) {
	fake := func(cmd []string) (string, int) { return "", 0 }
	out := runSpawn(t, `begin
  Kernel.fork { raise "boom" }
rescue => e
  puts e.message
end`, fake)
	if out != "boom\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnForkNoBlock covers fork without a block (unsupported): the program is
// expected to rescue the NotImplementedError it raises.
func TestSpawnForkNoBlock(t *testing.T) {
	fake := func([]string) (string, int) { return "", 0 }
	out := runSpawn(t, `begin; Kernel.fork; rescue => e; puts e.class.name; end`, fake)
	if out != "NotImplementedError\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnReadNonblock covers read_nonblock / readpartial: draining buffered
// bytes, EAGAIN when nothing is buffered, and EOFError once the writer closes.
func TestSpawnReadNonblock(t *testing.T) {
	fake := func([]string) (string, int) { return "", 0 }
	out := runSpawn(t, `r, w = IO.pipe
w.write("abcd")
got = [r.read_nonblock(2), r.readpartial(10)]
begin; r.read_nonblock(1); rescue => e; got << e.class.name; end
w.close
begin; r.read_nonblock(1); rescue => e; got << e.class.name; end
p got`, fake)
	if out != "[\"ab\", \"cd\", \"Errno::EAGAIN\", \"EOFError\"]\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnSelect covers IO.select readiness reporting and the all-empty nil
// result.
func TestSpawnSelect(t *testing.T) {
	fake := func([]string) (string, int) { return "", 0 }
	out := runSpawn(t, `r, w = IO.pipe
empty = IO.select([r], [], [], 0)
w.write("x")
ready, = IO.select([r], [w], [])
p [empty, ready.first.equal?(r)]`, fake)
	if out != "[nil, true]\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnSelectEOFReady covers a reader becoming ready because its write end
// closed (EOF), even with no buffered bytes.
func TestSpawnSelectEOFReady(t *testing.T) {
	fake := func([]string) (string, int) { return "", 0 }
	out := runSpawn(t, `r, w = IO.pipe
w.close
ready, = IO.select([r])
p ready.first.equal?(r)`, fake)
	if out != "true\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnPipeBlockForm covers IO.pipe { |r, w| ... }.
func TestSpawnPipeBlockForm(t *testing.T) {
	fake := func([]string) (string, int) { return "", 0 }
	out := runSpawn(t, `v = IO.pipe { |r, w| w.write("z"); w.close; r.read }
p v`, fake)
	if out != "\"z\"\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnGetsAndEof covers the pipe-reader path of gets/eof? (pipeRefresh).
func TestSpawnGetsAndEof(t *testing.T) {
	fake := func([]string) (string, int) { return "", 0 }
	out := runSpawn(t, `r, w = IO.pipe
w.write("a\nb\n")
w.close
lines = [r.gets, r.gets]
lines << r.eof?
p lines`, fake)
	if out != "[\"a\\n\", \"b\\n\", true]\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnWaitpidUnknown covers waitpid2 / waitpid for an unknown pid: nil with
// a non-zero (WNOHANG) flag, ECHILD without.
func TestSpawnWaitpidUnknown(t *testing.T) {
	fake := func([]string) (string, int) { return "", 0 }
	out := runSpawn(t, `p Process.waitpid2(999, Process::WNOHANG)
p Process.waitpid(999, Process::WNOHANG)
[:waitpid2, :waitpid].each do |m|
  begin; Process.send(m, 999); rescue => e; puts e.class.name; end
end`, fake)
	if out != "nil\nnil\nErrno::ECHILD\nErrno::ECHILD\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnWaitpidSuccess covers waitpid (single-value form) reaping a child.
func TestSpawnWaitpidSuccess(t *testing.T) {
	fake := func([]string) (string, int) { return "", 0 }
	out := runSpawn(t, `pid = Process.spawn("x")
p Process.waitpid(pid) == pid`, fake)
	if out != "true\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnSetsid covers Process.setsid (returns a pid-like integer).
func TestSpawnSetsid(t *testing.T) {
	fake := func([]string) (string, int) { return "", 0 }
	out := runSpawn(t, `p Process.setsid.is_a?(Integer)`, fake)
	if out != "true\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSpawnEnvAndOptsParsing covers parseSpawnArgs stripping a leading env Hash
// and a trailing options Hash, and writeSpawnOutput's no-opts / non-IO branches.
func TestSpawnEnvAndOptsParsing(t *testing.T) {
	var got []string
	fake := func(cmd []string) (string, int) { got = append(got, strings.Join(cmd, " ")); return "", 0 }
	// Leading env hash + trailing opts hash whose :out is not an IO (ignored).
	runSpawn(t, `Process.spawn({"A" => "1"}, "/bin/echo", "hi", {:out => 5})`, fake)
	// No options hash at all.
	runSpawn(t, `Process.spawn("/bin/echo", "bye")`, fake)
	if len(got) != 2 || got[0] != "/bin/echo hi" || got[1] != "/bin/echo bye" {
		t.Fatalf("commands: %v", got)
	}
}

// TestSpawnShellish covers the shell-vs-argv decision: a one-string command with
// metacharacters goes through the shell wrapper; a metachar-free argv does not.
func TestSpawnShellish(t *testing.T) {
	for _, c := range []struct {
		cmd  []string
		want bool
	}{
		{[]string{"/bin/echo hi"}, true}, // whitespace
		{[]string{"a|b"}, true},          // metachar
		{[]string{"/bin/false"}, false},  // bare
		{[]string{"a", "b"}, false},      // multi-element argv
	} {
		if _, sh := shellish(c.cmd); sh != c.want {
			t.Errorf("shellish(%v) = %v, want %v", c.cmd, sh, c.want)
		}
	}
}

// TestSpawnReopenCycle covers the cycle guard in writeBytes: a self-reopen must
// not recurse, and a normal reopen forwards writes to the target.
func TestSpawnReopenCycle(t *testing.T) {
	fake := func([]string) (string, int) { return "", 0 }
	// STDOUT.reopen($stdout) is a self/loop reopen; writing must still terminate
	// (and land on real stdout).
	out := runSpawn(t, `STDOUT.reopen($stdout); STDOUT.write("ok")`, fake)
	if out != "ok" {
		t.Fatalf("got %q", out)
	}
}

// TestExecSentinelError covers the execSentinel Error() string for completeness.
func TestExecSentinelError(t *testing.T) {
	if (execSentinel{}).Error() != "exec sentinel" {
		t.Fatal("execSentinel.Error mismatch")
	}
}

// TestExitCodeOf covers the exit-code extraction seam used by the native/windows
// runCaptured: success (nil), a real *exec.ExitError carrying a non-zero status,
// and a non-ExitError failure (mapped to 127, the shell's "not found" code).
func TestExitCodeOf(t *testing.T) {
	if exitCodeOf(nil) != 0 {
		t.Fatal("nil err should be 0")
	}
	if exitCodeOf(errString("boom")) != 127 {
		t.Fatal("non-ExitError should be 127")
	}
	// Produce a genuine *exec.ExitError by running a command that exits non-zero.
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/c", "exit 3")
	} else {
		c = exec.Command("sh", "-c", "exit 3")
	}
	err := c.Run()
	if got := exitCodeOf(err); got != 3 {
		t.Fatalf("exitCodeOf(ExitError) = %d, want 3", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// runSystem runs src with systemCommand replaced by a deterministic stub, so
// Kernel#system's true/false/nil and exception branches are exercised on every
// OS without a real subprocess.
func runSystem(t *testing.T, src string, fake func([]string) (string, int, bool)) string {
	t.Helper()
	orig := systemCommand
	systemCommand = fake
	defer func() { systemCommand = orig }()

	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	if _, err := New(&buf).Run(iseq); err != nil {
		t.Fatalf("run: %v\nsrc: %s", err, src)
	}
	return buf.String()
}

// TestSystemReturnValues covers Kernel#system: true (exit 0), false (non-zero),
// nil (could not spawn), the command output reaching $stdout, and $? being set
// to a Process::Status for a spawned command (and nil for an unspawned one).
func TestSystemReturnValues(t *testing.T) {
	// exit 0 -> true, output written, $? success.
	ok := func([]string) (string, int, bool) { return "out\n", 0, true }
	if got := runSystem(t, `r = system("true"); print r, " ", $?.exitstatus, " ", $?.success?`, ok); got != "out\ntrue 0 true" {
		t.Fatalf("exit0: got %q", got)
	}
	// non-zero -> false, $? reflects the code.
	bad := func([]string) (string, int, bool) { return "", 5, true }
	if got := runSystem(t, `p system("false"); p $?.exitstatus; p $?.success?`, bad); got != "false\n5\nfalse\n" {
		t.Fatalf("nonzero: got %q", got)
	}
	// could not spawn -> nil, $? nil.
	miss := func([]string) (string, int, bool) { return "", 127, false }
	if got := runSystem(t, `p system("nope"); p $?`, miss); got != "nil\nnil\n" {
		t.Fatalf("unspawned: got %q", got)
	}
}

// TestSystemExceptionOption covers system(..., exception: true): a raised
// RuntimeError on a non-zero exit and Errno::ENOENT when the command cannot be
// spawned, while exception: false keeps the return-value behaviour.
func TestSystemExceptionOption(t *testing.T) {
	bad := func([]string) (string, int, bool) { return "", 1, true }
	got := runSystem(t, `begin; system("false", exception: true); rescue => e; puts e.class; end`, bad)
	if got != "RuntimeError\n" {
		t.Fatalf("exit-fail exception: got %q", got)
	}
	miss := func([]string) (string, int, bool) { return "", 127, false }
	got = runSystem(t, `begin; system("nope", exception: true); rescue => e; puts e.class; end`, miss)
	if got != "Errno::ENOENT\n" {
		t.Fatalf("spawn-fail exception: got %q", got)
	}
	// exception: false is the same as omitting it (false return, no raise).
	got = runSystem(t, `p system("false", exception: false)`, bad)
	if got != "false\n" {
		t.Fatalf("exception:false: got %q", got)
	}
}

// TestSystemCommandReal exercises the real systemCommand for the running OS,
// covering the empty-argv, shell, direct-argv, ExitError and non-ExitError
// (could-not-spawn) branches with actual short-lived processes.
func TestSystemCommandReal(t *testing.T) {
	// Empty argv: nothing to run.
	if _, code, spawned := systemCommand(nil); spawned || code != 127 {
		t.Fatalf("empty argv: code=%d spawned=%v", code, spawned)
	}
	var zero, nonzero, missing []string
	if runtime.GOOS == "windows" {
		zero = []string{"cmd /c exit 0"} // whitespace -> shell path
		nonzero = []string{"cmd /c exit 7"}
		missing = []string{"this_binary_does_not_exist_rbgo"}
	} else {
		zero = []string{"exit 0"}                             // whitespace -> /bin/sh
		nonzero = []string{"false"}                           // bare argv, real exit 1
		missing = []string{"this_binary_does_not_exist_rbgo"} // bare argv, cannot spawn
	}
	if _, code, spawned := systemCommand(zero); !spawned || code != 0 {
		t.Fatalf("zero: code=%d spawned=%v", code, spawned)
	}
	if _, code, spawned := systemCommand(nonzero); !spawned || code == 0 {
		t.Fatalf("nonzero: code=%d spawned=%v", code, spawned)
	}
	if _, code, spawned := systemCommand(missing); spawned || code != 127 {
		t.Fatalf("missing: code=%d spawned=%v", code, spawned)
	}
}

// TestNewProcessStatus covers the $?-status builder directly.
func TestNewProcessStatus(t *testing.T) {
	vm := New(&bytes.Buffer{})
	st := vm.newProcessStatus(4242, 9).(*RObject)
	if st.ivars["@exitstatus"] != object.IntValue(9) || st.ivars["@pid"] != object.IntValue(4242) {
		t.Fatalf("status ivars: %+v", st.ivars)
	}
}
