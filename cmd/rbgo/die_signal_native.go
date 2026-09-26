// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !wasm

package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

// dieBySignal is signal.c ruby_default_signal: restore the signal's default
// disposition and raise(sig) at self, so the process is KILLED BY the signal
// rather than exiting with a number that merely looks the same. A shell then
// reports 128+signo with WIFSIGNALED set, which is what MRI leaves behind for an
// uncaught SignalException and what `$?.signaled?` reads in a parent.
//
// signal.Reset undoes any os/signal.Notify the process installed, which is the
// Go-level equivalent of `signal(sig, SIG_DFL)`; the Go runtime's own handler for
// a signal nobody is watching already performs the default action (for SIGTERM
// that is to die, resetting to SIG_DFL and re-raising internally).
//
// It returns instead of blocking for ever if the signal has not taken effect,
// so the caller can still exit with 128+signo. The wait is not load-bearing: a
// slow host does not change the status the caller reports, only how long it
// takes to report it.
func dieBySignal(signo int) {
	sig := syscall.Signal(signo)
	signal.Reset(sig)
	if err := syscall.Kill(os.Getpid(), sig); err != nil {
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}
