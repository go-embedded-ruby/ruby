// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build wasm

package vm

// runCaptured has no meaning under js/wasm (no subprocesses), so spawning a
// command raises NotImplementedError there rather than silently succeeding.
var runCaptured = func(cmd []string) (string, int) {
	raise("NotImplementedError", "subprocess execution is not supported on wasm")
	return "", 127
}

// systemCommand likewise has no meaning under js/wasm; Kernel#system raises.
var systemCommand = func(cmd []string) (string, int, bool) {
	raise("NotImplementedError", "subprocess execution is not supported on wasm")
	return "", 127, false
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

// runSpawnProc has no meaning under wasm (no subprocesses), so Process.spawn
// raises NotImplementedError there rather than silently succeeding.
var runSpawnProc = func(*spawnReq) int {
	raise("NotImplementedError", "subprocess execution is not supported on wasm")
	return 127
}
