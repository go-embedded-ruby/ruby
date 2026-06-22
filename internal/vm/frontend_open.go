//go:build !rbgo_closed

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// parseCompileFn turns Ruby source into a runnable ISeq using the embedded
// front-end (lexer → parser → compiler). Routing eval, require and the prelude
// through this one seam is what lets `rbgo build --closed` drop the front-end:
// the closed build replaces this file with frontend_closed.go, whose stub raises
// instead — so the parser and compiler are never referenced and the linker drops
// them.
var parseCompileFn = openParseCompile

func openParseCompile(src string) (*bytecode.ISeq, error) {
	prog, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	return compiler.Compile(prog)
}
