// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !wasm

package vm

import (
	"errors"
	"strings"
	"syscall"
	"testing"
)

// withGetrlimit swaps the getrlimit seam for the duration of fn, so the shared
// coercion and error-shaping paths can be driven without a kernel that fails on
// demand (and without a host whose real limits vary).
func withGetrlimit(t *testing.T, fake func(int) (uint64, uint64, error), fn func()) {
	t.Helper()
	orig := procGetrlimit
	procGetrlimit = fake
	defer func() { procGetrlimit = orig }()
	fn()
}

// TestProcessRlimitCoercion covers every way process.c's rlimit_resource_type
// and rlimit_resource_value accept (or reject) a designator: the constant, the
// Symbol and String short names, #to_str, #to_int, and each error.
func TestProcessRlimitCoercion(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		// Integer / Symbol / String resource, and the symbolic limit values.
		{`p Process.getrlimit(Process::RLIMIT_CORE) == Process.getrlimit(:CORE)`, "true\n"},
		{`p Process.getrlimit("CORE") == Process.getrlimit(:CORE)`, "true\n"},
		{`p Process.setrlimit(:CORE, *Process.getrlimit(:CORE))`, "nil\n"},
		// #to_str wins over #to_int (rb_check_string_type runs first).
		{`o = Object.new
def o.to_str; "CORE"; end
def o.to_int; raise "not reached"; end
p Process.getrlimit(o) == Process.getrlimit(:CORE)`, "true\n"},
		// #to_str that does not return a String falls through to #to_int.
		{`o = Object.new
def o.to_str; nil; end
def o.to_int; Process::RLIMIT_CORE; end
p Process.getrlimit(o) == Process.getrlimit(:CORE)`, "true\n"},
	} {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("%s\n got %q want %q", tc.src, got, tc.want)
		}
	}

	for _, tc := range []struct{ src, class, msg string }{
		{`Process.getrlimit(:FOO)`, "ArgumentError", "invalid resource name: FOO"},
		{`Process.getrlimit("core")`, "ArgumentError", "invalid resource name: core"},
		{`Process.getrlimit(nil)`, "TypeError", "no implicit conversion from nil to integer"},
		{`Process.getrlimit(Object.new)`, "TypeError", "no implicit conversion of Object into Integer"},
		{`o = Object.new; def o.to_int; nil; end; Process.getrlimit(o)`,
			"TypeError", "can't convert Object to Integer (Object#to_int gives nil)"},
		{`Process.getrlimit`, "ArgumentError", "wrong number of arguments (given 0, expected 1)"},
		{`Process.setrlimit(:CORE)`, "ArgumentError", "wrong number of arguments (given 1, expected 2..3)"},
		{`Process.setrlimit(:CORE, 0, 0, 0)`, "ArgumentError", "wrong number of arguments (given 4, expected 2..3)"},
		{`Process.setrlimit(:CORE, :NOPE, 0)`, "ArgumentError", "invalid resource value: NOPE"},
		{`Process.getrlimit(-7)`, "Errno::EINVAL", "Invalid argument - getrlimit"},
		{`Process.setrlimit(-7, 0, 0)`, "Errno::EINVAL", "Invalid argument - setrlimit"},
	} {
		class, msg := evalErr(t, tc.src)
		if class != tc.class || msg != tc.msg {
			t.Errorf("%s: got %s/%q want %s/%q", tc.src, class, msg, tc.class, tc.msg)
		}
	}

	// The remaining setrlimit forms — an omitted or nil hard limit (which
	// repeats the soft one), a symbolic limit and a #to_int limit — go through
	// the seam rather than the kernel: applying them for real would lower this
	// process's own hard limit, which no later call could raise back, and every
	// test after it would fail with EPERM.
	var applied [][2]uint64
	origSet := procSetrlimit
	procSetrlimit = func(_ int, cur, max uint64) error {
		applied = append(applied, [2]uint64{cur, max})
		return nil
	}
	defer func() { procSetrlimit = origSet }()
	for _, tc := range []struct {
		src  string
		want [2]uint64
	}{
		{`Process.setrlimit(:CORE, 7)`, [2]uint64{7, 7}},
		{`Process.setrlimit(:CORE, 7, nil)`, [2]uint64{7, 7}},
		{`Process.setrlimit(:CORE, 7, 9)`, [2]uint64{7, 9}},
		{`Process.setrlimit(:CORE, :SAVED_CUR, "INFINITY")`,
			[2]uint64{rlimitValues["SAVED_CUR"], rlimitValues["INFINITY"]}},
		{`o = Object.new; def o.to_int; 5; end; Process.setrlimit(:CORE, o, 5)`, [2]uint64{5, 5}},
	} {
		applied = nil
		eval(t, tc.src)
		if len(applied) != 1 || applied[0] != tc.want {
			t.Errorf("%s: setrlimit got %v want [%v]", tc.src, applied, tc.want)
		}
	}
}

// TestProcessRlimitInfinityPromotes drives the seam with a limit that does not
// fit in an int64 — RLIM_INFINITY is ~0 on Linux — and asserts it comes back as
// a Bignum rather than wrapping negative.
func TestProcessRlimitInfinityPromotes(t *testing.T) {
	withGetrlimit(t, func(int) (uint64, uint64, error) { return 1, ^uint64(0), nil }, func() {
		if got := eval(t, `p Process.getrlimit(:CORE)`); got != "[1, 18446744073709551615]\n" {
			t.Errorf("getrlimit with an out-of-int64 limit: got %q", got)
		}
	})
}

// TestProcessSysFailFallback: an errno with no registered Errno::Exxx class
// still surfaces as a rescuable SystemCallError rather than a Go panic.
func TestProcessSysFailFallback(t *testing.T) {
	withGetrlimit(t, func(int) (uint64, uint64, error) { return 0, 0, errors.New("weird failure") }, func() {
		class, msg := evalErr(t, `Process.getrlimit(:CORE)`)
		if class != "SystemCallError" || msg != "Weird failure - getrlimit" {
			t.Errorf("unregistered errno: got %s/%q", class, msg)
		}
	})
	// An errno that IS a syscall.Errno but is not one of the registered names
	// takes the same fallback (the errno lookup misses).
	withGetrlimit(t, func(int) (uint64, uint64, error) { return 0, 0, syscall.Errno(0) }, func() {
		if class, _ := evalErr(t, `Process.getrlimit(:CORE)`); class != "SystemCallError" {
			t.Errorf("errno 0: got class %s want SystemCallError", class)
		}
	})
}

// TestProcessGroupsAndSessions covers the process-group and session accessors,
// their arities, and their failure path.
func TestProcessGroupsAndSessions(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`p Process.getpgrp == Process.getpgid(0)`, "true\n"},
		{`p Process.getsid == Process.getsid(0)`, "true\n"},
		{`p Process.getsid(nil).is_a?(Integer)`, "true\n"},
		{`p Process.setpgid(0, Process.getpgrp)`, "0\n"},
		{`p Process.setpgrp`, "0\n"},
		{`p Process.getpriority(Process::PRIO_PROCESS, 0).is_a?(Integer)`, "true\n"},
		{`p Process.getpriority(Process::PRIO_USER, Process.uid).is_a?(Integer)`, "true\n"},
		{`p Process.setpriority(Process::PRIO_PROCESS, 0, Process.getpriority(Process::PRIO_PROCESS, 0))`, "0\n"},
	} {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.src, got, tc.want)
		}
	}
	for _, tc := range []struct{ src, class, msg string }{
		{`Process.getpgid`, "ArgumentError", "wrong number of arguments (given 0, expected 1)"},
		{`Process.setpgid(0)`, "ArgumentError", "wrong number of arguments (given 1, expected 2)"},
		{`Process.getpriority(0)`, "ArgumentError", "wrong number of arguments (given 1, expected 2)"},
		{`Process.setpriority(0, 0)`, "ArgumentError", "wrong number of arguments (given 2, expected 3)"},
		{`Process.getpgid(2 ** 30)`, "Errno::ESRCH", "No such process"},
		{`Process.getsid(2 ** 30)`, "Errno::ESRCH", "No such process"},
		{`Process.setpgid(2 ** 30, 0)`, "Errno::ESRCH", "No such process"},
		{`Process.getpriority(99, 0)`, "Errno::EINVAL", "Invalid argument"},
		{`Process.setpriority(99, 0, 0)`, "Errno::EINVAL", "Invalid argument"},
	} {
		class, msg := evalErr(t, tc.src)
		if class != tc.class || msg != tc.msg {
			t.Errorf("%s: got %s/%q want %s/%q", tc.src, class, msg, tc.class, tc.msg)
		}
	}
}

// TestProcessSetpgrpFails drives setpgrp's failure branch, which a healthy
// kernel never takes for the calling process.
func TestProcessSetpgrpFails(t *testing.T) {
	orig := procSetpgid
	procSetpgid = func(int, int) error { return syscall.EPERM }
	defer func() { procSetpgid = orig }()
	if class, msg := evalErr(t, `Process.setpgrp`); class != "Errno::EPERM" || msg != "Operation not permitted" {
		t.Errorf("setpgrp failure: got %s/%q", class, msg)
	}
	if class, _ := evalErr(t, `Process.setpgid(0, 0)`); class != "Errno::EPERM" {
		t.Errorf("setpgid failure: got class %s", class)
	}
}

// TestProcessGetpgrpFails drives getpgrp's failure branch the same way.
func TestProcessGetpgrpFails(t *testing.T) {
	orig := procGetpgid
	procGetpgid = func(int) (int, error) { return -1, syscall.EPERM }
	defer func() { procGetpgid = orig }()
	if class, _ := evalErr(t, `Process.getpgrp`); class != "Errno::EPERM" {
		t.Errorf("getpgrp failure: got class %s", class)
	}
}

// TestGetpriorityAmbiguousMinusOne pins the rule that resolves getpriority(2)'s
// ambiguous -1 return (see getpriorityFailed): a documented errno is a real
// failure, anything else is a nice value of -1.
func TestGetpriorityAmbiguousMinusOne(t *testing.T) {
	orig := procGetpriority
	defer func() { procGetpriority = orig }()

	// A stale, undocumented errno alongside -1 is not a failure.
	procGetpriority = func(int, int) (int, error) { return -1, syscall.ETIMEDOUT }
	if got := eval(t, `p Process.getpriority(Process::PRIO_USER, 0)`); got != "-1\n" {
		t.Errorf("stale errno with -1: got %q want \"-1\\n\"", got)
	}
	// A non-errno error alongside -1 likewise.
	procGetpriority = func(int, int) (int, error) { return -1, errors.New("not an errno") }
	if got := eval(t, `p Process.getpriority(Process::PRIO_USER, 0)`); got != "-1\n" {
		t.Errorf("non-errno with -1: got %q", got)
	}
	// A documented errno alongside -1 IS a failure.
	procGetpriority = func(int, int) (int, error) { return -1, syscall.ESRCH }
	if class, _ := evalErr(t, `Process.getpriority(Process::PRIO_USER, 0)`); class != "Errno::ESRCH" {
		t.Errorf("ESRCH with -1: got class %s", class)
	}
	// Any other return value with an error is a failure whatever the errno.
	procGetpriority = func(int, int) (int, error) { return -2, syscall.ETIMEDOUT }
	if class, _ := evalErr(t, `Process.getpriority(Process::PRIO_USER, 0)`); class != "Errno::ETIMEDOUT" {
		t.Errorf("errno with -2: got class %s", class)
	}
}

// TestProcessKill covers Process.kill's signal designators, its pid coercion,
// the process-group form, and every argument error. Only signal 0 is ever
// really delivered; the process-group and failure paths go through the seam so
// the test never signals anything.
func TestProcessKill(t *testing.T) {
	if got := eval(t, `p Process.kill(0, Process.pid)`); got != "1\n" {
		t.Errorf("kill(0, self): got %q", got)
	}
	if got := eval(t, `p Process.kill(0, Process.pid, Process.pid)`); got != "2\n" {
		t.Errorf("kill(0, self, self): got %q", got)
	}

	var calls [][2]int
	orig := procKill
	procKill = func(pid, sig int) error {
		calls = append(calls, [2]int{pid, sig})
		return nil
	}
	defer func() { procKill = orig }()

	for _, tc := range []struct {
		src  string
		want [2]int
	}{
		{`Process.kill(:SIGTERM, 4242)`, [2]int{4242, 15}},
		{`Process.kill("SIGTERM", 4242)`, [2]int{4242, 15}},
		{`Process.kill("TERM", 4242)`, [2]int{4242, 15}},
		{`Process.kill(15, 4242)`, [2]int{4242, 15}},
		{`o = Object.new; def o.to_str; "TERM"; end; Process.kill(o, 4242)`, [2]int{4242, 15}},
		{`o = Object.new; def o.to_int; 4242; end; Process.kill("TERM", o)`, [2]int{4242, 15}},
		// A leading '-' signals the process group: killpg(pgid, sig).
		{`Process.kill("-TERM", 4242)`, [2]int{-4242, 15}},
		{`Process.kill(:"-SIGTERM", 4242)`, [2]int{-4242, 15}},
	} {
		calls = nil
		eval(t, tc.src)
		if len(calls) != 1 || calls[0] != tc.want {
			t.Errorf("%s: kill called with %v want [%v]", tc.src, calls, tc.want)
		}
	}

	procKill = func(int, int) error { return syscall.ESRCH }
	if class, msg := evalErr(t, `Process.kill("TERM", 4242)`); class != "Errno::ESRCH" || msg != "No such process" {
		t.Errorf("kill failure: got %s/%q", class, msg)
	}

	for _, tc := range []struct{ src, class, msg string }{
		{`Process.kill("TERM")`, "ArgumentError", "wrong number of arguments (given 1, expected 2+)"},
		{`Process.kill("FOO", 0)`, "ArgumentError", "unsupported signal 'SIGFOO'"},
		{`Process.kill("SIGFOO", 0)`, "ArgumentError", "unsupported signal 'SIGFOO'"},
		{`Process.kill("term", 0)`, "ArgumentError", "unsupported signal 'SIGterm'"},
		{`Process.kill("SIG", 0)`, "ArgumentError", "unsupported signal 'SIG'"},
		{`Process.kill("TER\0M", 0)`, "ArgumentError", "signal name with null byte"},
		{`Process.kill(Object.new, 0)`, "ArgumentError", "bad signal type Object"},
		{`Process.kill("TERM", nil)`, "TypeError", "no implicit conversion from nil to integer"},
	} {
		class, msg := evalErr(t, tc.src)
		if class != tc.class || msg != tc.msg {
			t.Errorf("%s: got %s/%q want %s/%q", tc.src, class, msg, tc.class, tc.msg)
		}
	}
}

// TestProcessLimitConstants: every RLIMIT_/RLIM_/PRIO_ name the platform table
// carries is reachable both as Process::NAME and through const_get, and the
// short name round-trips through getrlimit.
func TestProcessLimitConstants(t *testing.T) {
	var b strings.Builder
	for name := range rlimitResources {
		b.WriteString("p Process.const_get(:RLIMIT_" + name + ").is_a?(Integer)\n")
		b.WriteString("p Process.getrlimit(:" + name + ") == Process.getrlimit(Process::RLIMIT_" + name + ")\n")
	}
	for name := range rlimitValues {
		b.WriteString("p Process::RLIM_" + name + ".is_a?(Integer)\n")
	}
	for name := range prioTargets {
		b.WriteString("p Process::PRIO_" + name + ".is_a?(Integer)\n")
	}
	got := eval(t, b.String())
	want := strings.Repeat("true\n", 2*len(rlimitResources)+len(rlimitValues)+len(prioTargets))
	if got != want {
		t.Errorf("constants: got %q want %q", got, want)
	}
}
