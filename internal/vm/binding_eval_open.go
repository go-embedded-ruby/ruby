//go:build !rbgo_closed

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// bindingEval compiles src against the binding's locals (resolved at depth 1 via
// a child scope) and runs it with the binding's environment, self and definee —
// so the eval'd code sees and writes the binding's local variables. It uses the
// front-end directly (CompileWithLocals), so a closed-world build replaces it
// with the stub in binding_eval_closed.go.
func (vm *VM) bindingEval(b *Binding, srcV object.Value) object.Value {
	s, ok := srcV.(*object.String)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(srcV))
	}
	prog, perr := parser.Parse(s.Str())
	if perr != nil {
		raise("SyntaxError", "%s", perr.Error())
	}
	iseq, cerr := compiler.CompileWithLocals(prog, b.names)
	if cerr != nil {
		raise("SyntaxError", "%s", cerr.Error())
	}
	iseq.Name = "(eval)"
	return vm.exec(iseq, b.self, nil, b.definee, "", b.env, nil, nil, nil)
}
