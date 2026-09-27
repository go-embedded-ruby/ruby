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
	return openParseCompileEval(src, 1)
}

// parseCompileEvalFn is parseCompileFn for a string-eval given an explicit first
// line: the offset must reach the COMPILER (see compiler.CompileEval), because
// `__LINE__` becomes an Integer in the constant pool that no later pass can
// distinguish from any other. It is a second seam rather than an argument to the
// first so the many require/prelude callers keep the shape they have.
var parseCompileEvalFn = openParseCompileEval

func openParseCompileEval(src string, firstLine int) (*bytecode.ISeq, error) {
	prog, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	return compiler.CompileEval(prog, compiler.MagicSourceEncoding(src), firstLine)
}
