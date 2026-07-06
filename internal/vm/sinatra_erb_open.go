//go:build !rbgo_closed

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// sinatraErbEval evaluates the compiled ERB source in the handler's binding: the
// SinatraCtx is self (so @ivars set in the action and the request helpers such as
// `params` resolve) and the render locals are bound as depth-1 local variables,
// exactly as MRI Sinatra evaluates a Tilt template against the route instance's
// binding. It compiles the source in a child scope over a synthetic parent frame
// holding the locals (compiler.CompileWithLocals), seeds that parent env with the
// locals' values in slot order and runs the ISeq with the context as self. The
// compiled template's final expression is its output buffer, so exec returns the
// rendered String. It uses the front-end directly, so a closed-world build
// replaces it with the stub in sinatra_erb_closed.go.
func (vm *VM) sinatraErbEval(sc *SinatraCtx, src string, names []string, vals []object.Value) object.Value {
	prog, perr := parser.Parse(src)
	if perr != nil {
		// go-ruby-erb emits valid Ruby for any well-formed template, so a parse error
		// here means the template's embedded Ruby (<% … %>) is itself malformed —
		// surfaced as a SyntaxError, matching MRI (which raises when eval'ing the
		// compiled source of a template with broken embedded code).
		raise("SyntaxError", "%s", perr.Error())
	}
	iseq, cerr := compiler.CompileWithLocals(prog, names)
	if cerr != nil {
		raise("SyntaxError", "%s", cerr.Error())
	}
	iseq.Name = "(erb)"
	// The synthetic parent frame holds the render locals by slot, so the compiled
	// template resolves them at depth 1. A copy of vals backs the env slots so the
	// caller's slice is never retained/mutated.
	slots := make([]object.Value, len(vals))
	copy(slots, vals)
	parent := &Env{slots: slots}
	return vm.exec(iseq, sc, nil, vm.classOf(sc), "", parent, nil, nil, nil)
}
