//go:build rbgo_closed

package vm

import "github.com/go-embedded-ruby/ruby/internal/bytecode"

// parseCompileFn is the closed-world stub: a binary built with `rbgo build
// --closed` links no lexer/parser/compiler, so any runtime attempt to compile
// Ruby (eval, a `.rb` require) raises instead of parsing. Its compiled program
// and the prelude are baked in as bytecode (see FreezeISeq / embeddedProgram /
// embeddedPrelude), which need no front-end.
var parseCompileFn = frontendDropped

func frontendDropped(string) (*bytecode.ISeq, error) {
	raise("NotImplementedError", "eval/require of source is unavailable in a closed-world binary (built with rbgo build --closed, without the front-end)")
	return nil, nil // unreachable: raise panics
}

// parseCompileEvalFn is the closed-world stub of the eval seam (see
// frontend_open.go): it refuses for the same reason and with the same message.
var parseCompileEvalFn = func(src string, firstLine int) (*bytecode.ISeq, error) {
	_ = firstLine
	return frontendDropped(src)
}
