// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

import (
	"os/exec"
)

// runCaptured runs cmd on Windows, returning its combined output and exit code. A
// one-string command with metacharacters/whitespace goes through cmd.exe; an
// explicit argv runs directly. See spawn_native.go for the Unix variant.
var runCaptured = func(cmd []string) (string, int) {
	if len(cmd) == 0 {
		return "", 127
	}
	var c *exec.Cmd
	if s, sh := shellish(cmd); sh {
		c = exec.Command("cmd", "/c", s)
	} else {
		c = exec.Command(cmd[0], cmd[1:]...)
	}
	out, err := c.CombinedOutput()
	return string(out), exitCodeOf(err)
}

// systemCommand runs cmd for Kernel#system on Windows (see spawn_native.go for
// the semantics and why it is a package var).
var systemCommand = func(cmd []string) (out string, status int, spawned bool) {
	if len(cmd) == 0 {
		return "", 127, false
	}
	var c *exec.Cmd
	if s, sh := shellish(cmd); sh {
		c = exec.Command("cmd", "/c", s)
	} else {
		c = exec.Command(cmd[0], cmd[1:]...)
	}
	b, err := c.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return "", 127, false
		}
	}
	return string(b), exitCodeOf(err), true
}

// exitCodeOf extracts a process exit code from exec's error (see spawn_native.go).
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
// Non-POSIX process seams
//
// This target has no getrlimit/setrlimit, no process groups, no sessions and no
// kill(2), so the shared process.go defines none of the methods that need them
// (procPosix is false) and the seams below exist only to satisfy the compiler.
// MRI compiles the same methods out here, which is why ruby/spec expects
// Process.respond_to?(:getrlimit) to be false on Windows.
//
// Reference: ruby/ruby v3_4_0 process.c — the HAVE_SETRLIMIT / HAVE_SETPGID /
// HAVE_GETPRIORITY guards around proc_setrlimit, proc_setpgid and friends,
// each falling back to rb_f_notimplement.

const procPosix = false

var (
	rlimitResources = map[string]int{}
	rlimitValues    = map[string]uint64{}
	prioTargets     = map[string]int{}

	procGetrlimit   func(res int) (cur, max uint64, err error)
	procSetrlimit   func(res int, cur, max uint64) error
	procGetpriority func(which, who int) (int, error)
	procSetpriority func(which, who, prio int) error
	procGetpgid     func(pid int) (int, error)
	procSetpgid     func(pid, pgid int) error
	procGetsid      func(pid int) (int, error)
	procKill        func(pid, sig int) error
	procRusage      func() (utime, stime float64)
)
