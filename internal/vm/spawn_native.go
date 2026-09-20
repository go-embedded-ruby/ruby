// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !wasm

package vm

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// runCaptured runs cmd, returning its combined stdout+stderr and exit code. A
// one-string command with shell metacharacters/whitespace goes through /bin/sh
// (MRI semantics for a single string); an explicit argv runs directly. The
// command runner is a seam so tests can drive every branch without spawning real
// processes.
var runCaptured = func(cmd []string) (string, int) {
	if len(cmd) == 0 {
		return "", 127
	}
	var c *exec.Cmd
	if s, sh := shellish(cmd); sh {
		c = exec.Command("/bin/sh", "-c", s)
	} else {
		c = exec.Command(cmd[0], cmd[1:]...)
	}
	out, err := c.CombinedOutput()
	return string(out), exitCodeOf(err)
}

// systemCommand runs cmd for Kernel#system: it returns the combined output, the
// exit status, and whether the command was actually spawned (false when the
// process could not be started at all, e.g. a bare argv naming a missing binary —
// MRI's Kernel#system returns nil in that case). Same shell-vs-argv rule as
// runCaptured. A package var so tests drive every branch without real processes.
var systemCommand = func(cmd []string) (out string, status int, spawned bool) {
	if len(cmd) == 0 {
		return "", 127, false
	}
	var c *exec.Cmd
	if s, sh := shellish(cmd); sh {
		c = exec.Command("/bin/sh", "-c", s)
	} else {
		c = exec.Command(cmd[0], cmd[1:]...)
	}
	b, err := c.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			// Not an ExitError: the process never ran (binary missing, etc.).
			return "", 127, false
		}
	}
	return string(b), exitCodeOf(err), true
}

// exitCodeOf extracts a process exit code from exec's error: 0 on success, the
// real status from an ExitError, and 127 (command not found, as a shell reports)
// otherwise.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 127
}

// ---------------------------------------------------------------------------
// POSIX process seams
//
// Everything below is the thin, platform-bound half of the Process module: the
// resource-limit table this host's kernel uses, and one-line wrappers over the
// syscalls. All of the argument peeling, coercion and error shaping lives in the
// shared process.go, so the build-tagged variants (spawn_windows.go /
// spawn_wasm.go) stay empty and the POSIX lanes can cover every branch.
//
// Reference: ruby/ruby v3_4_0 process.c — rlimit_resource_name2int (the RLIMIT_
// name table), proc_getrlimit / proc_setrlimit, proc_getpgid / proc_setpgid /
// proc_getsid, proc_getpriority / proc_setpriority; signal.c rb_f_kill.

// procPosix reports that this build has the POSIX process surface (resource
// limits, process groups, sessions, priorities, kill). Process defines those
// methods only when it is true — on Windows MRI compiles them out entirely, and
// the ruby/spec expectation there is Process.respond_to?(:getrlimit) == false.
const procPosix = true

// rlimitResources is the host's RLIMIT_<name> table, keyed by the bare name
// process.c's rlimit_resource_name2int accepts. The numbers differ per OS
// (RLIMIT_NPROC is 6 on Linux and 7 on Darwin), so they come from the kernel
// headers via x/sys/unix rather than being written out here.
var rlimitResources = map[string]int{
	"AS":      unix.RLIMIT_AS,
	"CORE":    unix.RLIMIT_CORE,
	"CPU":     unix.RLIMIT_CPU,
	"DATA":    unix.RLIMIT_DATA,
	"FSIZE":   unix.RLIMIT_FSIZE,
	"MEMLOCK": unix.RLIMIT_MEMLOCK,
	"NOFILE":  unix.RLIMIT_NOFILE,
	"NPROC":   unix.RLIMIT_NPROC,
	"RSS":     unix.RLIMIT_RSS,
	"STACK":   unix.RLIMIT_STACK,
}

// rlimitValues is the symbolic limit table process.c's rlimit_resource_value
// consults (:INFINITY / :SAVED_MAX / :SAVED_CUR). POSIX only guarantees
// RLIM_INFINITY; the two saved-limit names are aliases of it on the platforms
// MRI ships them on, which is what Process::RLIM_SAVED_MAX reports on Darwin.
var rlimitValues = map[string]uint64{
	"INFINITY":  uint64(unix.RLIM_INFINITY),
	"SAVED_MAX": uint64(unix.RLIM_INFINITY),
	"SAVED_CUR": uint64(unix.RLIM_INFINITY),
}

// prioTargets is the PRIO_<name> table for getpriority(2)/setpriority(2).
var prioTargets = map[string]int{
	"PROCESS": unix.PRIO_PROCESS,
	"PGRP":    unix.PRIO_PGRP,
	"USER":    unix.PRIO_USER,
}

// The seams themselves. Each is a package var so a whitebox test can drive the
// failure branch of the shared code without needing a kernel that fails.
var (
	procGetrlimit = func(res int) (cur, max uint64, err error) {
		var rl unix.Rlimit
		if err := unix.Getrlimit(res, &rl); err != nil {
			return 0, 0, err
		}
		return uint64(rl.Cur), uint64(rl.Max), nil
	}
	procSetrlimit = func(res int, cur, max uint64) error {
		rl := unix.Rlimit{Cur: cur, Max: max}
		return unix.Setrlimit(res, &rl)
	}
	procGetpriority = unix.Getpriority
	procSetpriority = unix.Setpriority
	procGetpgid     = unix.Getpgid
	procSetpgid     = unix.Setpgid
	procGetsid      = unix.Getsid
	// procKill takes a plain int signal number so the shared caller never has to
	// name syscall.Signal (which is spelt differently on the non-POSIX targets).
	procKill = func(pid, sig int) error { return unix.Kill(pid, syscall.Signal(sig)) }
	// procRusage reports this process's accumulated user and system CPU time in
	// seconds, the two fields Process.times fills that Go's runtime does not
	// expose (getrusage(RUSAGE_SELF)).
	procRusage = func() (utime, stime float64) {
		var ru unix.Rusage
		if err := unix.Getrusage(unix.RUSAGE_SELF, &ru); err != nil {
			return 0, 0
		}
		return timevalSeconds(ru.Utime), timevalSeconds(ru.Stime)
	}
)

// timevalSeconds converts a struct timeval to fractional seconds. The field
// widths differ per architecture (int32 on 32-bit Linux, int64 elsewhere), so
// the conversion goes through int64 rather than naming a concrete type.
func timevalSeconds(tv unix.Timeval) float64 {
	return float64(tv.Sec) + float64(tv.Usec)/1e6
}

// runSpawnProc runs one prepared child to completion, writing its two output
// streams to the request's sinks and returning its exit code. It is the richer
// counterpart of runCaptured: Process.spawn needs the child's environment, its
// working directory and its two streams kept apart, none of which a combined
// capture can express. A package var so tests drive every caller without real
// processes.
var runSpawnProc = func(r *spawnReq) int {
	var c *exec.Cmd
	if r.shell != "" {
		c = exec.Command("/bin/sh", "-c", r.shell)
	} else {
		// The program was already resolved against the CHILD's PATH; os/exec would
		// otherwise look it up in the parent's, which is a different search.
		c = exec.Command(r.path, r.argv[1:]...)
		// The [prog, argv0] command-array form tells the child a name other than
		// the file that was executed, so argv[0] is set apart from the path.
		c.Args[0] = r.argv[0]
	}
	c.Dir, c.Env, c.Stdout, c.Stderr = r.dir, r.env, r.stdout, r.stderr
	return exitCodeOf(c.Run())
}
