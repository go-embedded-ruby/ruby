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
