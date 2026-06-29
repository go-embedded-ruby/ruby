// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !(js && wasm)

package vm

import (
	"os/exec"
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
