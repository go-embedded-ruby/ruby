package aot

// Level-2 lowering: the program's *top-level* code (the `<main>` ISeq) and the
// *blocks* it passes to methods, lowered to Go — the broad, method-/block-/
// string-/array-/hash-heavy "real app code" shape that defines no hot method for
// level-1/level-3 to specialise, so it otherwise runs entirely on the
// interpreter (the array/blocks/hash/wordcount rows in bench/README.md).
//
// Where level 1 lowers a single method's straight-line bytecode, level 2 lowers
// a whole scope *tree*: the top-level ISeq becomes `func (vm *VM) aotMain()`, and
// every literal block a `send` carries becomes an inline Go closure passed as a
// native Proc, so the block body runs as compiled Go too (no per-yield exec
// frame, no bytecode dispatch loop). Locals a block closes over are the
// enclosing Go function's variables, captured lexically by the closure — so
// `t += i` inside `10_000_000.times { |i| t += i }` mutates the outer `t`
// directly. Every operator and send still goes through the runtime
// (vm.binaryOp / vm.aotSend), so semantics are identical to the interpreter; the
// gain is purely the removed interpreter overhead. Each send site carries its
// own inline method cache (aoticN), matching the interpreter's monomorphic fast
// path.
//
// Anything this stage cannot lower — a def/class/module at top level, a rescue
// handler, a splat/keyword/block parameter on a block, a non-local `return`, an
// unsupported opcode — makes CompileMain return ok=false and leaves the whole
// program interpreted (a sound fall-back).

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
)

// mgen carries the state shared across a program's whole scope tree: the next
// scope id (for unique variable names) and the running inline-cache count (one
// slot per lowered send site).
type mgen struct {
	nextScopeID int
	nCaches     int
}

// mscope is one lowered ISeq scope (the top level, or a block). id names its
// variables (l<id>_<slot>, s<id>_<slot>); parent is the lexically enclosing
// scope, so a block's outer-local access walks up the chain to the Go variable
// the enclosing function declared (and the closure captures).
type mscope struct {
	g      *mgen
	iseq   *bytecode.ISeq
	id     int
	parent *mscope
	depth  []int
}

// CompileMain lowers the program's top-level ISeq to `func (vm *VM) aotMain()
// object.Value`, plus the `nCaches` inline-cache slots its send sites need. ok is
// false when the top level uses a construct this stage does not lower, in which
// case the caller keeps interpreting the whole program.
func CompileMain(top *bytecode.ISeq) (fnSrc string, nCaches int, ok bool) {
	g := &mgen{}
	body, ok := g.lowerScope(top, nil, false)
	if !ok {
		return "", 0, false
	}
	var b strings.Builder
	b.WriteString("func (vm *VM) aotMain() object.Value {\n")
	b.WriteString("\tself := vm.main\n\t_ = self\n")
	b.WriteString(body)
	b.WriteString("}\n")
	return b.String(), g.nCaches, true
}

// lowerScope emits the body (declarations + parameter binding + instructions) of
// one scope. isBlock selects block-argument binding for a block's parameters;
// the top level (isBlock=false) has no parameters and its self is bound by the
// aotMain wrapper. It returns ok=false if the scope uses an unlowerable shape.
func (g *mgen) lowerScope(iseq *bytecode.ISeq, parent *mscope, isBlock bool) (string, bool) {
	// Only fixed positional parameters: a splat/keyword/**rest/&block parameter
	// needs binding machinery this stage does not model.
	if iseq.SplatIndex >= 0 || len(iseq.KwNames) > 0 || iseq.KwRestSlot >= 0 || iseq.BlockSlot >= 0 {
		return "", false
	}
	if len(iseq.Params) != iseq.NumRequired {
		return "", false // optional positionals need the default-value prologue
	}
	if len(iseq.Insns) == 0 || iseq.Insns[len(iseq.Insns)-1].Op != bytecode.OpReturn {
		return "", false
	}
	depth, maxDepth, known := stackDepths(iseq)
	for _, k := range known {
		if !k {
			return "", false // dead code — leave the program interpreted
		}
	}
	s := &mscope{g: g, iseq: iseq, id: g.nextScopeID, parent: parent, depth: depth}
	g.nextScopeID++
	targets := jumpTargets(iseq)

	var b strings.Builder
	if iseq.NumLocals > 0 {
		fmt.Fprintf(&b, "\tvar %s object.Value\n", s.localDecls())
		for i := 0; i < iseq.NumLocals; i++ {
			fmt.Fprintf(&b, "\t_ = %s\n", s.name(i))
		}
	}
	// The scope ends in OpReturn, which pops a value, so it pushed at least one:
	// maxDepth is always ≥ 1 and the stack declaration is non-empty.
	fmt.Fprintf(&b, "\tvar %s object.Value\n", s.stackDecls(maxDepth))
	if isBlock {
		np := len(iseq.Params)
		fmt.Fprintf(&b, "\tbargs = aotBlockArgs(%d, bargs)\n\t_ = bargs\n", np)
		for i := 0; i < np; i++ {
			fmt.Fprintf(&b, "\t%s = bargs[%d]\n", s.name(i), i)
		}
	}
	for pc := 0; pc < len(iseq.Insns); pc++ {
		if targets[pc] {
			fmt.Fprintf(&b, "L%d:\n", pc)
		}
		stmt, ok := s.emit(pc)
		if !ok {
			return "", false
		}
		b.WriteString(stmt)
	}
	return b.String(), true
}

// emit returns the Go statement(s) for one instruction, or ok=false when the
// opcode (or a variant of it) is not lowered by this stage.
func (s *mscope) emit(pc int) (string, bool) {
	in := s.iseq.Insns[pc]
	d := s.depth[pc]
	line := func(format string, a ...any) string { return "\t" + fmt.Sprintf(format, a...) + "\n" }
	switch in.Op {
	case bytecode.OpPushConst:
		expr, ok := constExpr(s.iseq.Consts[in.A])
		if !ok {
			return "", false
		}
		return line("%s = %s", s.sv(d), expr), true
	case bytecode.OpPushNil:
		return line("%s = object.NilV", s.sv(d)), true
	case bytecode.OpPushTrue:
		return line("%s = object.True", s.sv(d)), true
	case bytecode.OpPushFalse:
		return line("%s = object.False", s.sv(d)), true
	case bytecode.OpPushSelf:
		return line("%s = self", s.sv(d)), true
	case bytecode.OpGetLocal:
		l, ok := s.local(in.A, in.B)
		if !ok {
			return "", false
		}
		return line("%s = %s", s.sv(d), l), true
	case bytecode.OpSetLocal:
		l, ok := s.local(in.A, in.B)
		if !ok {
			return "", false
		}
		return line("%s = %s", l, s.sv(d-1)), true
	case bytecode.OpPop:
		return "", true
	case bytecode.OpDup:
		return line("%s = %s", s.sv(d), s.sv(d-1)), true
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv,
		bytecode.OpMod, bytecode.OpLt, bytecode.OpGt, bytecode.OpLe,
		bytecode.OpGe, bytecode.OpEq, bytecode.OpNeq:
		return line("%s = vm.binaryOp(bytecode.%s, %s, %s)", s.sv(d-2), opName(in.Op), s.sv(d-2), s.sv(d-1)), true
	case bytecode.OpNeg:
		return line("%s = negate(%s)", s.sv(d-1), s.sv(d-1)), true
	case bytecode.OpNot:
		return line("%s = object.Bool(!%s.Truthy())", s.sv(d-1), s.sv(d-1)), true
	case bytecode.OpTruthy:
		return line("%s = object.Bool(%s.Truthy())", s.sv(d-1), s.sv(d-1)), true
	case bytecode.OpJump:
		return line("goto L%d", in.A), true
	case bytecode.OpBranchIf:
		return line("if %s.Truthy() { goto L%d }", s.sv(d-1), in.A), true
	case bytecode.OpBranchUnless:
		return line("if !%s.Truthy() { goto L%d }", s.sv(d-1), in.A), true
	case bytecode.OpBranchNil:
		return line("if _, isNil := %s.(object.Nil); isNil { goto L%d }", s.sv(d-1), in.A), true
	case bytecode.OpSend:
		return s.emitSend(pc, d)
	case bytecode.OpNewArray:
		return line("%s = &object.Array{Elems: []object.Value{%s}}", s.sv(d-in.A), s.slotRange(d-in.A, in.A)), true
	case bytecode.OpNewHash:
		n := in.A * 2
		base := d - n
		var sb strings.Builder
		sb.WriteString("\t{\n\t\th := object.NewHash()\n")
		for i := 0; i < n; i += 2 {
			fmt.Fprintf(&sb, "\t\th.Set(%s, %s)\n", s.sv(base+i), s.sv(base+i+1))
		}
		fmt.Fprintf(&sb, "\t\t%s = h\n\t}\n", s.sv(base))
		return sb.String(), true
	case bytecode.OpNewRange:
		excl := "false"
		if in.A == 1 {
			excl = "true"
		}
		return line("%s = &object.Range{Lo: %s, Hi: %s, Exclusive: %s}", s.sv(d-2), s.sv(d-2), s.sv(d-1), excl), true
	case bytecode.OpGetIvar:
		return line("%s = getIvar(self, %q)", s.sv(d), s.iseq.Names[in.A]), true
	case bytecode.OpSetIvar:
		return line("setIvar(self, %q, %s)", s.iseq.Names[in.A], s.sv(d-1)), true
	case bytecode.OpGetConst:
		return line("%s = vm.aotConst(%q)", s.sv(d), s.iseq.Names[in.A]), true
	case bytecode.OpSetConst:
		return line("vm.consts[%q] = %s", s.iseq.Names[in.A], s.sv(d-1)), true
	case bytecode.OpGetGVar:
		return line("%s = vm.gvar(%q)", s.sv(d), s.iseq.Names[in.A]), true
	case bytecode.OpReturn:
		if in.A != 0 {
			return "", false // an explicit non-local `return` from a block: not lowered
		}
		return line("return %s", s.sv(d-1)), true
	}
	return "", false
}

// emitSend lowers OpSend. Without a literal block it becomes a cached vm.aotSend
// (mirroring the interpreter's monomorphic fast path + visibility check); with
// one, the child block ISeq is lowered to an inline Go closure passed as a
// native Proc so the block body runs as compiled Go.
func (s *mscope) emitSend(pc, d int) (string, bool) {
	in := s.iseq.Insns[pc]
	argc := in.B
	recvSlot := d - argc - 1
	name := s.iseq.Names[in.A]
	argList := make([]string, argc)
	for i := 0; i < argc; i++ {
		argList[i] = s.sv(recvSlot + 1 + i)
	}
	argsExpr := "[]object.Value{" + strings.Join(argList, ", ") + "}"
	if in.C == 0 {
		cache := s.g.nCaches
		s.g.nCaches++
		return fmt.Sprintf("\t%s = vm.aotSend(&aotic%d, %s, %q, %s, %d, self, nil)\n",
			s.sv(recvSlot), cache, s.sv(recvSlot), name, argsExpr, in.Flags), true
	}
	closure, ok := s.g.emitClosure(s.iseq.Children[in.C-1], s)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("\t%s = vm.dispatchSend(%s, %q, %s, &Proc{native: %s})\n",
		s.sv(recvSlot), s.sv(recvSlot), name, argsExpr, closure), true
}

// emitClosure lowers a block ISeq to a Go func literal of the native-Proc shape
// `func(vm *VM, bargs []object.Value) object.Value`, whose body is the block
// lowered by lowerScope. Outer-scope locals it reads/writes resolve to the
// enclosing scope's Go variables, which the closure captures lexically.
func (g *mgen) emitClosure(child *bytecode.ISeq, parent *mscope) (string, bool) {
	body, ok := g.lowerScope(child, parent, true)
	if !ok {
		return "", false
	}
	return "func(vm *VM, bargs []object.Value) object.Value {\n" + body + "}", true
}

// name is the Go variable for local slot i in this scope.
func (s *mscope) name(i int) string { return "l" + strconv.Itoa(s.id) + "_" + strconv.Itoa(i) }

// sv is the Go variable for operand-stack slot d in this scope.
func (s *mscope) sv(d int) string { return "s" + strconv.Itoa(s.id) + "_" + strconv.Itoa(d) }

// local resolves an OpGetLocal/OpSetLocal (slot, up) to the Go variable of the
// scope `up` levels out, or ok=false if the chain does not reach that far.
func (s *mscope) local(slot, up int) (string, bool) {
	t := s
	for ; up > 0; up-- {
		if t.parent == nil {
			return "", false
		}
		t = t.parent
	}
	return t.name(slot), true
}

func (s *mscope) localDecls() string {
	parts := make([]string, s.iseq.NumLocals)
	for i := range parts {
		parts[i] = s.name(i)
	}
	return strings.Join(parts, ", ")
}

func (s *mscope) stackDecls(max int) string {
	parts := make([]string, max)
	for i := range parts {
		parts[i] = s.sv(i)
	}
	return strings.Join(parts, ", ")
}

func (s *mscope) slotRange(from, count int) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = s.sv(from + i)
	}
	return strings.Join(parts, ", ")
}
