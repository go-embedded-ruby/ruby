// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !wasm

package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// runSrcErr runs a program and returns its stdout plus whatever error came out
// of Run, so a test can assert on a program that ends by raising or exiting.
// runSrc (aot_dispatch_test.go) t.Fatals on an error, which is the wrong shape
// for everything here.
func runSrcErr(t *testing.T, src string) (*VM, string, error) {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	machine := New(&buf)
	_, runErr := machine.Run(iseq)
	return machine, buf.String(), runErr
}

// TestSelfSignalRaisesInsteadOfKilling is the witness for issue #691: on
// origin/main this program's process DIED (128+15) between 10% and 20% of runs
// and printed nothing the rest of the time, because Process.kill called kill(2)
// and then raced the Go runtime's default disposition against the program's own
// exit. signal.c rb_f_kill never sends a deferrable signal to itself; it enqueues
// it and runs rb_thread_execute_interrupts() before returning, so the raise
// happens synchronously.
//
// A/B: this test cannot pass on the old code even in the runs where the process
// survived, because no exception was raised at all there either ("NO RAISE - kept
// running" in the issue). Reverting vm.selfSignal's call site in
// defineProcessKill to an unconditional procKill still compiles and fails here.
func TestSelfSignalRaisesInsteadOfKilling(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// rb_signal_exec's cmd == 0 arm: SIGTERM and the rest of default_handler's
		// first group become SignalException, carrying #signo and #signm.
		{"TERM", `begin
  Process.kill(:TERM, Process.pid)
rescue SignalException => e
  puts "#{e.class} #{e.message} #{e.signo} #{e.signm}"
end
puts "alive"`, "SignalException SIGTERM 15 SIGTERM\nalive\n"},
		// SIGINT is the one case rb_signal_exec routes to rb_interrupt() instead,
		// and interrupt_init leaves the message EMPTY.
		{"INT", `begin
  Process.kill(:INT, Process.pid)
rescue Interrupt => e
  puts "#{e.class} #{e.message.inspect} #{e.signo}"
end
puts "alive"`, "Interrupt \"\" 2\nalive\n"},
		// Interrupt < SignalException, so the wider rescue catches it.
		{"INT rescued as SignalException", `begin
  Process.kill("SIGINT", Process.pid)
rescue SignalException => e
  puts e.class
end`, "Interrupt\n"},
		// A trap runs with the signal NUMBER as its argument (signal_exec ->
		// rb_eval_cmd_kw(cmd, [INT2NUM(sig)])).
		{"trap block", `Signal.trap(:TERM) { |s| puts "trapped #{s}" }
Process.kill(:TERM, Process.pid)
puts "after"`, "trapped 15\nafter\n"},
		// A trap installed as a Proc rather than a block.
		{"trap proc", `Signal.trap(:HUP, ->(s) { puts "hup #{s}" })
Process.kill(:HUP, Process.pid)`, "hup 1\n"},
		// "IGNORE" discards it: signal_ignored() == 1, so MRI neither sends the
		// signal nor runs anything.
		{"IGNORE", `Signal.trap(:TERM, "IGNORE")
Process.kill(:TERM, Process.pid)
puts "after"`, "after\n"},
		{"SIG_IGN", `Signal.trap(:TERM, "SIG_IGN")
Process.kill(:TERM, Process.pid)
puts "after"`, "after\n"},
		{"nil handler ignores", `Signal.trap(:TERM, nil)
Process.kill(:TERM, Process.pid)
puts "after"`, "after\n"},
		// "DEFAULT" restores default_handler(sig), which for TERM is MRI's own
		// handler — so the SignalException comes back rather than the OS default.
		{"DEFAULT restores the raise", `Signal.trap(:TERM) { puts "no" }
Signal.trap(:TERM, "DEFAULT")
begin
  Process.kill(:TERM, Process.pid)
rescue SignalException => e
  puts "back to #{e.message}"
end`, "back to SIGTERM\n"},
		// "EXIT" is rb_threadptr_signal_exit: SystemExit with EXIT_FAILURE.
		{"EXIT", `Signal.trap(:TERM, "EXIT")
begin
  Process.kill(:TERM, Process.pid)
rescue SystemExit => e
  puts "exit #{e.status}"
end`, "exit 1\n"},
		// Process.kill(0, pid) is a liveness probe and must reach the OS, returning
		// the number of pids signalled.
		{"signal 0 probes", `p Process.kill(0, Process.pid)`, "1\n"},
		// A signal MRI leaves to the OS raises nothing here, so the trap list has
		// to report SYSTEM_DEFAULT rather than DEFAULT for it (trap()'s oldcmd).
		{"trap previous handler", `p Signal.trap(:HUP, "IGNORE")
p Signal.trap(:HUP, "DEFAULT")
p Signal.trap(:WINCH, "IGNORE")`, "\"DEFAULT\"\n\"IGNORE\"\n\"SYSTEM_DEFAULT\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := runSrcErr(t, tc.src)
			if err != nil {
				t.Fatalf("run: %v (output %q)", err, out)
			}
			if out != tc.want {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
		})
	}
}

// TestSelfSignalFallsThroughToTheOS pins the two dispositions where rb_f_kill
// really does call kill(2) on this process, by swapping the procKill seam rather
// than signalling the test binary. Without the seam this could only be tested by
// killing the test process, which is why the seam is what the assertion reads.
func TestSelfSignalFallsThroughToTheOS(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		wantSig   int
	}{
		// SIGKILL cannot be deferred to a safe point: rb_f_kill's inner switch
		// calls kill(pid, sig) for it directly.
		{"KILL is delivered for real", `Process.kill(:KILL, Process.pid)`, 9},
		// "SYSTEM_DEFAULT" sets SIG_DFL, so signal_ignored() returns -1 and
		// rb_f_kill falls through to kill(2).
		{"SYSTEM_DEFAULT is delivered for real",
			`Signal.trap(:TERM, "SYSTEM_DEFAULT")
Process.kill(:TERM, Process.pid)`, 15},
		// A signal MRI installs no handler for has SIG_DFL disposition too.
		{"an unmanaged signal is delivered for real", `Process.kill(:WINCH, Process.pid)`, 28},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orig := procKill
			t.Cleanup(func() { procKill = orig })
			var gotPid, gotSig int
			calls := 0
			procKill = func(pid, sig int) error {
				calls++
				gotPid, gotSig = pid, sig
				return nil
			}
			if _, out, err := runSrcErr(t, tc.src); err != nil {
				t.Fatalf("run: %v (output %q)", err, out)
			}
			if calls != 1 {
				t.Fatalf("procKill called %d times, want 1", calls)
			}
			if gotSig != tc.wantSig {
				t.Errorf("signal = %d, want %d", gotSig, tc.wantSig)
			}
			if gotPid <= 0 {
				t.Errorf("pid = %d, want this process", gotPid)
			}
		})
	}
}

// TestSelfSignalOnlyShortCircuitsThisProcess proves the synchronous arm is
// reached only for pid == self and only for a positive signal: another pid, and a
// process-GROUP kill even of our own group, must still go to the OS. A version
// that tested `pid == self` alone would pass while silently swallowing
// Process.kill(-15, Process.getpgrp).
func TestSelfSignalOnlyShortCircuitsThisProcess(t *testing.T) {
	orig := procKill
	t.Cleanup(func() { procKill = orig })
	var sent [][2]int
	procKill = func(pid, sig int) error { sent = append(sent, [2]int{pid, sig}); return nil }

	src := `Process.kill(:TERM, 999999) rescue nil
Process.kill(-15, Process.pid) rescue nil
begin
  Process.kill(:TERM, Process.pid)
rescue SignalException
  puts "self raised"
end`
	_, out, err := runSrcErr(t, src)
	if err != nil {
		t.Fatalf("run: %v (output %q)", err, out)
	}
	if out != "self raised\n" {
		t.Errorf("output = %q, want %q", out, "self raised\n")
	}
	if len(sent) != 2 {
		t.Fatalf("procKill calls = %v, want exactly the foreign pid and the group kill", sent)
	}
	if sent[0][0] != 999999 || sent[0][1] != 15 {
		t.Errorf("foreign kill = %v, want [999999 15]", sent[0])
	}
	// killpg(pgid, sig) == kill(-pgid, sig): the pid is negated, the signal is not.
	if sent[1][0] >= 0 || sent[1][1] != 15 {
		t.Errorf("group kill = %v, want a negative pid with signal 15", sent[1])
	}
}

// TestSignalExceptionConstruction covers signal.c esignal_init and
// interrupt_init directly: the name branch, the number branch, the explicit
// message, and each ArgumentError.
func TestSignalExceptionConstruction(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// The name branch prefixes SIG when the name was given without one.
		{"from a name", `e = SignalException.new("TERM"); puts "#{e.message} #{e.signo}"`, "SIGTERM 15\n"},
		{"from a prefixed name", `e = SignalException.new("SIGTERM"); puts "#{e.message} #{e.signo}"`, "SIGTERM 15\n"},
		{"from a symbol", `e = SignalException.new(:HUP); puts "#{e.message} #{e.signo}"`, "SIGHUP 1\n"},
		// The number branch defaults the message through rb_signo2signm.
		{"from a number", `e = SignalException.new(15); puts "#{e.message} #{e.signo}"`, "SIGTERM 15\n"},
		{"from a number with a message", `e = SignalException.new(15, "boom"); puts "#{e.message} #{e.signo}"`, "boom 15\n"},
		// rb_signo2signm falls back to "SIG%u" for a number no name carries.
		{"an unnamed number", `e = SignalException.new(62); puts "#{e.message} #{e.signo}"`, "SIG62 62\n"},
		// #signm is an alias of #message (rb_alias in Init_signal).
		{"signm aliases message", `e = SignalException.new(15, "boom"); p e.signm == e.message`, "true\n"},
		// interrupt_init fixes the signal at SIGINT and leaves the message empty.
		// Interrupt.new leaves the message unset, so Exception#message falls back
		// to the class name. The Interrupt rb_interrupt() raises carries an explicit
		// empty message instead — see TestInterruptMessageFallsBackToTheClassName.
		{"Interrupt", `e = Interrupt.new; puts "#{e.message.inspect} #{e.signo}"`, "\"Interrupt\" 2\n"},
		{"Interrupt with a message", `e = Interrupt.new("ouch"); puts "#{e.message} #{e.signo}"`, "ouch 2\n"},
		// Errors. A name nothing matches, and a number outside NSIG.
		{"unknown name", `begin; SignalException.new("NOPE"); rescue ArgumentError => e; puts e.message; end`,
			"unsupported signal 'SIGNOPE'\n"},
		{"number too large", `begin; SignalException.new(9999); rescue ArgumentError => e; puts e.message; end`,
			"invalid signal number (9999)\n"},
		{"no argument", `begin; SignalException.new; rescue ArgumentError => e; puts e.message; end`,
			"wrong number of arguments (given 0, expected 1..2)\n"},
		// The name branch takes exactly one argument (argnum stays 1).
		{"a name with a second argument", `begin; SignalException.new("TERM", "x"); rescue ArgumentError => e; puts e.message; end`,
			"wrong number of arguments (given 2, expected 1)\n"},
		// The NUMBER branch takes 1..2 (argnum becomes 2), so three is the error.
		{"a number with two extra arguments",
			`begin; SignalException.new(15, "x", "y"); rescue ArgumentError => e; puts e.message; end`,
			"wrong number of arguments (given 3, expected 1..2)\n"},
		// rb_check_to_integer consults #to_int, so an object that answers it takes
		// the NUMBER branch and gets the default message from rb_signo2signm.
		{"an object with #to_int", `o = Object.new
def o.to_int; 15; end
e = SignalException.new(o); puts "#{e.message} #{e.signo}"`, "SIGTERM 15\n"},
		// ...and one whose #to_int hands back a non-Integer falls through to the
		// NAME branch, where its #to_str (absent) makes it a bad signal type.
		{"an object whose #to_int is not an Integer", `o = Object.new
def o.to_int; "nope"; end
begin; SignalException.new(o); rescue ArgumentError => e; puts e.message; end`,
			"bad signal type Object\n"},
		{"an object that is neither a number nor a name", `begin
  SignalException.new(Object.new)
rescue ArgumentError => e
  puts e.message
end`, "bad signal type Object\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := runSrcErr(t, tc.src)
			if err != nil {
				t.Fatalf("run: %v (output %q)", err, out)
			}
			if out != tc.want {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
		})
	}
}

// TestSignalListAndSignameCanonicalNames pins the two properties
// core/signal/list_spec.rb and core/signal/signame_spec.rb assert and that a
// Go map cannot provide on its own: EXIT is a member with number 0, and a
// number shared by a canonical name and an alias resolves to the CANONICAL one.
// With signalNames built from an unordered map this test would fail at random,
// which is exactly the shape of defect an ordered siglist exists to prevent.
func TestSignalListAndSignameCanonicalNames(t *testing.T) {
	src := `p Signal.list["EXIT"], Signal.list["KILL"]
p Signal.list["CLD"] == Signal.list["CHLD"]
p Signal.list["IOT"] == Signal.list["ABRT"]
p Signal.signame(Signal.list["ABRT"]), Signal.signame(Signal.list["CHLD"])
p Signal.signame(0), Signal.signame(-1), Signal.signame(9999)`
	want := "0\n9\ntrue\ntrue\n\"ABRT\"\n\"CHLD\"\n\"EXIT\"\nnil\nnil\n"
	_, out, err := runSrcErr(t, src)
	if err != nil {
		t.Fatalf("run: %v (output %q)", err, out)
	}
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	// Every key Signal.list reports must be a name ruby/spec's own RUBY_SIGNALS
	// list knows; a name outside it fails core/signal/list_spec.rb.
	known := map[string]bool{}
	for _, n := range strings.Fields(`EXIT HUP INT QUIT ILL TRAP IOT ABRT EMT FPE KILL BUS SEGV SYS
PIPE ALRM TERM URG STOP TSTP CONT CHLD CLD TTIN TTOU IO XCPU XFSZ VTALRM PROF
WINCH USR1 USR2 LOST MSG PWR POLL DANGER MIGRATE PRE GRANT RETRACT SOUND INFO`) {
		known[n] = true
	}
	for _, e := range siglist {
		if !known[e.name] {
			t.Errorf("siglist has %q, which ruby/spec's RUBY_SIGNALS does not list", e.name)
		}
	}
}

// TestSignameCoercesThroughToInt covers sig_signame's NUM2INT: a non-Integer
// converts through #to_int, and a #to_int that hands back a non-Integer is a
// TypeError rather than a silent nil.
func TestSignameCoercesThroughToInt(t *testing.T) {
	src := `o = Object.new
def o.to_int; 0; end
p Signal.signame(o)
begin; Signal.signame("hello"); rescue TypeError => e; puts "TypeError"; end
b = Object.new
def b.to_int; "not an int"; end
begin; Signal.signame(b); rescue TypeError => e; puts "TypeError"; end`
	want := "\"EXIT\"\nTypeError\nTypeError\n"
	_, out, err := runSrcErr(t, src)
	if err != nil {
		t.Fatalf("run: %v (output %q)", err, out)
	}
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestSignoToSignmFallback exercises rb_signo2signm's sprintf branch and
// withSIG's prefixing rule at the Go level, since neither is reachable through
// Ruby for every input.
func TestSignoToSignmFallback(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want string
	}{{15, "SIGTERM"}, {0, "SIGEXIT"}, {62, "SIG62"}, {-1, "SIG-1"}} {
		if got := signoToSignm(tc.in); got != tc.want {
			t.Errorf("signoToSignm(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct{ in, want string }{
		{"TERM", "SIGTERM"}, {"SIGTERM", "SIGTERM"}, {"SIG", "SIGSIG"},
	} {
		if got := withSIG(tc.in); got != tc.want {
			t.Errorf("withSIG(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, ok := signalNumberOf("NOPE"); ok {
		t.Error("signalNumberOf(NOPE) reported a number")
	}
}

// TestSigNumberPanicsOnATypo proves sigNumber's guard fires, so a misspelt name
// inside this package cannot silently become signal 0 (which kill(2) treats as a
// liveness probe and would therefore look like it worked).
func TestSigNumberPanicsOnATypo(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("sigNumber accepted an unknown name")
		}
	}()
	_ = sigNumber("NOT_A_SIGNAL")
}

// TestTrapArgumentErrors covers Signal.trap's own arity and coercion errors.
func TestTrapArgumentErrors(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"no arguments", `begin; Signal.trap; rescue ArgumentError => e; puts e.message; end`,
			"wrong number of arguments (given 0, expected 1..2)\n"},
		{"one argument and no block", `begin; Signal.trap(:TERM); rescue ArgumentError => e; puts e.message; end`,
			"tried to create Proc object without a block\n"},
		{"an unknown signal name", `begin; Signal.trap("NOPE", "IGNORE"); rescue ArgumentError => e; puts e.message; end`,
			"unsupported signal 'SIGNOPE'\n"},
		{"a signal number out of range", `begin; Signal.trap(9999, "IGNORE"); rescue ArgumentError => e; puts e.message; end`,
			"invalid signal number (9999)\n"},
		// trap_signm accepts a number, including 0 (Signal.trap(:EXIT)'s slot).
		{"a numeric signal", `p Signal.trap(15, "IGNORE")`, "\"DEFAULT\"\n"},
		{"Kernel#trap is the same operation", `trap(:TERM, "IGNORE")
Process.kill(:TERM, Process.pid)
puts "after"`, "after\n"},
		// A command that is neither a Proc nor one of the keywords is #call'd.
		{"an arbitrary callable", `h = Object.new
def h.call(s); puts "called #{s}"; end
Signal.trap(:HUP, h)
Process.kill(:HUP, Process.pid)`, "called 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := runSrcErr(t, tc.src)
			if err != nil {
				t.Fatalf("run: %v (output %q)", err, out)
			}
			if out != tc.want {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
		})
	}
}

// TestTrapRefusesReservedAndUntrappableSignals covers signal.c sig_trap's two
// refusals, which happen BEFORE it looks at the handler: reserved_signal_p names
// the signals MRI needs its own handlers for, and SIGKILL/SIGSTOP are the two
// sigaction(2) rejects outright, which trap() reports as Errno::EINVAL through
// rb_sys_fail_str rather than as ArgumentError.
func TestTrapRefusesReservedAndUntrappableSignals(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"SEGV", `begin; Signal.trap(:SEGV) {}; rescue ArgumentError => e; puts e.message; end`,
			"can't trap reserved signal: SIGSEGV\n"},
		{"BUS", `begin; Signal.trap(:BUS) {}; rescue ArgumentError => e; puts e.message; end`,
			"can't trap reserved signal: SIGBUS\n"},
		{"ILL", `begin; Signal.trap(:ILL) {}; rescue ArgumentError => e; puts e.message; end`,
			"can't trap reserved signal: SIGILL\n"},
		{"FPE", `begin; Signal.trap(:FPE) {}; rescue ArgumentError => e; puts e.message; end`,
			"can't trap reserved signal: SIGFPE\n"},
		{"VTALRM", `begin; Signal.trap(:VTALRM) {}; rescue ArgumentError => e; puts e.message; end`,
			"can't trap reserved signal: SIGVTALRM\n"},
		// Errno::EINVAL, not ArgumentError: the refusal comes from the kernel.
		{"KILL", `begin; Signal.trap(:KILL) {}; rescue StandardError => e; puts e.class; end`,
			"Errno::EINVAL\n"},
		{"STOP", `begin; Signal.trap(:STOP) {}; rescue StandardError => e; puts e.class; end`,
			"Errno::EINVAL\n"},
		// The refusal comes first: a reserved signal is refused even with a handler
		// that trap_handler would have accepted.
		{"reserved beats the handler", `begin; Signal.trap(:SEGV, "IGNORE"); rescue ArgumentError => e; puts "refused"; end`,
			"refused\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := runSrcErr(t, tc.src)
			if err != nil {
				t.Fatalf("run: %v (output %q)", err, out)
			}
			if out != tc.want {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
		})
	}
}

// TestTrapSignalArgumentCoercion is signal.c trap_signm plus signm2signo's
// argument coercion. The two negatives matter most and are easy to get backwards:
// trap_signm tests FIXNUM_P, so #to_int is NEVER called (a Float is a bad type,
// not a truncated number), while signm2signo DOES call #to_str. A single
// "coerce to a number somehow" implementation passes the positive rows and fails
// both negatives, which is what origin/main did.
func TestTrapSignalArgumentCoercion(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a String name", `p Signal.trap("HUP", "IGNORE")`, "\"DEFAULT\"\n"},
		{"a prefixed name", `p Signal.trap("SIGHUP", "IGNORE")`, "\"DEFAULT\"\n"},
		{"a Symbol name", `p Signal.trap(:HUP, "IGNORE")`, "\"DEFAULT\"\n"},
		{"an Integer", `p Signal.trap(Signal.list["HUP"], "IGNORE")`, "\"DEFAULT\"\n"},
		// EXIT / 0 is legal for trap (signm2signo's exit = TRUE) ...
		{"EXIT is a trappable name", `p Signal.trap(:EXIT, "IGNORE")`, "\"SYSTEM_DEFAULT\"\n"},
		{"signal 0 is trappable", `p Signal.trap(0, "IGNORE")`, "\"SYSTEM_DEFAULT\"\n"},
		// ... but NOT for SignalException, whose esignal_init passes exit = FALSE.
		{"EXIT is not a SignalException name",
			`begin; SignalException.new("EXIT"); rescue ArgumentError => e; puts e.message; end`,
			"unsupported signal 'SIGEXIT'\n"},
		// #to_str is consulted.
		{"#to_str", `o = Object.new
def o.to_str; "HUP"; end
p Signal.trap(o, "IGNORE")`, "\"DEFAULT\"\n"},
		// #to_int is NOT.
		{"#to_int is never called", `o = Object.new
def o.to_int; raise "to_int must not be called"; end
begin; Signal.trap(o, "IGNORE"); rescue ArgumentError => e; puts e.message; end`,
			"bad signal type Object\n"},
		// rb_obj_classname, so nil is NilClass rather than "nil".
		{"nil names its class", `begin; Signal.trap(nil, "IGNORE"); rescue ArgumentError => e; puts e.message; end`,
			"bad signal type NilClass\n"},
		{"a Float is a bad type, not a truncation",
			`begin; Signal.trap(100.0, "IGNORE"); rescue ArgumentError => e; puts e.message; end`,
			"bad signal type Float\n"},
		{"true names its class", `begin; Signal.trap(true, "IGNORE"); rescue ArgumentError => e; puts e.message; end`,
			"bad signal type TrueClass\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := runSrcErr(t, tc.src)
			if err != nil {
				t.Fatalf("run: %v (output %q)", err, out)
			}
			if out != tc.want {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
		})
	}
}

// TestTrapNilReportsNilNotIgnore pins trap()'s oldcmd switch, which has an
// explicit `case Qnil: break` that leaves a nil handler reporting back as nil —
// where "IGNORE" and "" both rewrite it to the string "IGNORE". Both DISPOSITIONS
// are the same (the signal is discarded), so a test that only checked the
// behaviour would pass with them collapsed.
func TestTrapNilReportsNilNotIgnore(t *testing.T) {
	src := `Signal.trap(:HUP, nil)
p Signal.trap(:HUP, "DEFAULT")
Signal.trap(:HUP, "IGNORE")
p Signal.trap(:HUP, "DEFAULT")
Signal.trap(:HUP, "")
p Signal.trap(:HUP, "DEFAULT")
Signal.trap(:TERM, nil)
Process.kill(:TERM, Process.pid)
puts "a nil handler still discards the signal"`
	want := "nil\n\"IGNORE\"\n\"IGNORE\"\na nil handler still discards the signal\n"
	_, out, err := runSrcErr(t, src)
	if err != nil {
		t.Fatalf("run: %v (output %q)", err, out)
	}
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestInterruptMessageFallsBackToTheClassName pins the difference measured on MRI
// 4.0.5 between the two ways an Interrupt comes into being: Interrupt.new leaves
// the message unset, so Exception#message answers the class name ("Interrupt"),
// while the one rb_interrupt() raises carries an explicit EMPTY message. Setting
// @message unconditionally in #initialize satisfies the raised case and breaks
// core/exception/interrupt_spec.rb, which is how it was found.
func TestInterruptMessageFallsBackToTheClassName(t *testing.T) {
	src := `p Interrupt.new.message, Interrupt.new.signm, Interrupt.new.signo
p Interrupt.new("x").message
begin
  Process.kill(:INT, Process.pid)
rescue Interrupt => e
  p e.message, e.signo
end`
	want := "\"Interrupt\"\n\"Interrupt\"\n2\n\"x\"\n\"\"\n2\n"
	_, out, err := runSrcErr(t, src)
	if err != nil {
		t.Fatalf("run: %v (output %q)", err, out)
	}
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// stubValue is an object.Value of a type classOf cannot place, which is the only
// way to reach objClassName's fallback: classOf ends in classOfPlatform, whose
// Windows form is a plain `return nil`. No Ruby expression can produce one, so
// the branch is reachable only from Go — and leaving it uncovered would leave a
// nil dereference in an error path untested.
type stubValue struct{}

func (stubValue) ToS() string     { return "stub" }
func (stubValue) Inspect() string { return "stub" }
func (stubValue) Truthy() bool    { return true }

func TestObjClassNameFallsBackWhenClassOfCannotPlaceTheValue(t *testing.T) {
	machine, _, _ := runSrcErr(t, `1`)
	if got := machine.objClassName(object.NilV); got != "NilClass" {
		t.Errorf("objClassName(nil) = %q, want NilClass", got)
	}
	if got := machine.objClassName(stubValue{}); got == "" {
		t.Error("objClassName gave no name for an unplaceable value")
	}
}

// TestSelfSignalOnSIGCHLDRaisesNothing covers the one member of
// default_handler's managed set that rb_signal_exec has NO cmd == 0 case for:
// Init_signal installs a handler for SIGCHLD to reap children, so the signal is
// consumed at a safe point, but nothing is raised. A reading that treated "MRI
// installs a handler" as "MRI raises" would raise SignalException here.
func TestSelfSignalOnSIGCHLDRaisesNothing(t *testing.T) {
	orig := procKill
	t.Cleanup(func() { procKill = orig })
	calls := 0
	procKill = func(int, int) error { calls++; return nil }
	_, out, err := runSrcErr(t, `Process.kill(:CHLD, Process.pid); puts "nothing raised"`)
	if err != nil {
		t.Fatalf("run: %v (output %q)", err, out)
	}
	if out != "nothing raised\n" {
		t.Errorf("output = %q, want %q", out, "nothing raised\n")
	}
	if calls != 0 {
		t.Errorf("procKill called %d times; SIGCHLD must be consumed at the safe point", calls)
	}
}

// TestTrapPreviousReportsEveryDisposition walks every arm of trap()'s oldcmd
// switch. SYSTEM_DEFAULT and EXIT are only observable through this return value —
// their DISPOSITIONS are "let the OS have it" and "raise SystemExit", neither of
// which names itself — so a test of behaviour alone leaves both unwitnessed.
func TestTrapPreviousReportsEveryDisposition(t *testing.T) {
	src := `p Signal.trap(:TERM, "SYSTEM_DEFAULT")
p Signal.trap(:TERM, "EXIT")
p Signal.trap(:TERM, "SIG_DFL")
p Signal.trap(:TERM, ->(s) {})
p Signal.trap(:TERM, "IGNORE").class
p Signal.trap(:TERM, "command string")
p Signal.trap(:TERM, "DEFAULT")`
	want := "\"DEFAULT\"\n\"SYSTEM_DEFAULT\"\n\"EXIT\"\n\"DEFAULT\"\nProc\n\"IGNORE\"\n\"command string\"\n"
	_, out, err := runSrcErr(t, src)
	if err != nil {
		t.Fatalf("run: %v (output %q)", err, out)
	}
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}
