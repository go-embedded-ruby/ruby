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

// BenchmarkSmallIntLoop is arithmetic-bound on small integers (counters and a
// modulo that stay within the interned range). It pins the small-integer cache
// win: with interning the inner loop allocates nothing; without it, every `+`/`%`
// boxes a fresh Integer (~250k allocs here).
func BenchmarkSmallIntLoop(b *testing.B) {
	benchProgram(b, "r = 0\no = 0\nwhile o < 1000\n  i = 0\n  while i < 500\n    i += 1\n    r = i % 7\n  end\n  o += 1\nend")
}

func BenchmarkBlockEach(b *testing.B) {
	benchProgram(b, "t = 0\n100000.times { |i| t += i }")
}

// BenchmarkDispatchMethods mirrors bench/dispatch.rb: a tight loop of
// monomorphic method calls into a tiny object. Each bump/total call is a leaf
// frame — no block, no ensure/rescue — so it is exactly the kind of frame that
// no longer allocates a returnTarget identity token at entry.
// BenchmarkTimesSmall exercises Integer#times over a range whose indices all
// fall inside the interned small-integer window (-256..1024). The block simply
// touches its index, so the only per-iteration boxing is the loop index the
// times builtin hands to the block. Routing that box through object.IntValue
// means every index reuses an interned box, so this benchmark allocates nothing
// on the index path.
func BenchmarkTimesSmall(b *testing.B) {
	benchProgram(b, "1000.times { |i| i }")
}

// BenchmarkTimesLarge is the same loop but wide enough that most indices fall
// outside the interned window, so each still heap-boxes a fresh Integer. It is
// the counterpoint to BenchmarkTimesSmall: funnelling boxing through
// object.IntValue does not change the large-range cost — collapsing that
// allocation is the job of a future immediate-Value representation, which this
// change is the localization prep for.
func BenchmarkTimesLarge(b *testing.B) {
	benchProgram(b, "5000.times { |i| i }")
}

func BenchmarkDispatchMethods(b *testing.B) {
	benchProgram(b, "class Counter\n  def initialize; @n = 0; end\n  def bump(k); @n += k; end\n  def total; @n; end\nend\nc = Counter.new\ni = 0\nwhile i < 100000\n  c.bump(2)\n  c.bump(1)\n  i += 1\nend\nc.total")
}

// BenchmarkProcCall mirrors bench/proc.rb: repeatedly invoking a captured lambda.
// The lambda body is a return-target frame with no block of its own, so under
// lazy allocation it never materialises a token either.
func BenchmarkProcCall(b *testing.B) {
	benchProgram(b, "adder = ->(a, b) { a + b }\nacc = 0\ni = 0\nwhile i < 100000\n  acc = adder.call(acc, i)\n  i += 1\nend\nacc")
}

// BenchmarkClassMethodSend exercises the general-send fallback (site #2): a
// class receiver bypasses the monomorphic inline cache and dispatches through
// vm.send. Each call passes two args from the operand stack, which used to be
// copied into a fresh slice per call and is now passed in place (send routes the
// ISeq callee straight into exec, which copies into env slots itself).
func BenchmarkClassMethodSend(b *testing.B) {
	benchProgram(b, "class M\n  def self.add(a, b); a + b; end\nend\nacc = 0\ni = 0\nwhile i < 100000\n  acc = M.add(acc, i)\n  i += 1\nend\nacc")
}

func BenchmarkArrayMap(b *testing.B) {
	benchProgram(b, "(1..10000).map { |x| x * 2 }.sum")
}

func BenchmarkStringConcat(b *testing.B) {
	benchProgram(b, "s = \"\"\n10000.times { s << \"x\" }")
}

// BenchmarkInlineRegexpMatch is the focused micro-bench: a non-interpolated
// literal /…/ used with =~ inside a hot loop. Before per-occurrence caching each
// iteration recompiled the pattern through go-ruby-regexp; after, the literal
// compiles once and the loop just matches.
func BenchmarkInlineRegexpMatch(b *testing.B) {
	benchProgram(b, "s = \"the quick brown fox 123 over 42 lazy dogs\"\nc = 0\n1000.times do\n  c += 1 if /\\w+/ =~ s\n  c += 1 if /\\d+/ =~ s\nend")
}

// BenchmarkInlineRegexpScan mirrors the strscan/tokenise workload: several inline
// literals driving String#scan in a loop (the idiomatic "regexp literal in a hot
// loop" the fix targets). It is the intra-rbgo speedup the fix delivers.
func BenchmarkInlineRegexpScan(b *testing.B) {
	benchProgram(b, "src = \"name = value; count = 42; flag = true; ratio = 3.14;\" * 2\nacc = 0\n200.times do\n  acc += src.scan(/[A-Za-z_]\\w*/).length\n  acc += src.scan(/\\d+(?:\\.\\d+)?/).length\n  acc += src.scan(/\\s+/).length\nend")
}
