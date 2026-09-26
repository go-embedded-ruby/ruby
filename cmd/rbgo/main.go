//go:build !rbgo_closed

// Command rbgo is the CLI front-end for the embedded-ruby interpreter.
//
// A closed-world binary (produced by `rbgo build --closed`) instead uses the
// generated main in closed_main.go, which runs a single embedded program with no
// front-end linked — so this CLI is excluded from that build.
//
//	rbgo run <file.rb>            compile in memory and interpret
//	rbgo run -e "<code>"          run a one-liner
//	rbgo <file.rb>                shorthand for `rbgo run`
//	rbgo build [-o out] <file.rb> AOT-compile the program's lowerable methods to
//	                             native Go and link a specialised binary (see
//	                             internal/aot and docs/aot-compiler.md)
//
// `repl` arrives in a later phase (plan-rbgo.md §17).
package main

import (
	"fmt"
	"os"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
	}

	cmd := args[0]
	switch cmd {
	case "run":
		runCmd(args[1:])
	case "build":
		buildCmd(args[1:])
	case "-h", "--help", "help":
		usage()
	default:
		// Shorthand: `rbgo file.rb`.
		runCmd(args)
	}
}

func runCmd(args []string) {
	var src, name string
	switch {
	case len(args) == 2 && args[0] == "-e":
		src, name = args[1], "-e"
	case len(args) == 1:
		b, err := os.ReadFile(args[0])
		if err != nil {
			fatal("rbgo: %v", err)
		}
		src, name = string(b), args[0]
	default:
		usage()
	}

	// finish never returns: it is MRI's ruby_run_node -> rb_ec_cleanup, which
	// turns the terminal exception into this process's exit status (or death by
	// signal) rather than always exiting 0 or 1. See exit_status.go.
	finish(run(src, name))
}

// run compiles and interprets one program, returning the machine it ran on
// alongside the error, because the terminal exception has to be classified
// against that machine's class hierarchy (vm.ExitingSplit). The machine is
// non-nil whenever the program actually ran; a parse or compile failure returns
// nil, which finish handles.
func run(src, name string) (*vm.VM, error) {
	prog, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	iseq, err := compiler.CompileWithEncoding(prog, compiler.MagicSourceEncoding(src))
	if err != nil {
		return nil, err
	}
	iseq.Name = name
	// Program output goes to stdout; every diagnostic MRI puts on fd 2 — warnings,
	// $stderr/STDERR writes, Kernel#abort's message — goes to stderr, so a
	// redirected or piped stdout carries only the program's data (#667).
	machine := vm.NewWithStderr(os.Stdout, os.Stderr)
	if name == "-e" {
		// A -e one-liner has no file on disk; record "-e" as the program name so
		// backtraces label its frames "-e" the way MRI does (require_relative still
		// resolves against the CWD, since there is no script directory).
		machine.SetScriptName(name)
	} else {
		machine.SetScriptPath(name) // so require/require_relative resolve relative to the script
	}
	_, err = machine.Run(iseq)
	return machine, err
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: rbgo run <file.rb> | rbgo run -e \"<code>\" | rbgo <file.rb>")
	fmt.Fprintln(os.Stderr, "       rbgo build [-o out] <file.rb>   AOT-compile methods and link a native binary")
	os.Exit(2)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
