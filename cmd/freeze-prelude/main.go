// Command freeze-prelude regenerates internal/vm/prelude_frozen_gen.go: it
// compiles the embedded-Ruby prelude (internal/vm/prelude.rb) and writes its
// bytecode out as a Go literal (embeddedPrelude), so a closed-world binary can
// load the standard library without linking the front-end.
//
// It finds the module root itself, so both spellings work:
//
//	go run ./cmd/freeze-prelude   # from the module root
//	go generate ./internal/vm     # which runs it FROM internal/vm
//
// That matters because the second is what TestEmbeddedPreludeMatchesSource
// tells you to run, and with paths taken relative to the working directory it
// could not work at all: go:generate runs a command in the directory of the
// file carrying the directive.
//
// TestEmbeddedPreludeMatchesSource fails if the committed file drifts from the
// prelude source, prompting a regeneration.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-embedded-ruby/ruby/internal/aot"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

const (
	srcRel = "internal/vm/prelude.rb"
	outRel = "internal/vm/prelude_frozen_gen.go"
)

// moduleRoot walks up from the working directory to the directory holding
// go.mod, so the paths above can be module-relative wherever this is invoked
// from. A missing go.mod is fatal rather than a silent fall back to the working
// directory: writing the generated file to the wrong place would leave the
// committed one stale while reporting success.
func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		fatal("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			fatal("no go.mod above %q: run this inside the module", dir)
		}
		dir = parent
	}
}

func main() {
	root := moduleRoot()
	srcPath := filepath.Join(root, srcRel)
	outPath := filepath.Join(root, outRel)
	src, err := os.ReadFile(srcPath)
	if err != nil {
		fatal("read %s: %v", srcPath, err)
	}
	prog, err := parser.Parse(string(src))
	if err != nil {
		fatal("parse prelude: %v", err)
	}
	// prelude.rb carries `# frozen_string_literal: true`, and the interpreter's
	// own path (vm's parseCompileFn) reads it. Compiling here without it made
	// the frozen blob disagree with a fresh compile of the same file, which is
	// exactly what TestEmbeddedPreludeMatchesSource guards.
	iseq, err := compiler.CompileWithMagic(prog, compiler.MagicComments(string(src)))
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
