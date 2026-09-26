// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build wasm || windows

package main

// dieBySignal cannot do what signal.c ruby_default_signal does on these targets,
// for two different reasons, and it is deliberately the same no-op for both.
//
// On wasm neither wasip1 nor js/wasm has kill(2) or a settable signal
// disposition, so there is no way to be killed BY a signal at all.
//
// On Windows there is no kill(2) either (Go does not define syscall.Kill there),
// and MRI's own answer is a different mechanism — the C runtime's raise(), whose
// observable effect on a waiting parent is not the POSIX WIFSIGNALED this code
// would be imitating. **There is no Windows host here to measure that on**, so
// rather than adapt the assertion to a guess, this returns and the caller exits
// 128+signo: the same NUMBER a POSIX shell reports, and a defensible status for a
// program killed by a signal, with the platform-specific part left unclaimed.
// The two end-to-end rows that assert death BY a signal skip on Windows and say
// this is why (cmd/rbgo/exit_status_test.go is POSIX-only for the same reason).
func dieBySignal(int) {}
