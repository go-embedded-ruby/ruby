//go:build rbgo_closed

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// bindingEval is unavailable in a closed-world binary: it would need the
// compiler (CompileWithLocals), which is dropped from the link. Capturing a
// binding and inspecting its locals (local_variable_get/set, local_variables,
// receiver) still works — only eval'ing source against it raises.
func (vm *VM) bindingEval(b *Binding, srcV object.Value) object.Value {
	_ = b
	_ = srcV
	raise("NotImplementedError", "Binding#eval is unavailable in a closed-world binary (built with rbgo build --closed, without the front-end)")
	return nil // unreachable: raise panics
}
