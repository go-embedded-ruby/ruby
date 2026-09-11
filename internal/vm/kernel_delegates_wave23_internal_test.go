// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"runtime"
	"testing"
)

// TestKernelModuleFunctionSplitCoversTheDelegates covers the visibility half of
// registerKernelDelegates: every global function it installs must be listed by
// registerKernelModuleFunctions, so it is a PRIVATE instance method of Kernel
// and a PUBLIC method on the Kernel module — the split MRI gives anything
// declared with rb_define_global_function. Each expectation was read off ruby
// 4.0.5 (Kernel.private_instance_methods(false) / Kernel.public_methods(false)).
func TestKernelModuleFunctionSplitCoversTheDelegates(t *testing.T) {
	for _, name := range []string{
		"`", "gets", "global_variables", "readline", "readlines",
		"select", "spawn", "syscall", "test",
	} {
		src := `p [Kernel.private_instance_methods(false).include?(:"` + name + `"),` +
			` Kernel.public_methods(false).include?(:"` + name + `"),` +
			` Kernel.public_instance_methods(false).include?(:"` + name + `")]`
		if got := eval(t, src); got != "[true, true, false]\n" {
			t.Errorf("Kernel#%s visibility: got %q want %q", name, got, "[true, true, false]\n")
		}
	}
}

// TestKernelGetsReadlineReadlinesForwardToARGF covers the ARGF-forwarding arm of
// registerKernelDelegates. MRI's rb_f_gets / rb_f_readline / rb_f_readlines
// (io.c) forward to ARGF by DISPATCH — `forward(argf, idGets, argc, argv)` — so
// a redefined ARGF.gets is what runs and the arguments reach it unchanged. The
// stubs keep the test off any real input stream.
func TestKernelGetsReadlineReadlinesForwardToARGF(t *testing.T) {
	values := []struct{ src, want string }{
		{"def ARGF.gets(*a); \"g#{a.inspect}\"; end\np gets", `"g[]"`},
		{"def ARGF.gets(*a); \"g#{a.inspect}\"; end\np gets(\"x\", 3)", `"g[\"x\", 3]"`},
		{"def ARGF.readline(*a); \"r#{a.inspect}\"; end\np readline", `"r[]"`},
		{"def ARGF.readlines(*a); [a.size]; end\np readlines(\"s\")", "[1]"},
	}
	for _, c := range values {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestKernelSelectForwardsToIOSelect covers the Kernel#select arm. MRI's
// rb_f_select (io.c) is IO.select under another name: nil when nothing is ready
// within the timeout, and the [readable, writable, exceptional] triple otherwise.
// rbgo reaches its single implementation by dispatch, so the two names cannot
// drift apart — the redefinition case below pins that property.
func TestKernelSelectForwardsToIOSelect(t *testing.T) {
	values := []struct{ src, want string }{
		{`p select([], [], [], 0)`, "nil"},
		{`IO.pipe { |r, w| p select([r], [], [], 0) }`, "nil"},
		{"IO.pipe do |r, w|\n  w.write \"data\"\n  res = select([r], [], [], 0)\n  p [res[0] == [r], res[1], res[2]]\nend", "[true, [], []]"},
		{"def IO.select(*a); [:stub, a.size]; end\np select(1, 2)", "[:stub, 2]"},
	}
	for _, c := range values {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestKernelSpawnForwardsToProcessSpawn covers the Kernel#spawn arm. MRI declares
// spawn twice over one implementation (rb_f_spawn, process.c); rbgo reaches its
// single Process.spawn by dispatch, which is what the stub observes — no
// subprocess is started here.
func TestKernelSpawnForwardsToProcessSpawn(t *testing.T) {
	src := "def Process.spawn(*a); [:spawned, a]; end\np spawn(\"cmd\", 7)"
	if got := eval(t, src); got != "[:spawned, [\"cmd\", 7]]\n" {
		t.Errorf("got %q", got)
	}
}

// TestKernelSyscallIsNotImplemented covers the Kernel#syscall arm. A CGO-free
// runtime has no portable syscall(2), which is the situation CRuby is in without
// HAVE_SYSCALL: io.c ends `#define rb_f_syscall rb_f_notimplement`. MRI 4.0.5 on
// darwin answers with exactly this class and message.
func TestKernelSyscallIsNotImplemented(t *testing.T) {
	class, msg := evalErr(t, `syscall(1)`)
	if class != "NotImplementedError" || msg != "syscall() function is unimplemented on this machine" {
		t.Errorf("got %s: %q", class, msg)
	}
}

// TestKernelBackquoteMethod covers the Kernel#` arm. MRI registers it with arity
// 1 and StringValue (io.c rb_f_backquote), so a non-String converts with #to_str
// and anything else is a TypeError. It is reached here through #send because
// rbgo's parser does not accept a backquote as a method name after a dot; the
// backquote LITERAL compiles to OpXStr, which runs the same runShellCommand, so
// both spellings share one implementation.
func TestKernelBackquoteMethod(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "wasip1" || runtime.GOOS == "js" {
		t.Skip("needs a POSIX shell to run `echo`")
	}
	if got := eval(t, "p send(:`, \"echo hi\")"); got != "\"hi\\n\"\n" {
		t.Errorf("literal command: got %q", got)
	}
	toStr := "o = Object.new\ndef o.to_str\n  \"echo two\"\nend\np Kernel.send(:`, o)"
	if got := eval(t, toStr); got != "\"two\\n\"\n" {
		t.Errorf("#to_str command: got %q", got)
	}
	if class, msg := evalErr(t, "send(:`, \"a\", \"b\")"); class != "ArgumentError" ||
		msg != "wrong number of arguments (given 2, expected 1)" {
		t.Errorf("arity: got %s: %q", class, msg)
	}
	if class, msg := evalErr(t, "send(:`, 5)"); class != "TypeError" ||
		msg != "no implicit conversion of Integer into String" {
		t.Errorf("non-String: got %s: %q", class, msg)
	}
}

// TestKernelGlobalVariables covers the Kernel#global_variables arm.
// rb_f_global_variables (variable.c) walks the global table and answers Symbols;
// the process/exception specials specialGvar serves out of VM state rather than
// the table ($!, $0, $$) are always defined, so they are listed even though the
// table holds no slot for them — that is the alwaysDefinedGvars branch. The list
// is sorted, so a freshly assigned global lands in order and the answer does not
// depend on Go's randomised map iteration.
func TestKernelGlobalVariables(t *testing.T) {
	values := []struct{ src, want string }{
		{`p global_variables.class`, "Array"},
		{`p global_variables.first.class`, "Symbol"},
		{`p global_variables.include?(:$stdout)`, "true"},
		// The specials that live outside the table.
		{`p [:$!, :$0, :"$$"].all? { |g| global_variables.include?(g) }`, "true"},
		// A newly assigned global appears; the count rises by exactly one.
		{"before = global_variables\n$k23_fresh = 1\nafter = global_variables\np [before.include?(:$k23_fresh), after.include?(:$k23_fresh), after.size - before.size]",
			"[false, true, 1]"},
		{`p global_variables == global_variables.sort`, "true"},
	}
	for _, c := range values {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestKernelExitStatus covers exitStatusArg and raiseSystemExit. MRI's rb_f_exit
// (process.c) maps the status through exit_status_code — true is EXIT_SUCCESS,
// false is EXIT_FAILURE, anything else NUM2INT — and raises
// SystemExit.new(status, "exit"); exit! differs only in defaulting to
// EXIT_FAILURE. Every expectation below was read off ruby 4.0.5.
func TestKernelExitStatus(t *testing.T) {
	probe := func(call string) string {
		t.Helper()
		return eval(t, "begin\n  "+call+"\nrescue SystemExit => e\n  p [e.status, e.message]\nend")
	}
	values := []struct{ call, want string }{
		{`exit`, `[0, "exit"]`},
		{`exit 0`, `[0, "exit"]`},
		{`exit 8`, `[8, "exit"]`},
		{`exit(-65536)`, `[-65536, "exit"]`},
		{`exit 65536`, `[65536, "exit"]`},
		{`exit true`, `[0, "exit"]`},
		{`exit false`, `[1, "exit"]`},
		// NUM2INT truncates a Float towards zero (through #to_int).
		{`exit 5.5`, `[5, "exit"]`},
		{`exit(-2.2)`, `[-2, "exit"]`},
		{`exit 827.999`, `[827, "exit"]`},
		// exit! defaults to EXIT_FAILURE but takes the same status mapping.
		{`exit!`, `[1, "exit"]`},
		{`exit! 7`, `[7, "exit"]`},
		{`exit! true`, `[0, "exit"]`},
		// Reachable as a public Kernel-module method too.
		{`Kernel.exit(3)`, `[3, "exit"]`},
		{`Kernel.exit!`, `[1, "exit"]`},
	}
	for _, c := range values {
		if got := probe(c.call); got != c.want+"\n" {
			t.Errorf("%s: got %q want %q", c.call, got, c.want+"\n")
		}
	}
	// A status with no #to_int is a TypeError, and 2+ arguments an ArgumentError.
	errs := []struct{ src, class, msg string }{
		{`exit Object.new`, "TypeError", "no implicit conversion of Object into Integer"},
		{`exit "0"`, "TypeError", "no implicit conversion of String into Integer"},
		{`exit [0]`, "TypeError", "no implicit conversion of Array into Integer"},
		{`exit 1, 2`, "ArgumentError", "wrong number of arguments (given 2, expected 0..1)"},
		{`exit! 1, 2`, "ArgumentError", "wrong number of arguments (given 2, expected 0..1)"},
	}
	for _, c := range errs {
		if class, msg := evalErr(t, c.src); class != c.class || msg != c.msg {
			t.Errorf("%s: got %s: %q want %s: %q", c.src, class, msg, c.class, c.msg)
		}
	}
	// A custom #to_int is honoured, as NUM2INT does.
	got := eval(t, "o = Object.new\ndef o.to_int\n  5\nend\nbegin\n  exit o\nrescue SystemExit => e\n  p e.status\nend")
	if got != "5\n" {
		t.Errorf("#to_int status: got %q", got)
	}
}

// TestKernelAbortStatusAndMessage covers the abort arm. MRI's rb_f_abort
// (process.c) puts the message — through StringValue, so #to_str converts — on
// stderr and raises SystemExit.new(EXIT_FAILURE, message); with no argument the
// message is the plain "exit". Unlike exit, the status never comes from the
// argument.
func TestKernelAbortStatusAndMessage(t *testing.T) {
	// eval's VM sends $stderr to the same buffer as $stdout (errOut defaults to
	// out), so the message abort puts on stderr precedes the probe's own line —
	// which is also how the message reaching stderr at all is asserted.
	values := []struct{ src, want string }{
		{"begin\n  abort\nrescue SystemExit => e\n  p [e.status, e.message]\nend", `[1, "exit"]`},
		{"begin\n  abort \"boom\"\nrescue SystemExit => e\n  p [e.status, e.message]\nend", "boom\n" + `[1, "boom"]`},
		{"o = Object.new\ndef o.to_str\n  \"strish\"\nend\nbegin\n  abort o\nrescue SystemExit => e\n  p [e.status, e.message]\nend", "strish\n" + `[1, "strish"]`},
	}
	for _, c := range values {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	errs := []struct{ src, class, msg string }{
		{`abort 1`, "TypeError", "no implicit conversion of Integer into String"},
		{`abort "a", "b"`, "ArgumentError", "wrong number of arguments (given 2, expected 0..1)"},
	}
	for _, c := range errs {
		if class, msg := evalErr(t, c.src); class != c.class || msg != c.msg {
			t.Errorf("%s: got %s: %q want %s: %q", c.src, class, msg, c.class, c.msg)
		}
	}
}

// TestRaiseAppliesTheBacktraceArgument covers applyRaiseBacktrace and the
// len(args) >= 3 arm of nativeRaise. MRI's make_exception ends
// `if (argc == 3) set_backtrace(mesg, argv[2])` (eval.c): the value goes through
// the exception's own #set_backtrace, so a String becomes a one-element array, an
// Array of String is taken as-is, nil clears it, and a non-String, non-Location
// element is a TypeError. Thread::Backtrace::Location values are accepted too and
// converted to their #to_s lines, which is exactly what
// Exception#backtrace_locations parses back.
func TestRaiseAppliesTheBacktraceArgument(t *testing.T) {
	values := []struct{ src, want string }{
		{"begin\n  raise ArgumentError, \"m\", [\"line1\", \"line2\"]\nrescue => e\n  p e.backtrace\nend",
			`["line1", "line2"]`},
		{"begin\n  raise ArgumentError, \"m\", \"single\"\nrescue => e\n  p e.backtrace\nend",
			`["single"]`},
		{"begin\n  raise ArgumentError, \"m\", []\nrescue => e\n  p e.backtrace\nend", `[]`},
		// A Location array round-trips through #to_s and back through
		// #backtrace_locations.
		{"locs = caller_locations(0, 2)\nbegin\n  raise ArgumentError, \"m\", locs\nrescue => e\n  p e.backtrace == locs.map(&:to_s)\n  p e.backtrace_locations.map(&:to_s) == locs.map(&:to_s)\nend",
			"true\ntrue"},
		// The backtrace argument coexists with the cause: keyword.
		{"c = StandardError.new(\"cm\")\nbegin\n  raise ArgumentError, \"m\", [\"l1\"], cause: c\nrescue => e\n  p [e.backtrace, e.cause.message]\nend",
			`[["l1"], "cm"]`},
		// nil clears the backtrace rather than stamping the current stack.
		{"begin\n  raise ArgumentError, \"m\", nil\nrescue => e\n  p e.backtrace.nil?\nend", "false"},
	}
	for _, c := range values {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// A non-String, non-Location element is rejected by #set_backtrace, as MRI.
	if class, msg := evalErr(t, `raise ArgumentError, "m", [1, 2]`); class != "TypeError" ||
		msg != "backtrace must be an Array of String or an Array of Thread::Backtrace::Location" {
		t.Errorf("got %s: %q", class, msg)
	}
}

// TestClassMethodsListsTheSingletonChain covers the class/module arm of
// Kernel#methods and mergeMethodNames. rb_obj_methods lists the instance methods
// of CLASS_OF(obj) (object.c); for a class that is its SINGLETON class, whose
// ancestry runs through the singleton classes of its superclasses before reaching
// Class/Module/Object — so a class method, an inherited class method and Class's
// own instance methods are all listed, each exactly once. Verified against
// ruby 4.0.5.
func TestClassMethodsListsTheSingletonChain(t *testing.T) {
	const defs = "class K23Base\n" +
		"  def self.inherited_cm; end\n" +
		"end\n" +
		"class K23Sub < K23Base\n" +
		"  def self.own_cm; end\n" +
		"  class << self\n" +
		"    def meta_cm; end\n" +
		"    private\n" +
		"    def priv_cm; end\n" +
		"  end\n" +
		"  def inst_m; end\n" +
		"end\n"
	values := []struct{ src, want string }{
		{defs + `p K23Sub.methods.include?(:own_cm)`, "true"},
		{defs + `p K23Sub.methods.include?(:meta_cm)`, "true"},
		{defs + `p K23Sub.methods.include?(:inherited_cm)`, "true"},
		// Class's own instance methods survive the merge.
		{defs + `p K23Sub.methods.include?(:new)`, "true"},
		{defs + `p K23Sub.methods.include?(:instance_methods)`, "true"},
		// A private class method is excluded, and an instance method is not a
		// method OF the class object.
		{defs + `p K23Sub.methods.include?(:priv_cm)`, "false"},
		{defs + `p K23Sub.methods.include?(:inst_m)`, "false"},
		// Listed exactly once each, and sorted (the merge de-duplicates).
		{defs + `p K23Sub.methods.count(:own_cm)`, "1"},
		{defs + `p K23Sub.methods.count(:new)`, "1"},
		{defs + `p K23Sub.methods == K23Sub.methods.uniq`, "true"},
		{defs + `p K23Sub.methods == K23Sub.methods.sort`, "true"},
		// methods(false) still reports only the class's OWN class methods.
		{defs + `p K23Sub.methods(false).sort`, "[:meta_cm, :own_cm]"},
		// The intersection the ruby/spec example takes is no longer empty.
		{defs + `p (K23Sub.methods(false) & K23Sub.methods).sort`, "[:meta_cm, :own_cm]"},
		// A module receiver takes the same path: its own class method and Module's
		// public instance methods are listed, while Module#module_function — a
		// PRIVATE instance method — is not.
		{"module K23Mod\n  def self.mod_cm; end\nend\np [K23Mod.methods.include?(:mod_cm), K23Mod.methods.include?(:instance_methods), K23Mod.methods.include?(:module_function)]", "[true, true, false]"},
		// A plain object is unaffected: its per-object singleton method is listed.
		{"o = Object.new\ndef o.solo; end\np [o.methods.include?(:solo), o.methods.include?(:frozen?)]", "[true, true]"},
	}
	for _, c := range values {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
