// Command freeze-prelude regenerates internal/vm/prelude_frozen_gen.go: it
// compiles the embedded-Ruby prelude (internal/vm/prelude.rb) and writes its
// bytecode out as a Go literal (embeddedPrelude), so a closed-world binary can
// load the standard library without linking the front-end.
//
// Run from the module root (see the //go:generate directive in prelude.go):
//
//	go run ./cmd/freeze-prelude
//
// TestEmbeddedPreludeMatchesSource fails if the committed file drifts from the
// prelude source, prompting a regeneration.
package main

import (
	"fmt"
	"os"

	"github.com/go-embedded-ruby/ruby/internal/aot"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

const (
	srcPath = "internal/vm/prelude.rb"
	outPath = "internal/vm/prelude_frozen_gen.go"
)

func main() {
	src, err := os.ReadFile(srcPath)
	if err != nil {
		fatal("read %s: %v", srcPath, err)
	}
	prog, err := parser.Parse(string(src))
	if err != nil {
		fatal("parse prelude: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		fatal("compile prelude: %v", err)
	}
	out := aot.FreezeISeq(iseq, "vm", "embeddedPrelude", "")
	if err := os.WriteFile(outPath, []byte(out), 0o644); err != nil {
		fatal("write %s: %v", outPath, err)
	}
	fmt.Fprintf(os.Stderr, "freeze-prelude: wrote %s\n", outPath)
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "freeze-prelude: "+format+"\n", a...)
	os.Exit(1)
}
