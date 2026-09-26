// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"os"

	"github.com/go-embedded-ruby/ruby/internal/vm"
)

// finish is the front-end's last step, MRI's ruby_run_node -> rb_ec_cleanup
// (eval.c). It is shared by every main in this package — the CLI, the
// closed-world native binary and the closed-world wasm binary — because a
// deliberate exit status has to reach the process from all three.
//
// rb_ec_cleanup splits the terminal exception with exiting_split (see
// vm.ExitingSplit), prints it only when exiting_split says so, and RETURNS the
// status to main(), which passes it to exit(). When a signal was named it does
// one more thing, last of all: ruby_default_signal(sig), which sets SIG_DFL and
// raise(sig)s, so the process dies BY the signal and a waiting shell sees
// WIFSIGNALED with 128+signo rather than a plain exit of the same number.
//
// finish never returns.
func finish(machine *vm.VM, err error) {
	if machine == nil {
		// The program never ran (a parse or compile failure), so there is no class
		// hierarchy to classify anything against and no Ruby exception to classify.
		if err != nil {
			reportError(err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if err == nil {
		// A program that ended by calling exit unwinds cleanly — Run reports no
		// error for a SystemExit — but its status is still the process's. This is
		// rb_ec_cleanup reading ec->errinfo after rb_ec_teardown has run the exit
		// handlers, which is why it is asked for here and not before.
		if texit, ok := machine.TerminalExit(); ok {
			err = texit
		} else {
			os.Exit(0)
		}
	}
	d, ok := machine.ExitingSplit(err)
	if !ok {
		// Not a Ruby exception: a parse, compile or host IO error.
		reportError(err)
		os.Exit(1)
	}
	if d.Message {
		reportError(err)
	}
	if d.Signal != 0 {
		dieBySignal(d.Signal)
		// dieBySignal returns only where the target cannot re-raise a signal at
		// all (wasm) or where the re-raise did not take effect; the status is then
		// the 128+signo a shell would have reported anyway.
	}
	os.Exit(d.Status)
}

// reportError prints an uncaught error to stderr. A Ruby exception (vm.RubyError)
// is rendered MRI-style — "<frame>: <message> (<Class>)" with a "\tfrom <frame>"
// line per outer frame from its backtrace — so a crashing program shows the call
// chain that led to the raise. A non-Ruby error (parse/compile/IO) prints plainly.
//
// An exception with an EMPTY message prints its class name alone, with no
// parenthesised suffix: eval_error.c print_errinfo takes the `elen == 0` branch
// and writes rb_class_name(eclass) by itself. That is the shape an uncaught
// Interrupt has, since Interrupt.new carries no message.
func reportError(err error) {
	rerr, ok := err.(vm.RubyError)
	if !ok {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return
	}
	frames := rerr.Backtrace()
	body := rerr.Message + " (" + rerr.Class + ")"
	if rerr.Message == "" {
		body = rerr.Class
	}
	if len(frames) == 0 {
		fmt.Fprintln(os.Stderr, body)
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", frames[0], body)
	for _, f := range frames[1:] {
		fmt.Fprintf(os.Stderr, "\tfrom %s\n", f)
	}
}
