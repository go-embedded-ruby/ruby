package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// AOT-compiler feasibility prototype. It hand-writes the Go code that a build-
// time compiler would emit for `def fib(n) = n < 2 ? n : fib(n-1) + fib(n-2)`,
// at two specialisation levels, and benchmarks them against the bytecode
// interpreter. The question: can compiled-to-Go Ruby match MRI's interpreter?

// fibL1 is the "sound, unspecialised" form: straight-line Go control flow (no
// bytecode dispatch loop, no operand stack, locals as Go variables) but every
// operator still goes through vm.binaryOp, so semantics match the interpreter
// exactly (a redefined Integer#+ would be honoured identically).
func (vm *VM) fibL1(n object.Value) object.Value {
	if vm.binaryOp(bytecode.OpLt, n, object.Integer(2)).Truthy() {
		return n
	}
	a := vm.fibL1(vm.binaryOp(bytecode.OpSub, n, object.Integer(1)))
	b := vm.fibL1(vm.binaryOp(bytecode.OpSub, n, object.Integer(2)))
	return vm.binaryOp(bytecode.OpAdd, a, b)
}

// fibL2 is the "type-specialised + guarded" form, like YJIT's specialisation: a
// type guard on the receiver, inline integer arithmetic on the fast path, and a
// deopt fall-back (here to fibL1) for the dynamic case. Overflow promotion is
// elided for the prototype (fib(34) fits in int64).
func (vm *VM) fibL2(n object.Value) object.Value {
	ni, ok := object.AsIntegerOK(n)
	if !ok {
		return vm.fibL1(n) // deopt
	}
	if ni < 2 {
		return ni
	}
	a := object.AsInteger(vm.fibL2(ni - 1))
	b := object.AsInteger(vm.fibL2(ni - 2))
	return a + b
}

// fibL3 is the "fully monomorphised" form a whole-method type-inference pass
// enables: once the entry guard proves the receiver is an Integer and the
// operators are not redefined, the entire recursive region runs on unboxed
// int64 — boxing only at the method boundary. This is what a runtime JIT cannot
// match: a JIT specialises a region at a time with runtime budget, while an AOT
// compiler proves types over the whole call graph at build time and hands the
// result to Go's optimiser (inlining, registers, SSA, and PGO).
func fibUnboxed(n int64) int64 {
	if n < 2 {
		return n
	}
	return fibUnboxed(n-1) + fibUnboxed(n-2)
}

// fibL3 is the boundary: guard + box at the edge, unboxed interior.
func (vm *VM) fibL3(n object.Value) object.Value {
	ni, ok := object.AsIntegerOK(n)
	if !ok {
		return vm.fibL1(n) // deopt
	}
	return object.Integer(fibUnboxed(int64(ni)))
}

const fibN = 30

func BenchmarkAOTInterpreted(b *testing.B) {
	src := "def fib(n) = n < 2 ? n : fib(n - 1) + fib(n - 2)\nfib(30)"
	iseq := mustCompile(b, src)
	m := New(io.Discard)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Run(iseq); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAOTLevel1(b *testing.B) {
	m := New(io.Discard)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.fibL1(object.Integer(fibN))
	}
}

func BenchmarkAOTLevel2(b *testing.B) {
	m := New(io.Discard)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.fibL2(object.Integer(fibN))
	}
}

func BenchmarkAOTLevel3(b *testing.B) {
	m := New(io.Discard)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.fibL3(object.Integer(fibN))
	}
}

func mustCompile(b *testing.B, src string) *bytecode.ISeq {
	b.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		b.Fatal(err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		b.Fatal(err)
	}
	return iseq
}
