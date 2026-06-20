package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// AOT dispatch seam. A method whose body was lowered to native Go at `rbgo
// build` time (see internal/aot) is registered here keyed by "Owner#name"; when
// that method is *first* defined at run time the compiled entry is attached to
// its Method, and invoke() prefers it over interpreting the ISeq. A later
// redefinition (monkey-patch, define_method, eval) installs a fresh Method with
// no compiled entry — a sound deopt back to the interpreter, since the compiled
// body no longer matches the source.

// CompiledMethod is the signature every AOT-emitted method body has: it mirrors
// `func (vm *VM) name(self object.Value, args []object.Value, block *Proc)
// object.Value`, taken as an unbound method value.
type CompiledMethod func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value

// compiledRegistry maps "Owner#name" to the method's AOT-compiled body. It is
// populated by an init() that `rbgo build` generates alongside the lowered
// functions; it is empty in a plain interpreter run.
var compiledRegistry = map[string]CompiledMethod{}

// RegisterCompiled records fn as the AOT-compiled body for key ("Owner#name").
// Generated build output calls this from init(), before any Ruby runs.
func RegisterCompiled(key string, fn CompiledMethod) {
	compiledRegistry[key] = fn
}

// compiledFor returns the registered compiled body for owner#name, if any.
func compiledFor(owner, name string) CompiledMethod {
	return compiledRegistry[owner+"#"+name]
}
