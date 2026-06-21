package vm_test

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

// benchProgram compiles src once and runs it b.N times on a reused VM, so the
// measurement reflects the execution loop rather than parse/compile/prelude
// setup.
func benchProgram(b *testing.B, src string) {
	prog, err := parser.Parse(src)
	if err != nil {
		b.Fatal(err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		b.Fatal(err)
	}
	m := vm.New(io.Discard)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Run(iseq); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFib(b *testing.B) {
	benchProgram(b, "def fib(n) = n < 2 ? n : fib(n - 1) + fib(n - 2)\nfib(25)")
}

func BenchmarkLoopSum(b *testing.B) {
	benchProgram(b, "s = 0\ni = 0\nwhile i < 300000\n  s += i\n  i += 1\nend")
}

func BenchmarkBlockEach(b *testing.B) {
	benchProgram(b, "t = 0\n100000.times { |i| t += i }")
}

func BenchmarkArrayMap(b *testing.B) {
	benchProgram(b, "(1..10000).map { |x| x * 2 }.sum")
}

func BenchmarkStringConcat(b *testing.B) {
	benchProgram(b, "s = \"\"\n10000.times { s << \"x\" }")
}
