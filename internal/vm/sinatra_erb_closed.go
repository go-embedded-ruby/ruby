//go:build rbgo_closed

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// sinatraErbEval is unavailable in a closed-world binary: rendering a template
// evaluates its compiled Ruby source, which needs the front-end (parser +
// CompileWithLocals) that a closed-world build drops from the link — the same
// reason Binding#eval and Kernel#eval on source raise there. Routing/dispatch and
// the rest of the Sinatra DSL still work; only `erb` view rendering raises.
func (vm *VM) sinatraErbEval(sc *SinatraCtx, src string, names []string, vals []object.Value) object.Value {
	_, _, _, _ = sc, src, names, vals
	raise("NotImplementedError", "Sinatra#erb view rendering is unavailable in a closed-world binary (built with rbgo build --closed, without the front-end)")
	return nil // unreachable: raise panics
}
