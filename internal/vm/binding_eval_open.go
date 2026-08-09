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
	// A top-scope assignment in the eval string creates a new local in the
	// binding's scope (MRI: `eval("x = 1", b)` then `b.local_variable_get(:x)`).
	// The first compile surfaces those new names as the eval block's own locals
	// (iseq.Locals); inject each into the binding — extending its environment so
	// the value persists — and recompile so the references resolve against the
	// binding's frame (depth 1) rather than the throwaway eval-block env.
	if newNames := bindingNewLocals(iseq.Locals); len(newNames) > 0 {
		for _, n := range newNames {
			b.names = append(b.names, n)
			b.added = append(b.added, n)
			b.env.slots = append(b.env.slots, object.NilV)
		}
		// Re-seeding the same program with additional valid local names cannot
		// introduce a compile error, so the earlier check already covered it.
		iseq, _ = compiler.CompileWithLocals(prog, b.names)
	}
	iseq.Name = "(eval)"
	return vm.exec(iseq, b.self, nil, b.definee, "", b.env, nil, nil, nil)
}

// bindingNewLocals returns the named top-scope locals a compiled eval body
// introduces. The eval block's own Locals are exactly the variables it declares
// at top level: an existing binding local the body assigns to resolves up-scope
// and never lands here, so every named entry is genuinely new. Anonymous slots
// (a `case` subject compiles to a "" local) are not variables and are dropped.
func bindingNewLocals(evalLocals []string) []string {
	var out []string
	for _, n := range evalLocals {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}
