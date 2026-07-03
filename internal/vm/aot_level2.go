package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Level-2 AOT runtime seam. The program's top-level code (the `<main>` ISeq) can
// be lowered to native Go at `rbgo build` time (internal/aot.CompileMain): a
// `func (vm *VM) aotMain() object.Value` registered here, which Run dispatches to
// instead of interpreting the top level. Its literal blocks are lowered to
// native Procs whose bodies are compiled Go; each send it makes goes through
// aotSend, matching the interpreter's cached monomorphic dispatch.

// CompiledMain is the signature the AOT-emitted top-level body has (the program
// runs against the VM's own `main` self, so no self/args are threaded in).
type CompiledMain func(vm *VM) object.Value

// compiledMainFn is the AOT-compiled top level, installed by an init() that
// `rbgo build` generates; nil in a plain interpreter run, in which case Run
// interprets the top-level ISeq as before.
var compiledMainFn CompiledMain

// RegisterCompiledMain records fn as the AOT-compiled program top level.
// Generated build output calls this from init(), before any Ruby runs.
func RegisterCompiledMain(fn CompiledMain) { compiledMainFn = fn }

// runTop runs the program's top level: the AOT-compiled native body when one was
// linked in, else the interpreted ISeq (self = the top-level main object,
// definee = Object — exactly what Run passed before).
func (vm *VM) runTop(iseq *bytecode.ISeq) object.Value {
	if vm.mainArmed && compiledMainFn != nil {
		vm.mainArmed = false // fire once, for the user program only (not nested require/eval)
		return compiledMainFn(vm)
	}
	return vm.exec(iseq, vm.main, nil, vm.cObject, "", nil, nil, nil, nil)
}

// aotBlockArgs shapes a native block's raw yield arguments to its np fixed
// positional parameters, mirroring bindBlockPositionals for the fixed-arity case
// (no splat / optional / keyword params — the only blocks CompileMain lowers): a
// single Array yielded to a multi-parameter block auto-splats, and a short/long
// argument list pads with nil / truncates.
func aotBlockArgs(np int, args []object.Value) []object.Value {
	if np > 1 && len(args) == 1 {
		if arr, ok := object.KindOK[*object.Array](args[0]); ok {
			args = arr.Elems
		}
	}
	if len(args) == np {
		return args
	}
	out := make([]object.Value, np)
	for i := range out {
		if i < len(args) {
			out[i] = args[i]
		}
	}
	return out
}

// aotSend is the send path for AOT-compiled top-level/block code. It mirrors the
// interpreter's OpSend exactly: a non-class receiver with no literal block takes
// the per-site inline cache (ic) monomorphic fast path — a pointer-compare method
// resolution + an explicit-receiver visibility check against the same resolved
// method — and everything else (class receiver, unresolved name, a block) falls
// back to dispatchSend, after the same explicit-visibility enforcement. self is
// the caller's self, for the protected-method check.
func (vm *VM) aotSend(ic *inlineCache, recv object.Value, name string, args []object.Value, flags int, self object.Value, blk *Proc) object.Value {
	if blk == nil {
		if _, isClass := object.KindOK[*RClass](recv); !isClass {
			if m := vm.lookupCached(ic, recv, name); m != nil {
				if flags&bytecode.FlagSendExplicit != 0 {
					vm.checkVisibility(recv, name, m, self)
				}
				return vm.invokeInPlace(m, recv, args, blk)
			}
		}
	}
	vm.enforceSendVis(flags, recv, name, self)
	return vm.dispatchSend(recv, name, args, blk)
}
