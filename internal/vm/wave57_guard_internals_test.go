// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The guards below are reachable but not reached by any Ruby program the suite
// runs, so the coverage gate named them. They are exercised directly rather
// than left recorded: each one decides something, and a decision nothing
// checks is a decision that can be quietly inverted.

// TestBasicOpNameRefusesANonOperator: basicOpName answers "" for an opcode that
// is not one of the eleven, and the guard's callers return "no override" on
// that. The arm matters because the function used to be built on arithOpName,
// whose own default returns "%" -- which silently gave every comparison opcode
// the wrong name.
func TestBasicOpNameRefusesANonOperator(t *testing.T) {
	for _, op := range []bytecode.Op{bytecode.OpPop, bytecode.OpDup, bytecode.OpJump, bytecode.OpLast} {
		if got := basicOpName(op); got != "" {
			t.Errorf("basicOpName(%v) = %q, want \"\" for a non-operator opcode", op, got)
		}
	}
	// and every opcode it DOES name, as the control
	for _, op := range basicOps {
		if basicOpName(op) == "" {
			t.Errorf("basicOpName(%v) = \"\", but it is in basicOps", op)
		}
	}
}

// TestSnapshotAncestorStopsAtAnUnwatchedChain: a class with no basic-operator
// ancestor has no record to compare against, and the guard must then keep the
// inline path rather than treat "nothing recorded" as "redefined".
func TestSnapshotAncestorStopsAtAnUnwatchedChain(t *testing.T) {
	vm := New(io.Discard)
	if vm.basicOps == nil || !vm.basicOps.taken {
		t.Fatal("the snapshot is not taken; the rest of this test means nothing")
	}
	// BasicObject is above every watched class, so walking up from it finds none.
	if got := vm.snapshotAncestor(vm.cBasicObject); got != nil {
		t.Errorf("snapshotAncestor(BasicObject) = %v, want nil", got.name)
	}
	// The control: a watched class finds itself, and a subclass finds its base.
	if got := vm.snapshotAncestor(vm.cString); got != vm.cString {
		t.Errorf("snapshotAncestor(String) = %v, want String", got)
	}
	sub := newClass("StringSub", vm.cString)
	if got := vm.snapshotAncestor(sub); got != vm.cString {
		t.Errorf("snapshotAncestor(StringSub) = %v, want String", got)
	}
}

// TestBasicOpWasDefinedAcrossTheChain: the send path's operator fallback asks
// this before resurrecting a removed operator. It must answer for a watched
// class, for a subclass of one, and for neither.
func TestBasicOpWasDefinedAcrossTheChain(t *testing.T) {
	vm := New(io.Discard)
	one := object.IntValue(1)
	if !vm.basicOpWasDefined(bytecode.OpAdd, one) {
		t.Error("Integer#+ was defined at snapshot time; basicOpWasDefined said no")
	}
	if vm.basicOpWasDefined(bytecode.OpAdd, object.SymVal("s")) {
		t.Error("Symbol has no #+ in the snapshot; basicOpWasDefined said it had")
	}
	// A subclass instance answers through its ancestor's record.
	sub := newClass("IntSub", vm.cInteger)
	inst := &RObject{class: sub, ivars: map[string]object.Value{}}
	if !vm.basicOpWasDefined(bytecode.OpAdd, inst) {
		t.Error("a subclass of Integer must inherit the record for #+")
	}
	// And a class in no watched chain answers false rather than guessing.
	bare := &RObject{class: newClass("Bare", vm.cBasicObject), ivars: map[string]object.Value{}}
	if vm.basicOpWasDefined(bytecode.OpAdd, bare) {
		t.Error("a class outside every watched chain must answer false")
	}
	// Before the snapshot nothing can have been redefined, so it answers false.
	fresh := &VM{}
	if fresh.basicOpWasDefined(bytecode.OpAdd, one) {
		t.Error("with no snapshot taken, basicOpWasDefined must answer false")
	}
}

// TestInstallOperatorMethodKeepsAnExistingOne: the installer must never replace
// an operator a registration site already defined -- a site that chose a body
// wins over this fallback. Both arms are exercised: the install, and the no-op.
func TestInstallOperatorMethodKeepsAnExistingOne(t *testing.T) {
	vm := New(io.Discard)
	marker := object.SymVal("original")

	// Arm 1: the name is free, so it installs and the body is the opcode's.
	fresh := newClass("Fresh", vm.cObject)
	installOperatorMethod(fresh, bytecode.OpAdd)
	m := fresh.methods["+"]
	if m == nil {
		t.Fatal("installOperatorMethod did not install #+ on a class without one")
	}
	if !m.argc.declared {
		t.Error("the installed method declares no arity; MRI's is 1")
	}
	if got := vm.invoke(m, object.IntValue(2), []object.Value{object.IntValue(3)}, nil); got != object.IntValue(5) {
		t.Errorf("installed #+ computed %v, want 5", got)
	}

	// Arm 2: the name is taken, so it is left alone.
	taken := newClass("Taken", vm.cObject)
	taken.defineArgc("+", 1, func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return marker
	})
	before := taken.methods["+"]
	installOperatorMethod(taken, bytecode.OpAdd)
	if taken.methods["+"] != before {
		t.Error("installOperatorMethod replaced a method a registration site had defined")
	}
	if got := vm.invoke(taken.methods["+"], object.NilV, []object.Value{object.IntValue(1)}, nil); got != marker {
		t.Errorf("the original body no longer runs: got %v", got)
	}
}

// TestBasicOpClassesHoldsNoNil: the nil filter is in basicOpClasses, so the
// snapshot loop needs no per-class guard. If a class ever is nil -- a
// build-tagged registration skipped -- it is dropped here and stays unwatched,
// which keeps its operators on the inline path rather than reading "no record"
// as "redefined".
func TestBasicOpClassesHoldsNoNil(t *testing.T) {
	vm := New(io.Discard)
	got := vm.basicOpClasses()
	if len(got) == 0 {
		t.Fatal("no basic operator classes at all")
	}
	for i, c := range got {
		if c == nil {
			t.Errorf("basicOpClasses()[%d] is nil; the filter did not run", i)
		}
	}
	// And a VM with a nil field drops it rather than returning it.
	partial := &VM{cInteger: vm.cInteger}
	for i, c := range partial.basicOpClasses() {
		if c == nil {
			t.Errorf("basicOpClasses()[%d] is nil on a partially built VM", i)
		}
	}
	if n := len(partial.basicOpClasses()); n != 1 {
		t.Errorf("a VM with one class set returned %d, want 1", n)
	}
}

// TestResolveBasicOpOverrideIgnoresANonOperator is the slow path's own guard: an
// opcode with no operator name has no override to find.
func TestResolveBasicOpOverrideIgnoresANonOperator(t *testing.T) {
	vm := New(io.Discard)
	if m := vm.resolveBasicOpOverride(bytecode.OpDup, object.IntValue(1), vm.cInteger); m != nil {
		t.Errorf("resolveBasicOpOverride(OpDup) = %v, want nil", m.name)
	}
}
