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

	if err := run(src, name); err != nil {
		reportError(err)
		os.Exit(1)
	}
}

func run(src, name string) error {
	prog, err := parser.Parse(src)
	if err != nil {
		return err
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		return err
	}
	iseq.Name = name
	machine := vm.New(os.Stdout)
	if name != "-e" {
		machine.SetScriptPath(name) // so require/require_relative resolve relative to the script
	}
	_, err = machine.Run(iseq)
	return err
}

// reportError prints an uncaught error to stderr. A Ruby exception (vm.RubyError)
// is rendered MRI-style — "<frame>: <message> (<Class>)" with a "\tfrom <frame>"
// line per outer frame from its backtrace — so a crashing program shows the call
// chain that led to the raise. A non-Ruby error (parse/compile/IO) prints plainly.
func reportError(err error) {
	rerr, ok := err.(vm.RubyError)
	if !ok {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return
	}
	frames := rerr.Backtrace()
	body := rerr.Message + " (" + rerr.Class + ")"
	if len(frames) == 0 {
		fmt.Fprintln(os.Stderr, body)
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", frames[0], body)
	for _, f := range frames[1:] {
		fmt.Fprintf(os.Stderr, "\tfrom %s\n", f)
	}
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
