package aot

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
)

// mainSrc compiles a whole program and lowers its top-level ISeq, asserting the
// lowering succeeds and the emitted Go parses. It returns the generated source.
func mainSrc(t *testing.T, src string) string {
	t.Helper()
	fn, _, ok := CompileMain(topISeq(t, src))
	if !ok {
		t.Fatalf("CompileMain(%q) bailed, expected success", src)
	}
	assertGoParses(t, "func (vm *VM) VM_placeholder() {}\n"+fn)
	return fn
}

// TestCompileMainSupported exercises the lowerable top-level opcodes through real
// Ruby programs: each must lower and emit syntactically valid Go containing the
// probe string.
func TestCompileMainSupported(t *testing.T) {
	cases := []struct{ name, src, contains string }{
		{"literals", "a = nil; b = true; c = false; puts [a, b, c]", "object.NilV"},
		{"selftrue", "puts true", "object.True"},
		{"selffalse", "puts false", "object.False"},
		{"int", "puts 40 + 2", "object.IntValue(40)"},
		{"float", "puts 1.5", "object.Float(1.5)"},
		{"string", "puts \"hi\"", `object.NewString("hi")`},
		{"symbol", "puts :sym", `object.Symbol("sym")`},
		{"arith", "a=6;b=2;puts a*b+a/b-a%b", "bytecode.OpMul"},
		{"cmp", "a=1;b=2;puts((a<b)==(a>=b))", "bytecode.OpLt"},
		{"cmp2", "a=1;b=2;puts((a>b)!=(a<=b))", "bytecode.OpGt"},
		{"neg", "x=5; puts(-x)", "negate("},
		{"not", "puts(!true)", "object.Bool(!s0_1.Truthy())"},
		{"whileloop", "i=0; i+=1 while i<3; puts i", "goto L"},
		{"ifbranch", "x=1; puts(x>0 ? :a : :b)", "if !s0_1.Truthy() { goto"},
		{"or_dup", "a=nil; puts(a || 7)", "if s0_2.Truthy() { goto"},
		{"safenav", "a=nil; puts a&.foo", "isNil"},
		{"array", "puts [1, 2, 3].length", "&object.Array{Elems:"},
		{"hash", "h = {a: 1, b: 2}; puts h.size", "object.NewHash()"},
		{"range", "puts((1..5).to_a.length)", "object.Range{Lo:"},
		{"rangex", "puts((1...5).to_a.length)", "Exclusive: true"},
		{"ivar", "@x = 3; puts @x", `getIvar(self, "@x")`},
		{"setivar", "@x = 3; puts @x", `setIvar(self, "@x"`},
		{"setconst", "K = 9; puts K", `vm.consts["K"]`},
		{"getconst", "K = 9; puts K", `vm.aotConst("K")`},
		{"gvar", "puts $0", `vm.gvar("$0")`},
		{"send_noblock", "puts [3, 1, 2].sort.first", "vm.aotSend(&aotic"},
		{"send_block", "t=0; 3.times { |i| t += i }; puts t", "&Proc{native: func(vm *VM, bargs"},
		{"block_outerlocal", "t=0; 3.times { |i| t += i }; puts t", "l0_0"},
		{"block_autosplat", "[[1,2]].each { |a, b| puts a+b }", "aotBlockArgs(2,"},
		{"nested_blocks", "s=0; 2.times { (1..3).map { |x| x }.each { |y| s += y } }; puts s", "func(vm *VM, bargs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mainSrc(t, c.src)
			if !strings.Contains(got, c.contains) {
				t.Errorf("generated main missing %q:\n%s", c.contains, got)
			}
		})
	}
}

// TestCompileMainBails covers every reason CompileMain declines to lower a
// program (leaving it interpreted): real Ruby that reaches an unlowerable
// construct, plus synthetic ISeqs for the structural guards.
func TestCompileMainBails(t *testing.T) {
	rubyBails := []struct{ name, src string }{
		{"toplevel_def", "def f; 1; end\nf"},                 // OpDefineMethod
		{"toplevel_class", "class C; end"},                   // OpDefineClass
		{"bignum", "puts 99999999999999999999999"},           // constExpr bails on Bignum
		{"multi_assign", "a, b = 1, 2\nputs a"},              // OpExpandArray unsupported
		{"setgvar", "$x = 1\nputs $x"},                       // OpSetGVar unsupported
		{"block_splat_param", "[1].each { |*a| a }"},         // block splat param
		{"block_kw_param", "[1].each { |k:| k }"},            // block keyword param
		{"block_opt_param", "[1].each { |a = 1| a }"},        // block optional param → Params != NumRequired
		{"unsupported_in_block", "[1].each { |x| a,b = x }"}, // OpExpandArray inside a block
		// case/in emits OpTruthy (lowered, executed) then an unsupported
		// OpRaiseNoMatch (bails) — so the OpTruthy arm runs before the bail.
		{"case_in", "x = 1\ncase x\nin 1 then :a\nend"},
	}
	for _, c := range rubyBails {
		t.Run(c.name, func(t *testing.T) {
			if _, _, ok := CompileMain(topISeq(t, c.src)); ok {
				t.Errorf("expected CompileMain(%s) to bail", c.name)
			}
		})
	}

	// Synthetic ISeqs for the structural guards the real compiler never emits at
	// top level. -1 sentinels clear the param checks so each reaches its guard.
	mk := func(numLocals int, insns ...bytecode.Instr) *bytecode.ISeq {
		return &bytecode.ISeq{SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1, NumLocals: numLocals, Insns: insns}
	}
	structural := []struct {
		name string
		iseq *bytecode.ISeq
	}{
		{"empty", mk(0)},
		{"not_return_terminated", mk(0, bytecode.Instr{Op: bytecode.OpPushNil})},
		{"dead_code", mk(0,
			bytecode.Instr{Op: bytecode.OpPushNil}, bytecode.Instr{Op: bytecode.OpReturn},
			bytecode.Instr{Op: bytecode.OpPushNil}, bytecode.Instr{Op: bytecode.OpReturn})},
		{"get_local_no_parent", mk(1,
			bytecode.Instr{Op: bytecode.OpGetLocal, A: 0, B: 1}, bytecode.Instr{Op: bytecode.OpReturn})},
		{"set_local_no_parent", mk(1,
			bytecode.Instr{Op: bytecode.OpPushNil},
			bytecode.Instr{Op: bytecode.OpSetLocal, A: 0, B: 1},
			bytecode.Instr{Op: bytecode.OpReturn})},
		// OpReturn A=1 is a non-local block return. A real block always leaves the
		// implicit epilogue unreachable after it (bailing at the dead-code guard
		// first), so the A=1 arm is reached only through a synthetic scope where the
		// return is the last, reachable instruction.
		{"nonlocal_return", mk(0,
			bytecode.Instr{Op: bytecode.OpPushNil},
			bytecode.Instr{Op: bytecode.OpReturn, A: 1})},
	}
	for _, c := range structural {
		t.Run(c.name, func(t *testing.T) {
			if _, _, ok := CompileMain(c.iseq); ok {
				t.Errorf("expected CompileMain(%s) to bail", c.name)
			}
		})
	}
}

// TestCompileMainCaches: each lowered send site gets its own inline cache slot,
// counted by nCaches.
func TestCompileMainCaches(t *testing.T) {
	fn, n, ok := CompileMain(topISeq(t, "puts [2, 1].sort.first"))
	if !ok {
		t.Fatal("expected lowering")
	}
	// One inline-cache slot per non-block send site; each is referenced by name.
	if n != strings.Count(fn, "vm.aotSend(") {
		t.Errorf("nCaches = %d but %d aotSend sites", n, strings.Count(fn, "vm.aotSend("))
	}
	if n == 0 {
		t.Errorf("expected at least one send site")
	}
	if !strings.Contains(fn, "&aotic0,") {
		t.Errorf("first cache slot not referenced:\n%s", fn)
	}
}

// TestCompileProgramMain: CompileProgram also lowers a lowerable top level,
// weaving in the aotMain function, its inline-cache slot declarations, the
// RegisterCompiledMain registration, and a "<main>" key — independently of any
// per-method lowering.
func TestCompileProgramMain(t *testing.T) {
	// A block-/hash-heavy top level with no method definition: nothing for the
	// per-method pass, everything for CompileMain.
	content, keys, ok := CompileProgram(topISeq(t, "h = {}\n3.times { |i| h[i] = i * 2 }\nputs h.size"))
	if !ok {
		t.Fatal("expected CompileProgram to lower the top level")
	}
	for _, want := range []string{
		"func (vm *VM) aotMain() object.Value {",
		"var aotic0 inlineCache",
		"RegisterCompiledMain((*VM).aotMain)",
		"&Proc{native: func(vm *VM, bargs",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated file missing %q:\n%s", want, content)
		}
	}
	found := false
	for _, k := range keys {
		if k == "<main>" {
			found = true
		}
	}
	if !found {
		t.Errorf("keys %v missing \"<main>\"", keys)
	}
}
