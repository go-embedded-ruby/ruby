package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

func wantRaise(t *testing.T, class string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected a %s panic, got none", class)
		}
		re, ok := r.(RubyError)
		if !ok || re.Class != class {
			t.Fatalf("expected RubyError %s, got %#v", class, r)
		}
	}()
	fn()
}

// The operator helpers' "impossible operator" defaults are defensive; exercise
// them directly so the package can hold 100% coverage.
func TestOperatorDefaults(t *testing.T) {
	wantRaise(t, "VMError", func() { intOp(bytecode.OpNop, 1, 1) })
	wantRaise(t, "VMError", func() { floatOp(bytecode.OpNop, 1, 1) })
	wantRaise(t, "NoMethodError", func() { stringOp(bytecode.OpNop, "a", object.NilV) })
	wantRaise(t, "NoMethodError", func() { negate(object.True) })
	wantRaise(t, "TypeError", func() { binary(bytecode.OpAdd, object.True, object.True) })
	wantRaise(t, "ZeroDivisionError", func() { intOp(bytecode.OpMod, 1, 0) })
}

func TestValueEqualBranches(t *testing.T) {
	cases := []struct {
		a, b object.Value
		want bool
	}{
		{object.Integer(2), object.Integer(2), true},
		{object.Integer(2), object.Float(2), true},
		{object.Integer(2), object.String("x"), false},
		{object.Float(2), object.Integer(2), true},
		{object.Float(2), object.String("x"), false},
		{object.String("a"), object.String("a"), true},
		{object.String("a"), object.Integer(1), false},
		{object.Bool(true), object.Bool(true), true},
		{object.Bool(true), object.Integer(1), false},
		{object.NilV, object.NilV, true},
		{object.NilV, object.Integer(1), false},
		{object.NewMain(), object.Integer(1), false},
	}
	for _, c := range cases {
		if got := valueEqual(c.a, c.b); got != c.want {
			t.Errorf("valueEqual(%v,%v)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

// foreignValue is an object.Value the VM's classOf does not know about, used to
// exercise the defensive default branch.
type foreignValue struct{}

func (foreignValue) ToS() string     { return "" }
func (foreignValue) Inspect() string { return "" }
func (foreignValue) Truthy() bool    { return true }

func TestClassOfUnknown(t *testing.T) {
	vm := New(io.Discard)
	if c := vm.classOf(foreignValue{}); c != nil {
		t.Fatalf("classOf(unknown) = %v, want nil", c)
	}
}

func TestFloatModSign(t *testing.T) {
	if got := floatMod(-7.5, 2.0); got != 0.5 {
		t.Errorf("floatMod(-7.5,2.0)=%v want 0.5", got)
	}
}

// Exercises opcodes the compiler does not currently emit (nop, dup, push_self,
// branch_if) plus the unknown-opcode guard, via hand-built ISeqs.
func TestExecCraftedOpcodes(t *testing.T) {
	vm := New(io.Discard)
	iseq := &bytecode.ISeq{
		SplatIndex: -1,
		Consts:     []object.Value{object.Integer(7)},
		Insns: []bytecode.Instr{
			{Op: bytecode.OpNop},
			{Op: bytecode.OpPushSelf},
			{Op: bytecode.OpPop},
			{Op: bytecode.OpPushConst, A: 0},
			{Op: bytecode.OpDup},
			{Op: bytecode.OpPop},
			{Op: bytecode.OpPushTrue},
			{Op: bytecode.OpBranchIf, A: 9},
			{Op: bytecode.OpPushConst, A: 0}, // skipped
			{Op: bytecode.OpReturn},
		},
	}
	got, err := vm.Run(iseq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != object.Integer(7) {
		t.Fatalf("got %v want 7", got)
	}
}

// An ISeq that runs off the end without an explicit return yields nil.
func TestExecFallsOffEnd(t *testing.T) {
	vm := New(io.Discard)
	got, err := vm.Run(&bytecode.ISeq{SplatIndex: -1, Insns: []bytecode.Instr{{Op: bytecode.OpNop}}})
	if err != nil || got != object.NilV {
		t.Fatalf("got (%v,%v) want (nil,<nil>)", got, err)
	}
}

func TestExecUnknownOpcode(t *testing.T) {
	vm := New(io.Discard)
	_, err := vm.Run(&bytecode.ISeq{SplatIndex: -1, Insns: []bytecode.Instr{{Op: bytecode.Op(254)}}})
	if err == nil || err.(RubyError).Class != "VMError" {
		t.Fatalf("expected VMError, got %v", err)
	}
}

// A non-RubyError panic inside exec (here an out-of-range local) must propagate
// out of Run rather than be swallowed.
func TestRunPropagatesInternalPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected the internal panic to propagate")
		}
	}()
	vm := New(io.Discard)
	vm.Run(&bytecode.ISeq{SplatIndex: -1, Insns: []bytecode.Instr{{Op: bytecode.OpGetLocal, A: 5}}})
}
