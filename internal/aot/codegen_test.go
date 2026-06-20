package aot

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	rparser "github.com/go-embedded-ruby/ruby/internal/parser"
)

// methodISeq compiles `src` and returns its first method/block child ISeq.
func methodISeq(t *testing.T, src string) *bytecode.ISeq {
	t.Helper()
	prog, err := rparser.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	if len(iseq.Children) == 0 {
		t.Fatalf("%q has no method child", src)
	}
	return iseq.Children[0]
}

// assertGoParses checks the emitted function is syntactically valid Go (the
// identifiers vm/object/bytecode need not resolve — only the syntax matters).
func assertGoParses(t *testing.T, src string) {
	t.Helper()
	file := "package p\nfunc (vm *VM) f() {}\n" // placeholder so the unit is a file
	_ = file
	wrapped := "package p\n" + src
	if _, err := parser.ParseFile(token.NewFileSet(), "gen.go", wrapped, 0); err != nil {
		t.Fatalf("generated Go does not parse: %v\n%s", err, src)
	}
}

// TestCompileSupported exercises every lowerable opcode/constant through real
// Ruby methods, asserting Compile succeeds and emits syntactically valid Go.
func TestCompileSupported(t *testing.T) {
	cases := []struct{ name, src, ruby, contains string }{
		{"fib", "def fib(n) = n < 2 ? n : fib(n - 1) + fib(n - 2)\nfib(5)", "fib", "vm.f(self,"},
		{"arith", "def m(a, b) = a * b + a / b - a % b\nm(1, 2)", "m", "OpMul"},
		{"cmp", "def m(a, b) = a > b == (a <= b)\nm(1, 2)", "m", "OpGt"},
		{"cmp2", "def m(a, b) = (a >= b) != (a < b)\nm(1, 2)", "m", "OpGe"},
		{"not", "def m(a) = !a\nm(1)", "m", "!s0.Truthy()"},
		{"neg", "def m(a) = -a\nm(1)", "m", "negate("},
		{"or", "def m(a) = a || 1\nm(1)", "m", "s1.Truthy() { goto"},
		{"and", "def m(a) = a && 1\nm(1)", "m", "!s1.Truthy() { goto"},
		{"safenav", "def m(a) = a&.foo\nm(1)", "m", "isNil"},
		{"setlocal", "def m(a)\n  x = a\n  x\nend\nm(1)", "m", "l1 = s0"},
		{"send_other", "def m(a) = a.foo\nm(1)", "m", "dispatchSend"},
		{"nilv", "def m = nil\nm", "m", "object.NilV"},
		{"truev", "def m = true\nm", "m", "object.True"},
		{"falsev", "def m = false\nm", "m", "object.False"},
		{"floatc", "def m = 1.5\nm", "m", "object.Float(1.5)"},
		{"stringc", "def m = \"hi\"\nm", "m", "object.NewString"},
		{"symbolc", "def m = :sym\nm", "m", "object.Symbol"},
		{"while", "def m(a)\n  while a > 0\n    a -= 1\n  end\n  a\nend\nm(3)", "m", "goto L"},
		{"array", "def m(a, b, c) = [a, b, c]\nm(1, 2, 3)", "m", "object.Array{Elems"},
		{"empty_array", "def m = []\nm", "m", "object.Array{Elems: []object.Value{}}"},
		{"hash", "def m(a, b) = {x: a, y: b}\nm(1, 2)", "m", "h.Set("},
		{"empty_hash", "def m = {}\nm", "m", "object.NewHash()"},
		{"range", "def m(a) = (1..a)\nm(5)", "m", "object.Range{Lo"},
		{"range_excl", "def m(a) = (1...a)\nm(5)", "m", "Exclusive: true"},
		{"getivar", "def m = @x\nm", "m", "getIvar(self,"},
		{"setivar", "def m(a)\n  @x = a\nend\nm(1)", "m", "setIvar(self,"},
		{"getconst", "def m = Foo\nm", "m", "vm.aotConst("},
		{"setconst", "def m\n  X = 1\n  X\nend\nm", "m", "vm.consts["},
		{"getgvar", "def m = $g\nm", "m", "vm.gvar("},
		{"splat_concat", "def m(a, b) = [a, *[b, b]]\nm(1, 2)", "m", "aotConcat("},
		{"splat", "def m(a, b) = [a, *[b, b]]\nm(1, 2)", "m", "aotSplat("},
		{"regexp", "def m = /ab/\nm", "m", "compileRegexp("},
		{"block_given", "def m = block_given?\nm", "m", "object.Bool(block != nil)"},
		{"invoke_block", "def m\n  yield 1\nend\nm", "m", "aotYield(block,"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, ok := Compile(methodISeq(t, c.src), "f", c.ruby)
			if !ok {
				t.Fatalf("Compile(%s) bailed unexpectedly", c.name)
			}
			if !strings.Contains(src, c.contains) {
				t.Errorf("generated code missing %q:\n%s", c.contains, src)
			}
			assertGoParses(t, src)
		})
	}
}

// TestCompileSelfRecursionDirect checks a self-send to the compiled method
// becomes a direct call, but a self-send to a *different* name dispatches.
func TestCompileSelfRecursionDirect(t *testing.T) {
	src, ok := Compile(methodISeq(t, "def fib(n) = n < 2 ? n : fib(n - 1) + fib(n - 2)\nfib(5)"), "aotFib", "fib")
	if !ok {
		t.Fatal("bailed")
	}
	if !strings.Contains(src, "vm.aotFib(self,") {
		t.Errorf("self-recursion not lowered to a direct call:\n%s", src)
	}
	// Compiled under a different ruby name → the self-send falls back to dispatch.
	src2, _ := Compile(methodISeq(t, "def fib(n) = n < 2 ? n : fib(n - 1) + fib(n - 2)\nfib(5)"), "aotG", "other")
	if strings.Contains(src2, "vm.aotG(self,") || !strings.Contains(src2, `dispatchSend(s0, "fib"`) {
		t.Errorf("non-matching self-send should dispatch:\n%s", src2)
	}
}

// TestCompileBails covers every reason this stage declines to lower a method.
func TestCompileBails(t *testing.T) {
	rubyBails := []struct{ name, src string }{
		{"splat", "def m(*a) = a\nm"},
		{"keyword", "def m(a:) = a\nm(a: 1)"},
		{"block_param", "def m(&b) = b\nm"},
		{"optional_param", "def m(a = 5) = a\nm"}, // optional positionals need default-value machinery
		{"send_with_block", "def m(a) = a.each { |x| x }\nm(1)"},
		{"bignum_const", "def m = 99999999999999999999999\nm"},
		{"unsupported_opcode", "def m\n  a, b = 1, 2\n  a\nend\nm"}, // OpExpandArray
		// case/in emits OpTruthy (covered before the bail) then an unsupported
		// OpRaiseNoMatch.
		{"case_in", "def m(x)\n  case x\n  in 1 then 1\n  end\nend\nm(1)"},
	}
	for _, c := range rubyBails {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := Compile(methodISeq(t, c.src), "f", "m"); ok {
				t.Errorf("expected Compile(%s) to bail", c.name)
			}
		})
	}

	// Synthetic ISeqs for the structural bail conditions. SplatIndex/KwRestSlot/
	// BlockSlot are -1 (as the real compiler emits) so they clear the param check
	// and reach the condition under test.
	mk := func(insns ...bytecode.Instr) *bytecode.ISeq {
		return &bytecode.ISeq{SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1, NumLocals: 1, Insns: insns}
	}
	t.Run("empty", func(t *testing.T) {
		if _, ok := Compile(mk(), "f", "m"); ok {
			t.Error("empty iseq should bail")
		}
	})
	t.Run("not_return_terminated", func(t *testing.T) {
		if _, ok := Compile(mk(bytecode.Instr{Op: bytecode.OpPushNil}), "f", "m"); ok {
			t.Error("non-return-terminated iseq should bail")
		}
	})
	t.Run("closure_get_local", func(t *testing.T) {
		if _, ok := Compile(mk(
			bytecode.Instr{Op: bytecode.OpGetLocal, A: 0, B: 1}, bytecode.Instr{Op: bytecode.OpReturn},
		), "f", "m"); ok {
			t.Error("outer-scope get_local should bail")
		}
	})
	t.Run("closure_set_local", func(t *testing.T) {
		if _, ok := Compile(mk(
			bytecode.Instr{Op: bytecode.OpPushNil},
			bytecode.Instr{Op: bytecode.OpSetLocal, A: 0, B: 1},
			bytecode.Instr{Op: bytecode.OpReturn},
		), "f", "m"); ok {
			t.Error("outer-scope set_local should bail")
		}
	})
	t.Run("unreachable_dead_code", func(t *testing.T) {
		// pc 2..3 are unreachable (nothing falls into them after the return).
		if _, ok := Compile(mk(
			bytecode.Instr{Op: bytecode.OpPushNil}, bytecode.Instr{Op: bytecode.OpReturn},
			bytecode.Instr{Op: bytecode.OpPushNil}, bytecode.Instr{Op: bytecode.OpReturn},
		), "f", "m"); ok {
			t.Error("iseq with dead code should bail")
		}
	})
}

// TestOpNameDefault covers the defensive default of opName (emit only ever calls
// it with arithmetic/comparison opcodes, so a unit test reaches the fallthrough).
func TestOpNameDefault(t *testing.T) {
	if opName(bytecode.OpNop) != "" {
		t.Error("opName(non-arith) should be empty")
	}
}
