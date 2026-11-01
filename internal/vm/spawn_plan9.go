// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build plan9

package vm

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// This is the GOOS=plan9 counterpart of spawn_native.go. Plan 9 has /bin/rc, not
// /bin/sh, so a one-string command line goes through `rc -c`; resource limits,
// process groups, sessions and priorities do not exist and raise
// NotImplementedError, as MRI does where the call is missing (rb_f_notimplement:
// "<name>() function is unimplemented on this machine"); and Process.kill writes a
// note to /proc/<pid>/note through the Go os package.

// runCaptured runs cmd, returning its combined output and exit code (see
// spawn_native.go for the contract). A one-string command with metacharacters goes
// through rc.
var runCaptured = func(cmd []string) (string, int) {
	if len(cmd) == 0 {
		return "", 127
	}
	var c *exec.Cmd
	if s, sh := shellish(cmd); sh {
		c = exec.Command("/bin/rc", "-c", s)
	} else {
		c = exec.Command(cmd[0], cmd[1:]...)
	}
	out, err := c.CombinedOutput()
	return string(out), exitCodeOf(err)
}

// systemCommand runs cmd for Kernel#system (see spawn_native.go).
var systemCommand = func(cmd []string) (out string, status int, spawned bool) {
	if len(cmd) == 0 {
		return "", 127, false
	}
	var c *exec.Cmd
	if s, sh := shellish(cmd); sh {
		c = exec.Command("/bin/rc", "-c", s)
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

// procPosix is true so Process.kill is defined; every other member of the POSIX
// surface raises NotImplementedError through the seams below.
const procPosix = true

// rlimitResources names the six resources every POSIX system has so that
// Process.getrlimit(:CORE) gets past the name lookup and reaches the seam that
// raises NotImplementedError (an empty table would raise ArgumentError "invalid
// resource name" first). The numbers are placeholders no call ever uses: Plan 9
// has no resource limits. rlimitValues and prioTargets stay empty.
var (
	rlimitResources = map[string]int{"CORE": 0, "CPU": 1, "DATA": 2, "FSIZE": 3, "NOFILE": 4, "STACK": 5}
	rlimitValues    = map[string]uint64{}
	prioTargets     = map[string]int{}
)

// notImplemented raises the NotImplementedError MRI's rb_f_notimplement does for a
// call the platform lacks.
func notImplemented(name string) {
	raise("NotImplementedError", "%s() function is unimplemented on this machine", name)
}

var (
	procGetrlimit = func(int) (cur, max uint64, err error) {
		notImplemented("getrlimit")
		return 0, 0, nil
	}
	procSetrlimit = func(int, uint64, uint64) error {
		notImplemented("setrlimit")
		return nil
	}
	procGetpriority = func(int, int) (int, error) {
		notImplemented("getpriority")
		return 0, nil
	}
	procSetpriority = func(int, int, int) error {
		notImplemented("setpriority")
		return nil
	}
	procGetpgid = func(int) (int, error) {
		notImplemented("getpgid")
		return 0, nil
	}
	procSetpgid = func(int, int) error {
		notImplemented("setpgid")
		return nil
	}
	procGetsid = func(int) (int, error) {
		notImplemented("getsid")
		return 0, nil
	}
	// procRusage reports no CPU time: Plan 9 has no getrusage.
	procRusage = func() (utime, stime float64) { return 0, 0 }
)

// errNoSuchProcess is the error Process.kill reports for a pid with no /proc entry.
// Plan 9 has no ESRCH; plan9ErrnoName reads this text back as Errno::ESRCH.
var errNoSuchProcess = syscall.NewError(plan9ESRCHText)

// procKill delivers signal number sig to pid as a Plan 9 note
// (/proc/<pid>/note, written by os.(*Process).Signal). Signal 0 only checks that
// /proc/<pid> exists, as kill(pid, 0) does. A signal with no note, and a negative
// pid (Plan 9 has no process groups to address), fail with Errno::EINVAL.
var procKill = func(pid, sig int) error {
	if pid < 0 {
		return syscall.EINVAL
	}
	if sig == 0 {
		if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); err != nil {
			return errNoSuchProcess
		}
		return nil
	}
	note, ok := plan9NoteFor(sig)
	if !ok {
		return syscall.EINVAL
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return errNoSuchProcess
	}
	if err := p.Signal(syscall.Note(note)); err != nil {
		if os.IsNotExist(err) {
			return errNoSuchProcess
		}
		return err
	}
	return nil
}

// runSpawnProc runs one prepared child to completion (see spawn_native.go).
var runSpawnProc = func(r *spawnReq) int {
	var c *exec.Cmd
	if r.shell != "" {
		c = exec.Command("/bin/rc", "-c", r.shell)
	} else {
		c = exec.Command(r.path, r.argv[1:]...)
		c.Args[0] = r.argv[0]
	}
	c.Dir, c.Env, c.Stdout, c.Stderr = r.dir, r.env, r.stdout, r.stderr
	return exitCodeOf(c.Run())
}
