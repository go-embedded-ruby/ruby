package vm_test

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

// benchIvarProgram is benchProgram, kept here so these benchmarks can be run
// against a tree that does not have them yet (the before/after comparison for
// #672 copies this file onto the earlier revision).
func benchIvarProgram(b *testing.B, src string) {
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

// The ivar read/write path of a plain object: the hot case, and the one every
// Ruby program pays. It goes through setIvar/getIvar, which #672 rerouted through
// ivarStoreOf — the object's own map is still found by the same type switch, one
// arm in.
func BenchmarkIvarObjectAccessors(b *testing.B) {
	benchIvarProgram(b, `
class C
  def initialize; @a = 0; @b = 0; end
  def bump; @a += 1; @b = @a; end
  def read; @a + @b; end
end
c = C.new
i = 0
while i < 20000
  c.bump
  c.read
  i += 1
end
`)
}

// Allocating objects that take ivars: what a per-object ivar field or an eager
// table would have made more expensive.
func BenchmarkIvarObjectAlloc(b *testing.B) {
	benchIvarProgram(b, `
class P
  def initialize(x, y); @x = x; @y = y; end
  def sum; @x + @y; end
end
i = 0
t = 0
while i < 20000
  t += P.new(i, i).sum
  i += 1
end
`)
}

// A String taking an instance variable is the case #672 made work at all: it
// costs one lookup in the VM-wide generic table, and the table entry is created
// by the first write.
func BenchmarkIvarOnString(b *testing.B) {
	benchIvarProgram(b, `
i = 0
while i < 5000
  s = "carrier"
  s.instance_variable_set(:@tag, i)
  s.instance_variable_get(:@tag)
  i += 1
end
`)
}

// Strings that never take an ivar, which is nearly all of them: the point of a
// lazily-created generic table is that this path is untouched — no field, no
// allocation, no map entry.
func BenchmarkStringsWithoutIvars(b *testing.B) {
	benchIvarProgram(b, `
i = 0
n = 0
while i < 20000
  s = "a string " + i.to_s
  n += s.length
  i += 1
end
`)
}
