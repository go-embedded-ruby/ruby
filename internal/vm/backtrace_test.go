// Copyright (c) the go-embedded-ruby/ruby authors
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

// runScript parses, compiles and runs src on a fresh VM whose top-level program
// path is name (so backtraces and __FILE__ report it the way the CLI does). It
// returns stdout and any uncaught error.
func runScript(t *testing.T, src, name string) (string, error) {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	iseq.Name = name
	var b strings.Builder
	m := vm.New(&b)
	switch {
	case name == "-e":
		m.SetScriptName(name)
	case name != "":
		m.SetScriptPath(name)
	}
	_, err = m.Run(iseq)
	return b.String(), err
}

// TestBacktraceCaptureChain verifies a raise records the file+label of every
// frame between the raise site and the top level, innermost-first like MRI.
func TestBacktraceCaptureChain(t *testing.T) {
	script := filepath.Join("/x", "prog.rb")
	out, err := runScript(t, `
def foo
  raise ArgumentError, "boom"
end
def bar
  foo
end
begin
  bar
rescue => e
  puts e.backtrace
end
`, script)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := strings.Join([]string{
		script + ":0:in 'foo'",
		script + ":0:in 'bar'",
		script + ":0:in '<main>'",
	}, "\n") + "\n"
	if out != want {
		t.Fatalf("backtrace\n got %q\nwant %q", out, want)
	}
}

// TestBacktraceNeverRaised: an exception built but never raised has a nil
// backtrace, matching MRI.
func TestBacktraceNeverRaised(t *testing.T) {
	out, err := runScript(t, `p RuntimeError.new("x").backtrace`, "-e")
	if err != nil {
		t.Fatal(err)
	}
	if out != "nil\n" {
		t.Fatalf("got %q want %q", out, "nil\n")
	}
}

// TestBacktraceLocations mirrors #backtrace (best-effort) and is nil before a
// raise.
func TestBacktraceLocations(t *testing.T) {
	out, err := runScript(t, `
p RuntimeError.new("x").backtrace_locations
begin
  raise "y"
rescue => e
  puts e.backtrace_locations.length
end
`, "-e")
	if err != nil {
		t.Fatal(err)
	}
	if out != "nil\n1\n" {
		t.Fatalf("got %q", out)
	}
}

// TestReRaisePreservesBacktrace: re-raising a rescued exception keeps the
// original backtrace (MRI does not overwrite it).
func TestReRaisePreservesBacktrace(t *testing.T) {
	out, err := runScript(t, `
def deep
  raise "first"
end
begin
  begin
    deep
  rescue => e
    raise e
  end
rescue => e2
  puts e2.backtrace.first
end
`, "/x/p.rb")
	if err != nil {
		t.Fatal(err)
	}
	if out != "/x/p.rb:0:in 'deep'\n" {
		t.Fatalf("got %q", out)
	}
}

// TestSetBacktraceForms covers set_backtrace with a String, an Array of String
// and nil, plus the TypeError paths for a non-String scalar and an Array with a
// non-String element.
func TestSetBacktraceForms(t *testing.T) {
	out, err := runScript(t, `
e = RuntimeError.new("x")
e.set_backtrace("a.rb:1:in 'x'")
p e.backtrace
e.set_backtrace(["b.rb:2:in 'y'", "c.rb:3:in 'z'"])
p e.backtrace
e.set_backtrace(nil)
p e.backtrace
begin
  e.set_backtrace(42)
rescue TypeError => t
  puts t.message
end
begin
  e.set_backtrace([1, 2])
rescue TypeError => t
  puts t.message
end
`, "-e")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		`["a.rb:1:in 'x'"]`,
		`["b.rb:2:in 'y'", "c.rb:3:in 'z'"]`,
		`nil`,
		`backtrace must be an Array of String or an Array of Thread::Backtrace::Location`,
		`backtrace must be an Array of String or an Array of Thread::Backtrace::Location`,
	}, "\n") + "\n"
	if out != want {
		t.Fatalf("\n got %q\nwant %q", out, want)
	}
}

// TestSetBacktraceNoArgs: set_backtrace with no argument clears the backtrace
// (the args-length zero branch defaults to nil).
func TestSetBacktraceNoArgs(t *testing.T) {
	out, err := runScript(t, `
e = RuntimeError.new("x")
e.set_backtrace("a.rb:1:in 'x'")
e.set_backtrace
p e.backtrace
`, "-e")
	if err != nil {
		t.Fatal(err)
	}
	if out != "nil\n" {
		t.Fatalf("got %q", out)
	}
}

// TestFullMessageShape checks full_message renders the first frame, the
// "<msg> (<Class>)" body and a "\tfrom" line per remaining frame.
func TestFullMessageShape(t *testing.T) {
	out, err := runScript(t, `
def foo
  raise ArgumentError, "boom"
end
begin
  foo
rescue => e
  puts e.full_message
end
`, "/x/p.rb")
	if err != nil {
		t.Fatal(err)
	}
	want := "/x/p.rb:0:in 'foo': boom (ArgumentError)\n" +
		"\tfrom /x/p.rb:0:in '<main>'\n"
	if out != want {
		t.Fatalf("\n got %q\nwant %q", out, want)
	}
}

// TestDetailedMessageAndNoBacktrace covers detailed_message and full_message's
// degraded form when no backtrace is present (never-raised exception): both
// yield "<msg> (<Class>)".
func TestDetailedMessageAndNoBacktrace(t *testing.T) {
	out, err := runScript(t, `
e = RuntimeError.new("x")
puts e.detailed_message
puts e.full_message
ec = RuntimeError.new("x")
ec.set_backtrace([])
puts ec.full_message
`, "-e")
	if err != nil {
		t.Fatal(err)
	}
	want := "x (RuntimeError)\nx (RuntimeError)\nx (RuntimeError)\n"
	if out != want {
		t.Fatalf("\n got %q\nwant %q", out, want)
	}
}

// TestMessageDefaultsToClassName: with no @message the message/detailed_message
// fall back to the class name (covers exceptionMessageText's else branch).
func TestMessageDefaultsToClassName(t *testing.T) {
	out, err := runScript(t, `
e = RuntimeError.new
puts e.detailed_message
`, "-e")
	if err != nil {
		t.Fatal(err)
	}
	if out != "RuntimeError (RuntimeError)\n" {
		t.Fatalf("got %q", out)
	}
}

// TestUncaughtBacktraceEscaping confirms an uncaught Ruby exception carries its
// captured backtrace out of Run as RubyError.Frames (the data the CLI prints).
func TestUncaughtBacktraceEscaping(t *testing.T) {
	_, err := runScript(t, `
def boom
  raise ArgumentError, "nope"
end
boom
`, "/x/p.rb")
	re, ok := err.(vm.RubyError)
	if !ok {
		t.Fatalf("want RubyError, got %#v", err)
	}
	bt := re.Backtrace()
	want := []string{"/x/p.rb:0:in 'boom'", "/x/p.rb:0:in '<main>'"}
	if strings.Join(bt, "|") != strings.Join(want, "|") {
		t.Fatalf("Backtrace()\n got %v\nwant %v", bt, want)
	}
}

// TestUncaughtInternalRaiseBacktrace: an internal (Go-level) raise — an arity
// error — also escapes with a backtrace, since exceptionObject captures it.
func TestUncaughtInternalRaiseBacktrace(t *testing.T) {
	_, err := runScript(t, `
def f(a, b); end
def g; f; end
g
`, "/x/p.rb")
	re, ok := err.(vm.RubyError)
	if !ok || re.Class != "ArgumentError" {
		t.Fatalf("want ArgumentError RubyError, got %#v", err)
	}
	if len(re.Backtrace()) == 0 {
		t.Fatalf("expected a backtrace on the internal raise, got none")
	}
}

// TestCallerStillWorks checks the generalised backtraceFrames keeps Kernel#caller
// dropping its own caller's frame and reporting outer frames file-qualified.
func TestCallerStillWorks(t *testing.T) {
	out, err := runScript(t, `
def a
  b
end
def b
  puts caller.first
end
a
`, "/x/p.rb")
	if err != nil {
		t.Fatal(err)
	}
	if out != "/x/p.rb:0:in 'a'\n" {
		t.Fatalf("got %q", out)
	}
}

// TestEDashFileLabel: a -e one-liner with no frame file labels frames "-e".
func TestEDashFileLabel(t *testing.T) {
	out, err := runScript(t, `
def k
  caller
end
begin
  raise "x"
rescue => e
  puts e.backtrace.last
end
`, "-e")
	if err != nil {
		t.Fatal(err)
	}
	if out != "-e:0:in '<main>'\n" {
		t.Fatalf("got %q", out)
	}
}
