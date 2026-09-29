//go:build !rbgo_closed

// Command rbgo is the CLI front-end for the embedded-ruby interpreter.
//
// A closed-world binary (produced by `rbgo build --closed`) instead uses the
// generated main in closed_main.go, which runs a single embedded program with no
// front-end linked — so this CLI is excluded from that build.
//
//	rbgo [options] [--] <file.rb> [arguments...]   interpret, ARGV = the arguments
//	rbgo [options] -e "<code>" [arguments...]      run a one-liner
//	rbgo run ...                                   the same, spelled explicitly
//	rbgo build [-o out] <file.rb>                  AOT-compile the program's
//	                                               lowerable methods to native Go
//	                                               and link a specialised binary
//	                                               (see internal/aot and
//	                                               docs/aot-compiler.md)
//
// The options and the script/ARGV boundary follow ruby.c; options.go holds the
// rule and the citations.
//
// `repl` arrives in a later phase (plan-rbgo.md §17).
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		// No arguments at all is a usage error. MRI would read the program from
		// stdin here; rbgo keeps the usage message because it is a multi-command
		// front-end (run/build) and a bare `rbgo` silently waiting on a terminal
		// would be worse. `rbgo -` still reads stdin, as does `rbgo` with options
		// but no script — see options.load.
		usageError()
	}

	switch args[0] {
	case "run":
		if len(args) == 1 {
			usageError()
		}
		runCmd(args[1:])
	case "build":
		buildCmd(args[1:])
	case "help":
		help()
	default:
		// Everything else is `ruby`'s own command line, including -h/--help, which
		// parseOptions recognises. A script whose name happens to be "run",
		// "build" or "help" needs a path ("./run"), which was already true.
		runCmd(args)
	}
}

func runCmd(args []string) {
	o, err := parseOptions(args, os.Stderr)
	if err != nil {
		// MRI's own wording and exit status, not a usage summary (#708).
		fatal("%s", err)
	}
	if o.help {
		help()
	}
	src, name, fromFile, err := o.load()
	if err != nil {
		var le *loadError
		if errors.As(err, &le) {
			// MRI's own LoadError line for an unreadable script, not Go's
			// *os.PathError wording.
			fatal("%s", le)
		}
		fatal("rbgo: %v", err)
	}

	// finish never returns: it is MRI's ruby_run_node -> rb_ec_cleanup, which
	// turns the terminal exception into this process's exit status (or death by
	// signal) rather than always exiting 0 or 1. See exit_status.go.
	finish(runProgram(src, name, fromFile, o))
}

// run compiles and interprets one program with an empty ARGV and the default
// $VERBOSE. It is the shape the tests and the embedded callers want; the CLI goes
// through runProgram, which also seeds what the command line asked for.
func run(src, name string) (*vm.VM, error) {
	return runProgram(src, name, name != "-e", nil)
}

// runProgram compiles and interprets one program, returning the machine it ran on
// alongside the error, because the terminal exception has to be classified
// against that machine's class hierarchy (vm.ExitingSplit). The machine is
// non-nil whenever the program actually ran; a parse or compile failure returns
// nil, which finish handles.
//
// o may be nil, which means an empty ARGV and an untouched $VERBOSE.
func runProgram(src, name string, fromFile bool, o *options) (*vm.VM, error) {
	prog, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	iseq, err := compiler.CompileWithMagic(prog, compiler.MagicComments(src))
	if err != nil {
		return nil, err
	}
	iseq.Name = name
	// Program output goes to stdout; every diagnostic MRI puts on fd 2 — warnings,
	// $stderr/STDERR writes, Kernel#abort's message — goes to stderr, so a
	// redirected or piped stdout carries only the program's data (#667).
	machine := vm.NewWithStderr(os.Stdout, os.Stderr)
	if fromFile {
		machine.SetScriptPath(name) // so require/require_relative resolve relative to the script
	} else {
		// A -e one-liner and a stdin program have no file on disk; record the name
		// ("-e" / "-") so backtraces label their frames the way MRI does
		// (require_relative still resolves against the CWD, since there is no
		// script directory).
		machine.SetScriptName(name)
	}
	if o != nil {
		// ruby_set_argv, after the interpreter exists and before the program runs.
		machine.SetARGV(o.argv)
		if o.warnSet {
			// proc_W_option only assigns ruby_verbose when a -w/-W was given, so an
			// untouched $VERBOSE stays unset here too rather than being written with
			// the value it already reads as (globals.go verboseSlot).
			machine.SetWarningLevel(o.warnLevel)
		}
	}
	_, err = machine.Run(iseq)
	return machine, err
}

// usageText is rbgo's own summary. MRI's -h describes switches rbgo does not
// have, so this is deliberately rbgo's interface and not a transcription.
const usageText = `usage: rbgo [options] [--] <file.rb> [arguments...]   interpret a script; ARGV = [arguments...]
       rbgo [options] -e "<code>" [arguments...]      run a one-liner
       rbgo -                                         read the program from stdin
       rbgo run ...                                   the same, spelled explicitly
       rbgo build [-o out] [--closed] <file.rb>       AOT-compile methods and link a native binary

options: -w, -W, -W0 | -W1 | -W2, -W:<category>   $VERBOSE: -W0 silent, -W1 default, -w/-W2 verbose
         -e <code>                                may be repeated; then ARGV takes no script
         -h, --help                               this message`

// help prints the usage summary as a successful answer to a question — stdout,
// exit 0, which is what `ruby -h` does and what a shell pipeline expects. It
// never returns.
func help() {
	fmt.Fprintln(os.Stdout, usageText)
	os.Exit(0)
}

// usageError prints the same summary as a complaint: stderr, exit 2. It never
// returns.
func usageError() {
	fmt.Fprintln(os.Stderr, usageText)
	os.Exit(2)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
