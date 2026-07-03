package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestAOTBlockArgs covers every shaping branch of aotBlockArgs: exact arity, the
// single-Array auto-splat to a multi-parameter block, a non-Array single arg,
// and padding a short argument list with nil.
func TestAOTBlockArgs(t *testing.T) {
	one := object.Integer(1)
	two := object.Integer(2)
	pair := &object.Array{Elems: []object.Value{object.IntValue(int64(one)), object.IntValue(int64(two))}}

	// Exact arity: the args pass straight through (same backing slice).
	if got := aotBlockArgs(1, []object.Value{object.IntValue(int64(one))}); len(got) != 1 || got[0] != one {
		t.Errorf("exact arity: got %v", got)
	}
	// np>1, a single Array arg: auto-splat into the elements.
	got := aotBlockArgs(2, []object.Value{object.Wrap(pair)})
	if len(got) != 2 || got[0] != one || got[1] != two {
		t.Errorf("auto-splat: got %v", got)
	}
	// np>1, a single non-Array arg: no splat, pad the missing parameter with nil.
	got = aotBlockArgs(2, []object.Value{object.IntValue(int64(one))})
	if len(got) != 2 || got[0] != one || got[1] != nil {
		t.Errorf("non-array single arg: got %v", got)
	}
	// A short argument list to a fixed-arity block pads with nil (no auto-splat
	// when np==1).
	got = aotBlockArgs(1, nil)
	if len(got) != 1 || got[0] != nil {
		t.Errorf("padding: got %v", got)
	}
	// More args than params: truncate to np.
	got = aotBlockArgs(1, []object.Value{object.IntValue(int64(one)), object.IntValue(int64(two))})
	if len(got) != 1 || got[0] != one {
		t.Errorf("truncate: got %v", got)
	}
}

// TestAOTSend covers every arm of the AOT send fast path: the cached
// non-class-receiver path with and without an explicit-receiver visibility
// check, an unresolved name falling through to the operator dispatch, a class
// receiver bypassing the cache, and a literal-block send routed through
// dispatchSend.
func TestAOTSend(t *testing.T) {
	vm := New(io.Discard)
	var ic inlineCache
	self := vm.main

	// Explicit receiver, resolved method: String#length via the cache + a
	// (passing) visibility check.
	if r := vm.aotSend(&ic, object.Wrap(object.NewString("hi")), "length", nil, bytecode.FlagSendExplicit, self, nil); r.Inspect() != "2" {
		t.Errorf("explicit length = %s, want 2", r.Inspect())
	}
	// Implicit receiver, resolved method: no visibility check taken.
	if r := vm.aotSend(&ic, object.Wrap(&object.Array{Elems: []object.Value{object.NilVal(), object.NilVal()}}), "length", nil, 0, self, nil); r.Inspect() != "2" {
		t.Errorf("implicit length = %s, want 2", r.Inspect())
	}
	// Unresolved name (`+` is an operator fast path, not a method): the cache
	// misses (nil) and the call falls through to dispatchSend → binaryOp.
	var ic2 inlineCache
	if r := vm.aotSend(&ic2, object.IntValue(int64(object.Integer(1))), "+", []object.Value{object.IntValue(int64(object.Integer(2)))}, 0, self, nil); r.Inspect() != "3" {
		t.Errorf("operator + = %s, want 3", r.Inspect())
	}
	// Class receiver: bypasses the cache (singleton dispatch) and goes through
	// dispatchSend.
	intClass := vm.classOf(object.IntValue(int64(object.Integer(0))))
	if r := vm.aotSend(&ic, object.Wrap(intClass), "name", nil, 0, self, nil); r.Inspect() != `"Integer"` {
		t.Errorf("class name = %s, want \"Integer\"", r.Inspect())
	}
	// A literal-block send: blk != nil skips the fast path and dispatches with the
	// native block, which each element is yielded to.
	sum := 0
	blk := &Proc{native: func(_ *VM, args []object.Value) object.Value {
		sum += int(object.AsInteger(args[0]))
		return object.NilVal()
	}}
	arr := &object.Array{Elems: []object.Value{object.IntValue(int64(object.Integer(2))), object.IntValue(int64(object.Integer(3)))}}
	vm.aotSend(&ic, object.Wrap(arr), "each", nil, 0, self, blk)
	if sum != 5 {
		t.Errorf("block each sum = %d, want 5", sum)
	}
}

// TestRunTopCompiled covers runTop dispatching to a registered compiled main
// (the armed path), and the latch that fires it exactly once.
func TestRunTopCompiled(t *testing.T) {
	prev := compiledMainFn
	t.Cleanup(func() { compiledMainFn = prev })

	calls := 0
	sentinel := object.NewString("from-aot-main")
	RegisterCompiledMain(func(_ *VM) object.Value {
		calls++
		return object.Wrap(sentinel)
	})

	vm := New(io.Discard) // arms mainArmed after the prelude
	if got := vm.runTop(&bytecode.ISeq{}); got != sentinel {
		t.Errorf("runTop = %v, want the compiled main's result", got)
	}
	if calls != 1 || vm.mainArmed {
		t.Errorf("compiled main should fire once and disarm: calls=%d armed=%v", calls, vm.mainArmed)
	}
}
