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
func (vm *VM) bindingEval(b *Binding, src string, loc evalLocation) object.Value {
	prog, perr := parser.Parse(src)
	if perr != nil {
		raiseEvalSyntaxError(loc, perr)
	}
	iseq, cerr := compiler.CompileEvalWithLocals(prog, b.names, loc.line)
	if cerr != nil {
		raiseEvalSyntaxError(loc, cerr)
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
		iseq, _ = compiler.CompileEvalWithLocals(prog, b.names, loc.line)
	}
	iseq.Name = "(eval)"
	// The location is the eval's own, exactly as on the no-binding path: MRI
	// reaches eval_string_with_scope through the same rb_f_eval, so the file and
	// first line given to eval(str, b, file, line) — or Binding#eval(str, file,
	// line) — stamp the compiled ISeq, and the binding's own source location is
	// left untouched (bind_location reads the binding, not the eval).
	setISeqFile(iseq, loc.file)
	// eval is transparent to Kernel#__method__ / #__callee__: the evaluated code
	// inherits the caller's method context (so `eval("__method__")`, which routes
	// through the caller's binding, reports the enclosing method), matching the
	// no-binding eval path.
	vm.pendingMethodCtx = vm.currentMethodCtxPtr()
	return vm.exec(iseq, b.self, nil, b.definee, "", b.env, nil, nil, nil, nil)
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
