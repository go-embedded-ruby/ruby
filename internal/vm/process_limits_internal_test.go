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

// TestProcessRlimitInfinityRoundTrip pins the LINUX representation of
// RLIM_INFINITY on every platform. rlim_t is unsigned and 64 bits wide, so on
// Linux RLIM_INFINITY is ~0, which exceeds LONG_MAX and therefore crosses into
// Ruby as a BIGNUM — process.c's own example is
//
//	Process.getrlimit(:CORE) # => [0, 18446744073709551615]
//
// Darwin's RLIM_INFINITY is 0x7fff_ffff_ffff_ffff, which fits an int64, so no
// test that only runs there can see this: the seam supplies the Linux value
// instead. What must hold on both is that the pair comes back as two Integers
// and that setrlimit takes the very same 64 bits back — which is exactly what
// Process.setrlimit(:CORE, *Process.getrlimit(:CORE)) asserts, and what failed
// on the two Linux arch lanes with "can't convert Object to Integer".
func TestProcessRlimitInfinityRoundTrip(t *testing.T) {
	var applied [][2]uint64
	origSet := procSetrlimit
	procSetrlimit = func(_ int, cur, max uint64) error {
		applied = append(applied, [2]uint64{cur, max})
		return nil
	}
	defer func() { procSetrlimit = origSet }()

	withGetrlimit(t, func(int) (uint64, uint64, error) { return 0, ^uint64(0), nil }, func() {
		// Both halves are Integers, and the big one prints in full rather than
		// wrapping negative.
		if got := eval(t, `r = Process.getrlimit(:CORE)
p r
p r.map { |v| v.is_a?(Integer) }`); got != "[0, 18446744073709551615]\n[true, true]\n" {
			t.Errorf("getrlimit with the Linux RLIM_INFINITY: got %q", got)
		}
		// The round trip is a no-op: every bit that came out goes back in.
		if got := eval(t, `p Process.setrlimit(:CORE, *Process.getrlimit(:CORE))`); got != "nil\n" {
			t.Errorf("round trip: got %q", got)
		}
		if len(applied) != 1 || applied[0] != [2]uint64{0, ^uint64(0)} {
			t.Errorf("round trip applied %v want [[0 %d]]", applied, uint64(1<<64-1))
		}
	})
}

// TestProcessRlimitRangeErrors: an integer too wide even for rlim_t is a
// RangeError, and so is one too wide for the int a resource number or a pid is.
// MRI 4.0.5 names the C type in both messages.
func TestProcessRlimitRangeErrors(t *testing.T) {
	for _, tc := range []struct{ src, msg string }{
		{`Process.setrlimit(:CORE, 2 ** 70, 0)`, "bignum too big to convert into 'unsigned long long'"},
		{`Process.getrlimit(2 ** 70)`, "bignum too big to convert into 'long'"},
		{`Process.kill(0, 2 ** 70)`, "bignum too big to convert into 'long'"},
	} {
		class, msg := evalErr(t, tc.src)
		if class != "RangeError" || msg != tc.msg {
			t.Errorf("%s: got %s/%q want RangeError/%q", tc.src, class, msg, tc.msg)
		}
	}
	// A negative limit wraps rather than raising, as NUM2ULL's cast does: MRI
	// hands setrlimit(:CORE, -1, -1) to the kernel, which answers EPERM.
	var applied [][2]uint64
	orig := procSetrlimit
	procSetrlimit = func(_ int, cur, max uint64) error {
		applied = append(applied, [2]uint64{cur, max})
		return nil
	}
	defer func() { procSetrlimit = orig }()
	eval(t, `Process.setrlimit(:CORE, -1, -1)`)
	if len(applied) != 1 || applied[0] != [2]uint64{^uint64(0), ^uint64(0)} {
		t.Errorf("negative limit: got %v want it wrapped to ~0", applied)
	}
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

// TestProcessResiduals covers Process::Tms and Process.times, clock_getres,
// argv0, setproctitle, warmup, maxgroups= and the Process::UID / GID / Sys
// identity modules.
func TestProcessResiduals(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		// Process::Tms: four readers and four writers, with and without arguments.
		{`t = Process::Tms.new(1, 2, 3, 4); p [t.utime, t.stime, t.cutime, t.cstime]`, "[1, 2, 3, 4]\n"},
		{`t = Process::Tms.new; p [t.utime, t.stime, t.cutime, t.cstime]`, "[nil, nil, nil, nil]\n"},
		{`t = Process::Tms.new
t.utime, t.stime, t.cutime, t.cstime = 1, 2, 3, 4
p [t.utime, t.stime, t.cutime, t.cstime]`, "[1, 2, 3, 4]\n"},
		{`p Process::Tms.name`, "\"Process::Tms\"\n"},
		// Process.times reports this process's own CPU time, with no child time.
		{`t = Process.times
p [t.is_a?(Process::Tms), t.utime.is_a?(Float), t.utime >= 0.0, t.cutime, t.cstime]`,
			"[true, true, true, 0.0, 0.0]\n"},
		{`u = Process.times.utime
1 until Process.times.utime > u
p Process.times.utime > u`, "true\n"},
		// clock_getres: the documented symbolic clocks and the numeric ones.
		{`p Process.clock_getres(:GETTIMEOFDAY_BASED_CLOCK_REALTIME, :nanosecond)`, "1000\n"},
		{`p Process.clock_getres(:TIME_BASED_CLOCK_REALTIME, :nanosecond)`, "1000000000\n"},
		{`p Process.clock_getres(:GETRUSAGE_BASED_CLOCK_PROCESS_CPUTIME_ID, :nanosecond)`, "1000\n"},
		{`p Process.clock_getres(Process::CLOCK_REALTIME, :nanosecond)`, "1000\n"},
		// 1e-06 second, the same value MRI reports; the spelling differs only
		// because this VM's Float#inspect drops the ".0" before the exponent
		// (`p 0.000001` prints 1e-06 here and 1.0e-06 in MRI) — a core
		// Float-formatting divergence, not a clock one.
		{`p Process.clock_getres(Process::CLOCK_MONOTONIC)`, "1e-06\n"},
		// argv0 is frozen and is the same object every time.
		{`p [Process.argv0.is_a?(String), Process.argv0.frozen?, Process.argv0.equal?(Process.argv0)]`,
			"[true, true, true]\n"},
		{`p Process.setproctitle("a-title")`, "\"a-title\"\n"},
		{`p Process.warmup`, "true\n"},
		// maxgroups is a tunable that remembers what was assigned.
		{`n = Process.maxgroups
Process.maxgroups = n - 1
p Process.maxgroups == n - 1
Process.maxgroups = n
p Process.maxgroups == n`, "true\ntrue\n"},
		// The identity modules answer with the same numbers Process does.
		{`p [Process::UID.rid == Process.uid, Process::UID.eid == Process.euid]`, "[true, true]\n"},
		{`p [Process::GID.rid == Process.gid, Process::GID.eid == Process.egid]`, "[true, true]\n"},
		{`p [Process::Sys.getuid == Process.uid, Process::Sys.geteuid == Process.euid,
    Process::Sys.getgid == Process.gid, Process::Sys.getegid == Process.egid]`,
			"[true, true, true, true]\n"},
		{`p [Process::UID.name, Process::Sys.name]`, "[\"Process::UID\", \"Process::Sys\"]\n"},
	} {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("%s\n got %q want %q", tc.src, got, tc.want)
		}
	}
	for _, tc := range []struct{ src, class, msg string }{
		{`Process.clock_getres`, "ArgumentError", "wrong number of arguments (given 0, expected 1..2)"},
		{`Process.setproctitle`, "ArgumentError", "wrong number of arguments (given 0, expected 1)"},
	} {
		class, msg := evalErr(t, tc.src)
		if class != tc.class || msg != tc.msg {
			t.Errorf("%s: got %s/%q want %s/%q", tc.src, class, msg, tc.class, tc.msg)
		}
	}
}

// TestMergeEnv covers every branch of the environment merge: a name the base
// carries and the Hash overrides, one the Hash removes, one the Hash adds, and
// one the base carries untouched.
func TestMergeEnv(t *testing.T) {
	got := mergeEnv([]string{"KEEP=1", "OVER=old", "DROP=1"},
		map[string]string{"OVER": "new", "ADD": "2"}, []string{"DROP"})
	want := []string{"KEEP=1", "OVER=new", "ADD=2"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("mergeEnv: got %v want %v", got, want)
	}
	if got := mergeEnv(nil, map[string]string{"B": "2", "A": "1"}, nil); strings.Join(got, " ") != "A=1 B=2" {
		t.Errorf("mergeEnv(nil base): got %v want deterministic A=1 B=2", got)
	}
}

// TestKernelSystemEnvHash keeps Kernel#system's own (older, combined-capture)
// argument peeling exercised: a leading environment Hash and a trailing options
// Hash are both stripped off the command.
func TestKernelSystemEnvHash(t *testing.T) {
	var got []string
	fake := func(cmd []string) (string, int, bool) {
		got = append(got, strings.Join(cmd, " "))
		return "", 0, true
	}
	orig := systemCommand
	systemCommand = fake
	defer func() { systemCommand = orig }()
	eval(t, `system({"A" => "1"}, "/bin/echo", "hi", {:exception => false})`)
	if len(got) != 1 || got[0] != "/bin/echo hi" {
		t.Errorf("system with an env Hash: got %v", got)
	}
}

// TestProcessRlimitSymbolicMatchesTable: on THIS host, the symbolic limit names
// and the numbers getrlimit reports are the same values — the check the faked
// Linux round trip above cannot make, because it replaces only one of the two.
func TestProcessRlimitSymbolicMatchesTable(t *testing.T) {
	var applied [][2]uint64
	orig := procSetrlimit
	procSetrlimit = func(_ int, cur, max uint64) error {
		applied = append(applied, [2]uint64{cur, max})
		return nil
	}
	defer func() { procSetrlimit = orig }()
	eval(t, `Process.setrlimit(:CORE, 0, :INFINITY)
Process.setrlimit(:CORE, 0, Process::RLIM_INFINITY)`)
	if len(applied) != 2 || applied[0] != applied[1] {
		t.Errorf(":INFINITY and Process::RLIM_INFINITY disagree: %v", applied)
	}
	if applied[0][1] != rlimitValues["INFINITY"] {
		t.Errorf("symbolic INFINITY = %d want the platform's %d", applied[0][1], rlimitValues["INFINITY"])
	}
}
