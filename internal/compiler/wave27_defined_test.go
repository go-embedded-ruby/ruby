// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package compiler

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// countOp reports how many times op appears in iseq's instructions.
func countOp(iseq *bytecode.ISeq, op bytecode.Op) int {
	n := 0
	for _, in := range iseq.Insns {
		if in.Op == op {
			n++
		}
	}
	return n
}

// definedTagConsts collects the String constants of an ISeq's pool, which for a
// `defined?` lowering are exactly the tags the compiler decided statically.
func definedTagConsts(iseq *bytecode.ISeq) []*object.String {
	var out []*object.String
	for _, c := range iseq.Consts {
		if s, ok := c.(*object.String); ok {
			out = append(out, s)
		}
	}
	return out
}

// TestDefinedTagsAreFrozenConstants covers pushDefinedTag: every tag the
// compiler bakes into the literal pool is built by bytecode.DefinedTag and is
// therefore FROZEN, as MRI's rb_iseq_defined_string (an fstring) is.
func TestDefinedTagsAreFrozenConstants(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`defined?(nil)`, `defined?(true)`, `defined?(false)`, `defined?(self)`,
		`defined?(1)`, `defined?(x = 1)`, `x = 1; defined?(x)`,
		`defined?(__FILE__)`, `defined?(__LINE__)`, `defined?(__ENCODING__)`,
		`defined?(defined?(y))`, `defined?([])`, `defined?([1, 2])`,
		`defined?({1 => 2})`,
	} {
		iseq := compileSrc(t, src)
		tags := definedTagConsts(iseq)
		if len(tags) == 0 {
			t.Fatalf("%s: no String constant in the pool, expected a defined? tag", src)
		}
		for _, s := range tags {
			if !s.Frozen {
				t.Errorf("%s: tag %q is not frozen", src, s.Str())
			}
		}
	}
}

// TestDefinedSuperEmitsItsOwnOpcode covers the *ast.Super arm of compileDefined:
// `defined?(super)` is DEFINED_ZSUPER, an opcode of its own, and it must NOT
// evaluate the super arguments — so no send for them is emitted either.
func TestDefinedSuperEmitsItsOwnOpcode(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`def m; defined?(super); end`,
		`def m(a); defined?(super(a)); end`,
		`def m(a); defined?(super(raise("never"))); end`,
	} {
		iseq := compileSrc(t, src)
		if len(iseq.Children) != 1 {
			t.Fatalf("%s: want one child ISeq, got %d", src, len(iseq.Children))
		}
		body := iseq.Children[0]
		if got := countOp(body, bytecode.OpDefinedSuper); got != 1 {
			t.Errorf("%s: OpDefinedSuper count = %d, want 1", src, got)
		}
		if got := countOp(body, bytecode.OpSend); got != 0 {
			t.Errorf("%s: emitted %d sends, want 0 (the super arguments are never evaluated)", src, got)
		}
	}
}

// TestDefinedContainerProbesEachElement covers compileDefinedElements: a
// non-empty container probes every element and branches away on the first
// undefined one, while an EMPTY one has nothing to probe and is a bare tag push.
func TestDefinedContainerProbesEachElement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src        string
		wantBranch int
	}{
		{`defined?([])`, 0},
		{`defined?({})`, 0},
		{`defined?([A])`, 1},
		{`defined?([A, B, C])`, 3},
		{`defined?({A => B})`, 2}, // a hash probes keys AND values
		// The array probes its one element, and that element — a splat — probes
		// the splatted expression in turn: two nested probes.
		{`defined?([*A])`, 2},
	} {
		iseq := compileSrc(t, tc.src)
		if got := countOp(iseq, bytecode.OpBranchNil); got != tc.wantBranch {
			t.Errorf("%s: OpBranchNil count = %d, want %d", tc.src, got, tc.wantBranch)
		}
	}
}

// TestDefinedSourceKeywordsAreNotMethodProbes covers isSourceKeyword and its
// call site: the three source pseudo-variables answer "expression" with a plain
// constant push, while a bare name that is neither a local nor one of them is a
// method probe (a guard child ISeq).
func TestDefinedSourceKeywordsAreNotMethodProbes(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"__FILE__", "__LINE__", "__ENCODING__"} {
		iseq := compileSrc(t, `defined?(`+name+`)`)
		if got := countOp(iseq, bytecode.OpDefinedGuard); got != 0 {
			t.Errorf("defined?(%s): emitted a method-probe guard", name)
		}
	}
	// A near-miss name (not one of the three) still probes for a method.
	iseq := compileSrc(t, `defined?(__method__)`)
	if got := countOp(iseq, bytecode.OpDefinedGuard); got != 1 {
		t.Errorf("defined?(__method__): OpDefinedGuard count = %d, want 1", got)
	}
	if isSourceKeyword("__method__") {
		t.Error(`isSourceKeyword("__method__") = true, want false`)
	}
}

// TestDefinedMethodProbeRecordsTheReceiverKind covers the B operand of
// OpDefinedMethod: 0 for an implicit receiver (MRI's DEFINED_FUNC, any
// visibility) and 1 for a written one (DEFINED_METHOD, visibility screened).
func TestDefinedMethodProbeRecordsTheReceiverKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		want int
	}{
		{`defined?(puts)`, 0},
		{`defined?(nil.foo)`, 1},
		{`defined?(self.foo)`, 1},
	} {
		iseq := compileSrc(t, tc.src)
		if len(iseq.Children) != 1 {
			t.Fatalf("%s: want one guard child, got %d", tc.src, len(iseq.Children))
		}
		found := false
		for _, in := range iseq.Children[0].Insns {
			if in.Op != bytecode.OpDefinedMethod {
				continue
			}
			found = true
			if in.B != tc.want {
				t.Errorf("%s: OpDefinedMethod B = %d, want %d", tc.src, in.B, tc.want)
			}
		}
		if !found {
			t.Errorf("%s: no OpDefinedMethod emitted", tc.src)
		}
	}
}

// TestDefinedInVoidContextEmitsNothing covers compileDiscarded: MRI compiles
// NODE_DEFINED to nothing when its value is popped, so the operand's receiver is
// never run — but the locals it assigns are still declared.
func TestDefinedInVoidContextEmitsNothing(t *testing.T) {
	t.Parallel()
	kept := compileSrc(t, `defined?(foo.bar); 1`)
	if got := countOp(kept, bytecode.OpDefinedGuard); got != 0 {
		t.Errorf("a discarded defined? emitted %d guards, want 0", got)
	}
	if len(kept.Children) != 0 {
		t.Errorf("a discarded defined? built %d child ISeqs, want 0", len(kept.Children))
	}
	// The value-returning form in the same position still compiles.
	used := compileSrc(t, `x = defined?(foo.bar); 1`)
	if got := countOp(used, bytecode.OpDefinedGuard); got != 1 {
		t.Errorf("a used defined? emitted %d guards, want 1", got)
	}
	// A discarded defined? still declares the operand's locals.
	decl := compileSrc(t, "defined?(zz = 1)\np 2")
	if !hasLocal(decl, "zz") {
		t.Error("a discarded defined?(zz = 1) did not declare zz")
	}
	// A non-defined? statement in a discarded position is untouched.
	other := compileSrc(t, `foo; 1`)
	if countOp(other, bytecode.OpSend) == 0 {
		t.Error("a discarded ordinary call was elided")
	}
}

// TestDefinedDeclaresLocalsInsideContainers covers the container arms of
// declareDefinedLocals: `defined?` never evaluates its operand, but the
// assignments inside one still take a local slot, as they do in MRI's parser.
func TestDefinedDeclaresLocalsInsideContainers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, name string }{
		{"defined?([a = 1])\np 0", "a"},
		{"defined?({(b = 1) => 2})\np 0", "b"},
		{"defined?({1 => (c = 2)})\np 0", "c"},
		{"defined?([*(d = [1])])\np 0", "d"},
		{"defined?(defined?(e = 1))\np 0", "e"},
	} {
		iseq := compileSrc(t, tc.src)
		if !hasLocal(iseq, tc.name) {
			t.Errorf("%s: local %q not declared (locals %v)", tc.src, tc.name, iseq.Locals)
		}
	}
}
