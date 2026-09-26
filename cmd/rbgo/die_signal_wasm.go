// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build wasm

package main

// dieBySignal cannot do what signal.c ruby_default_signal does on wasm: neither
// wasip1 nor js/wasm has kill(2) or a settable signal disposition, so there is no
// way to be killed BY a signal there. It returns at once and the caller exits
// with 128+signo — the same NUMBER a POSIX shell would report, without the
// WIFSIGNALED flag that no wasm host can produce.
//
// Nothing else in this change needs a build tag: the signal disposition table and
// Process.kill's self arm are pure Go with no syscall in them, and Process.kill
// itself is only registered on POSIX (registerProcessPosix), so a wasm program
// reaches neither.
func dieBySignal(int) {}
