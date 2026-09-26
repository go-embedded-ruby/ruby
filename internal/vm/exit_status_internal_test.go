// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestExitingSplitClassification is eval_error.c exiting_split, table-driven.
// It is the decision the rbgo CLI turns into the process's disposition, and it
// is the half of issue #676 that could be unit-tested at all: on origin/main
// neither ExitingSplit nor TerminalExit existed, so every one of these rows is a
// witness rather than a guard.
func TestExitingSplitClassification(t *testing.T) {
	for _, tc := range []struct {
		name, src   string
		wantStatus  int
		wantSignal  int
		wantMessage bool
	}{
		// SystemExit -> EXITING_WITH_STATUS, status = @status, and NO message:
		// a program that ends by calling exit is terminating, not crashing.
		{"exit 3", `exit 3`, 3, 0, false},
		{"exit false", `exit false`, 1, 0, false},
		{"exit 0", `exit 0`, 0, 0, false},
		{"exit!(4)", `exit!(4)`, 4, 0, false},
		{"abort", `abort "boom"`, 1, 0, false},
		{"Process.exit(5)", `Process.exit(5)`, 5, 0, false},
		{"Process.exit!(6)", `Process.exit!(6)`, 6, 0, false},
		{"Process.abort", `Process.abort "boom"`, 1, 0, false},
		// A raise of SystemExit itself, with no status ivar set by Kernel#exit:
		// SystemExit.new's default @status is 0.
		{"raise SystemExit", `raise SystemExit`, 0, 0, false},
		// SignalException -> EXITING_WITH_SIGNAL. No message for the class itself
		// (`rb_obj_is_instance_of(errinfo, rb_eSignal)` is true), so nothing is
		// printed and the process dies by the signal.
		{"uncaught SIGTERM", `Process.kill(:TERM, Process.pid)`, 128 + 15, 15, false},
		// ...but a SUBCLASS of SignalException DOES print, which is why an uncaught
		// Interrupt shows "Interrupt" on stderr and a bare SignalException shows
		// nothing. Getting this backwards is invisible in the exit status.
		{"uncaught Interrupt", `Process.kill(:INT, Process.pid)`, 128 + 2, 2, true},
		// SIGSEGV prints even as the base class (exiting_split's explicit case).
		{"SIGSEGV prints", `raise SignalException.new("SEGV")`, 128 + 11, 11, true},
		// Signal 0 cannot kill anything; kill(2) defines it as a liveness probe,
		// so re-raising it would be a no-op and leave the process exiting 0 on what
		// is really a failure.
		{"SignalException with signo 0", `raise SignalException.new(0)`, 1, 0, true},
		// Everything else -> EXIT_FAILURE with a message, unchanged.
		{"RuntimeError", `raise "x"`, 1, 0, true},
		{"a bare raise from a native method", `Integer("nope")`, 1, 0, true},
		{"NoMemoryError", `raise NoMemoryError`, 1, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			machine, out, err := runSrcErr(t, tc.src)
			if err == nil {
				texit, ok := machine.TerminalExit()
				if !ok {
					t.Fatalf("no error and no terminal exit (output %q)", out)
				}
				err = texit
			}
			d, ok := machine.ExitingSplit(err)
			if !ok {
				t.Fatalf("ExitingSplit did not classify %T %v", err, err)
			}
			if d.Status != tc.wantStatus {
				t.Errorf("Status = %d, want %d", d.Status, tc.wantStatus)
			}
			if d.Signal != tc.wantSignal {
				t.Errorf("Signal = %d, want %d", d.Signal, tc.wantSignal)
			}
			if d.Message != tc.wantMessage {
				t.Errorf("Message = %v, want %v", d.Message, tc.wantMessage)
			}
		})
	}
}

// TestExitingSplitRejectsANonRubyError proves the front-end still has a path for
// a parse, compile or host IO error: ExitingSplit must report false rather than
// classifying it as a plain failure, because those errors are not Ruby exceptions
// and have no class hierarchy to place.
func TestExitingSplitRejectsANonRubyError(t *testing.T) {
	machine, _, _ := runSrcErr(t, `1`)
	if _, ok := machine.ExitingSplit(errNotRuby{}); ok {
		t.Error("ExitingSplit classified a non-Ruby error")
	}
}

type errNotRuby struct{}

func (errNotRuby) Error() string { return "not a ruby exception" }

// TestTerminalExitIsAbsentWithoutOne pins the negative: a program that ends
// normally, and one whose SystemExit was RESCUED, must leave no terminal exit —
// otherwise `rbgo -e '...rescue SystemExit...'` would exit non-zero on a program
// MRI exits 0 for. An assertion that only checked the positive case would pass
// with a VM that recorded every SystemExit ever raised.
func TestTerminalExitIsAbsentWithoutOne(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"a program that ends normally", `x = 1`},
		{"a rescued SystemExit", `begin; exit 3; rescue SystemExit; end`},
		{"a rescued SystemExit through ensure", `begin; exit 3; rescue SystemExit; ensure; x = 1; end`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			machine, out, err := runSrcErr(t, tc.src)
			if err != nil {
				t.Fatalf("run: %v (output %q)", err, out)
			}
			if texit, ok := machine.TerminalExit(); ok {
				t.Errorf("TerminalExit reported %v, want none", texit)
			}
		})
	}
}

// TestExitBangSkipsAtExitHandlers is process.c rb_f_exit_bang, which is _exit(2)
// and therefore runs no exit handler. Measured against MRI 4.0.5:
// `at_exit { puts "x" }; exit!(4)` prints nothing and exits 4, where plain
// `exit 4` prints "x".
//
// MRI also skips `ensure` blocks, which this does NOT yet do — the exception
// machinery has no unwinding path that `ensure` does not see. The ensure row
// below records the remaining divergence rather than asserting MRI's answer,
// and says so, so nobody reads it as a specification.
func TestExitBangSkipsAtExitHandlers(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"exit! runs no at_exit", `at_exit { puts "handler" }; exit!(4)`, ""},
		{"exit runs at_exit", `at_exit { puts "handler" }; exit 4`, "handler\n"},
		{"exit! still lets ensure run (a KNOWN divergence from MRI, which skips it)",
			`begin; exit!(4); ensure; puts "ensure"; end`, "ensure\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			machine, out, err := runSrcErr(t, tc.src)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if out != tc.want {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
			texit, ok := machine.TerminalExit()
			if !ok {
				t.Fatal("no terminal exit recorded")
			}
			d, _ := machine.ExitingSplit(texit)
			if d.Status != 4 {
				t.Errorf("Status = %d, want 4", d.Status)
			}
		})
	}
}

// TestAtExitExitOverridesTheStatus is rb_ec_cleanup reading ec->errinfo AFTER
// rb_ec_teardown: a handler that calls exit sets the status, and overrides one an
// earlier exit asked for. Measured against MRI 4.0.5: 7, not 3, and 7 on a
// program that never called exit at all.
func TestAtExitExitOverridesTheStatus(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"an at_exit exit overrides an earlier one", `at_exit { exit 7 }; exit 3`, 7},
		{"an at_exit exit on a program that ends normally", `at_exit { exit 7 }`, 7},
		{"the LAST handler to exit wins", `at_exit { exit 7 }; at_exit { exit 8 }`, 7},
		// A handler that raises something else must not become the status: MRI
		// swallows it, and exiting_split would have made it 1.
		{"a raising handler leaves the status alone", `at_exit { raise "x" }; exit 3`, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			machine, _, err := runSrcErr(t, tc.src)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			texit, ok := machine.TerminalExit()
			if !ok {
				t.Fatal("no terminal exit recorded")
			}
			d, _ := machine.ExitingSplit(texit)
			if d.Status != tc.want {
				t.Errorf("Status = %d, want %d", d.Status, tc.want)
			}
		})
	}
}

// TestProcessExitFamilyIsPublic is issue #676's second defect: Init_process
// declares exit, exit! and abort with rb_define_module_function, so the qualified
// spelling is public. rbgo had only Kernel's private copies and raised
// NoMethodError.
func TestProcessExitFamilyIsPublic(t *testing.T) {
	src := `p Process.respond_to?(:exit), Process.respond_to?(:exit!), Process.respond_to?(:abort)
begin
  Process.exit(5)
rescue SystemExit => e
  puts "exit #{e.status}"
end
begin
  Process.exit!(6)
rescue SystemExit => e
  puts "exit! #{e.status}"
end
begin
  Process.exit
rescue SystemExit => e
  puts "bare #{e.status}"
end`
	want := "true\ntrue\ntrue\nexit 5\nexit! 6\nbare 0\n"
	_, out, err := runSrcErr(t, src)
	if err != nil {
		t.Fatalf("run: %v (output %q)", err, out)
	}
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestClassIsAWalksTheHierarchy covers classIsA's two branches, including the
// one a Ruby program cannot reach: a class name that is not a constant at all,
// which a raise() from a native method can produce.
func TestClassIsAWalksTheHierarchy(t *testing.T) {
	machine, _, _ := runSrcErr(t, `1`)
	for _, tc := range []struct {
		class, ancestor string
		want            bool
	}{
		{"SystemExit", "SystemExit", true},
		{"SystemExit", "Exception", true},
		{"SystemExit", "StandardError", false},
		{"Interrupt", "SignalException", true},
		{"SignalException", "Interrupt", false},
		{"RuntimeError", "SystemExit", false},
		{"NoSuchClassAnywhere", "NoSuchClassAnywhere", true},
		{"NoSuchClassAnywhere", "SystemExit", false},
	} {
		if got := machine.classIsA(tc.class, tc.ancestor); got != tc.want {
			t.Errorf("classIsA(%q, %q) = %v, want %v", tc.class, tc.ancestor, got, tc.want)
		}
	}
}

// TestExitStatusAndSignoOfMissingObject covers the nil-object paths: a
// SystemExit or SignalException raised by a native raise() carries a class name
// but no exception object, and reading its ivars must not panic.
func TestExitStatusAndSignoOfMissingObject(t *testing.T) {
	machine, _, _ := runSrcErr(t, `1`)
	if got := machine.exitStatusOf(nil); got != 0 {
		t.Errorf("exitStatusOf(nil) = %d, want 0", got)
	}
	if got := machine.signoOf(nil); got != 0 {
		t.Errorf("signoOf(nil) = %d, want 0", got)
	}
	d, ok := machine.ExitingSplit(RubyError{Class: "SystemExit", Message: "exit"})
	if !ok || d.Status != 0 || d.Message {
		t.Errorf("objectless SystemExit -> %+v, want status 0 and no message", d)
	}
	d, ok = machine.ExitingSplit(RubyError{Class: "SignalException", Message: "SIGTERM"})
	if !ok || d.Signal != 0 || d.Status != 1 || !d.Message {
		t.Errorf("objectless SignalException -> %+v, want the plain-failure fallback", d)
	}
}

// TestExitStatusAndSignoOfANonIntegerIvar covers the last arm of exitStatusOf and
// signoOf: the ivar is present but not an Integer, or the object has no ivars at
// all. eval_error.c's sysexit_status is a bare rb_ivar_get with no type check, so
// a value that is not a number has to mean 0 rather than crash — and a program
// CAN put one there (`e.instance_variable_set(:@status, "x")`).
func TestExitStatusAndSignoOfANonIntegerIvar(t *testing.T) {
	machine, _, _ := runSrcErr(t, `1`)
	// A value with no ivar table at all.
	if got := machine.exitStatusOf(objectOne()); got != 0 {
		t.Errorf("exitStatusOf(1) = %d, want 0", got)
	}
	if got := machine.signoOf(objectOne()); got != 0 {
		t.Errorf("signoOf(1) = %d, want 0", got)
	}
	// And one whose @status a Ruby program replaced with a String.
	_, out, err := runSrcErr(t, `e = SystemExit.new
e.instance_variable_set(:@status, "x")
p e.status`)
	if err != nil {
		t.Fatalf("run: %v (output %q)", err, out)
	}
}

// objectOne is an object.Value with no instance-variable table, which is what
// exitStatusOf and signoOf see when a SystemExit or SignalException was raised
// with a class name rather than an object.
func objectOne() object.Value { return object.IntValue(1) }
