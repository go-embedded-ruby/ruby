// Package aot is the build-time ahead-of-time compiler: it lowers a method's
// bytecode ISeq to Go source, which the Go toolchain then compiles to native
// code. This is the "level-1" form — sound and unspecialised: every operator
// still goes through vm.binaryOp and every non-self send through vm.dispatchSend,
// so the generated code's semantics are identical to the interpreter, while the
// bytecode dispatch loop, operand-stack heap traffic and per-instruction
// overhead are gone. A method using an opcode or constant this stage cannot
// lower is simply left to the interpreter (Compile returns ok=false).
package aot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Compile emits the Go source of a `func (vm *VM) <goName>(self object.Value,
// args []object.Value) object.Value` that runs iseq. rubyName is the method's
// own Ruby name, so a self-send to it becomes a direct recursive call to goName.
// ok is false when the body uses something this stage does not lower yet, in
// which case the caller keeps interpreting that method.
func Compile(iseq *bytecode.ISeq, goName, rubyName string) (string, bool) {
	// This stage handles only fixed positional parameters (no splat / keyword /
	// **rest / &block), and a body that ends in OpReturn (so the emitted function
	// returns on all paths).
	if iseq.SplatIndex >= 0 || len(iseq.KwNames) > 0 || iseq.KwRestSlot >= 0 || iseq.BlockSlot >= 0 {
		return "", false
	}
	// Optional parameters need the arg_given/default-value machinery, which the
	// prologue (which binds every param from args[i]) does not model; leave them
	// to the interpreter. With splat/kw already excluded, len(Params) > NumRequired
	// means optional positionals are present.
	if len(iseq.Params) != iseq.NumRequired {
		return "", false
	}
	if len(iseq.Insns) == 0 || iseq.Insns[len(iseq.Insns)-1].Op != bytecode.OpReturn {
		return "", false
	}

	depth, maxDepth, known := stackDepths(iseq)
	for _, k := range known {
		if !k { // unreachable instruction (dead code) — leave the method to the interpreter
			return "", false
		}
	}
	targets := jumpTargets(iseq)

	var b strings.Builder
	fmt.Fprintf(&b, "func (vm *VM) %s(self object.Value, args []object.Value, block *Proc) object.Value {\n", goName)
	if iseq.NumLocals > 0 {
		fmt.Fprintf(&b, "\tvar %s object.Value\n", localDecls(iseq.NumLocals))
		for i := 0; i < len(iseq.Params); i++ {
			fmt.Fprintf(&b, "\tl%d = args[%d]\n", i, i)
		}
		for i := 0; i < iseq.NumLocals; i++ {
			fmt.Fprintf(&b, "\t_ = l%d\n", i) // a local may be unread (e.g. an unused param)
		}
	}
	fmt.Fprintf(&b, "\t_ = self\n") // self/args/block may be unused in a leaf method
	fmt.Fprintf(&b, "\t_ = args\n")
	fmt.Fprintf(&b, "\t_ = block\n")
	// The body ends in OpReturn, which pops a value, so the operand stack is used
	// (maxDepth ≥ 1) and the declaration is always non-empty.
	fmt.Fprintf(&b, "\tvar %s object.Value\n", stackDecls(maxDepth))

	g := &gen{iseq: iseq, rubyName: rubyName, goName: goName, depth: depth, isSelf: map[int]bool{}}
	for pc := 0; pc < len(iseq.Insns); pc++ {
		if targets[pc] {
			fmt.Fprintf(&b, "L%d:\n", pc)
		}
		stmt, ok := g.emit(pc)
		if !ok {
			return "", false
		}
		b.WriteString(stmt)
	}
	// The last instruction is OpReturn (checked above), so the emitted body ends
	// in a terminating `return` — no fall-off-the-end return is needed.
	b.WriteString("}\n")
	return b.String(), true
}

type gen struct {
	iseq     *bytecode.ISeq
	rubyName string
	goName   string
	depth    []int        // operand-stack depth on entry to each pc
	isSelf   map[int]bool // stack slot index → currently holds `self`
}

// emit returns the Go statement(s) for one instruction (each line tab-indented
// and newline-terminated), or ok=false if it cannot be lowered.
func (g *gen) emit(pc int) (string, bool) {
	in := g.iseq.Insns[pc]
	d := g.depth[pc] // slots [0,d) are live on entry; a push lands in slot d
	line := func(format string, a ...any) string { return "\t" + fmt.Sprintf(format, a...) + "\n" }
	delete(g.isSelf, d)
	switch in.Op {
	case bytecode.OpPushConst:
		expr, ok := constExpr(g.iseq.Consts[in.A])
		if !ok {
			return "", false
		}
		return line("s%d = %s", d, expr), true
	case bytecode.OpPushNil:
		return line("s%d = object.NilV", d), true
	case bytecode.OpPushTrue:
		return line("s%d = object.True", d), true
	case bytecode.OpPushFalse:
		return line("s%d = object.False", d), true
	case bytecode.OpPushSelf:
		g.isSelf[d] = true
		return line("s%d = self", d), true
	case bytecode.OpGetLocal:
		if in.B != 0 { // an outer-scope (closure) local — not this stage
			return "", false
		}
		return line("s%d = l%d", d, in.A), true
	case bytecode.OpSetLocal:
		if in.B != 0 {
			return "", false
		}
		return line("l%d = s%d", in.A, d-1), true
	case bytecode.OpPop:
		return "", true // the slot is simply abandoned
	case bytecode.OpDup:
		return line("s%d = s%d", d, d-1), true
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv,
		bytecode.OpMod, bytecode.OpLt, bytecode.OpGt, bytecode.OpLe,
		bytecode.OpGe, bytecode.OpEq, bytecode.OpNeq:
		return line("s%d = vm.binaryOp(bytecode.%s, s%d, s%d)", d-2, opName(in.Op), d-2, d-1), true
	case bytecode.OpNeg:
		return line("s%d = negate(s%d)", d-1, d-1), true
	case bytecode.OpNot:
		return line("s%d = object.Bool(!s%d.Truthy())", d-1, d-1), true
	case bytecode.OpTruthy:
		return line("s%d = object.Bool(s%d.Truthy())", d-1, d-1), true
	case bytecode.OpJump:
		return line("goto L%d", in.A), true
	case bytecode.OpBranchIf:
		return line("if s%d.Truthy() { goto L%d }", d-1, in.A), true
	case bytecode.OpBranchUnless:
		return line("if !s%d.Truthy() { goto L%d }", d-1, in.A), true
	case bytecode.OpBranchNil:
		return line("if _, isNil := s%d.(object.Nil); isNil { goto L%d }", d-1, in.A), true
	case bytecode.OpSend:
		if in.C != 0 { // a literal block — not this stage
			return "", false
		}
		argc := in.B
		recvSlot := d - argc - 1
		name := g.iseq.Names[in.A]
		argList := make([]string, argc)
		for i := 0; i < argc; i++ {
			argList[i] = fmt.Sprintf("s%d", recvSlot+1+i)
		}
		argsExpr := "[]object.Value{" + strings.Join(argList, ", ") + "}"
		if g.isSelf[recvSlot] && name == g.rubyName {
			// Self-send to the method being compiled → direct recursive call (a
			// plain send carries no block).
			return line("s%d = vm.%s(self, %s, nil)", recvSlot, g.goName, argsExpr), true
		}
		return line("s%d = vm.dispatchSend(s%d, %q, %s, nil)", recvSlot, recvSlot, name, argsExpr), true
	case bytecode.OpNewArray:
		return line("s%d = &object.Array{Elems: []object.Value{%s}}", d-in.A, slotRange(d-in.A, in.A)), true
	case bytecode.OpNewHash:
		n := in.A * 2
		base := d - n
		var sb strings.Builder
		sb.WriteString("\t{\n\t\th := object.NewHash()\n")
		for i := 0; i < n; i += 2 {
			fmt.Fprintf(&sb, "\t\th.Set(s%d, s%d)\n", base+i, base+i+1)
		}
		fmt.Fprintf(&sb, "\t\ts%d = h\n\t}\n", base)
		return sb.String(), true
	case bytecode.OpNewRange:
		excl := "false"
		if in.A == 1 {
			excl = "true"
		}
		return line("s%d = &object.Range{Lo: s%d, Hi: s%d, Exclusive: %s}", d-2, d-2, d-1, excl), true
	case bytecode.OpGetIvar:
		return line("s%d = getIvar(self, %q)", d, g.iseq.Names[in.A]), true
	case bytecode.OpSetIvar:
		return line("setIvar(self, %q, s%d)", g.iseq.Names[in.A], d-1), true
	case bytecode.OpGetConst:
		return line("s%d = vm.aotConst(%q)", d, g.iseq.Names[in.A]), true
	case bytecode.OpSetConst:
		return line("vm.consts[%q] = s%d", g.iseq.Names[in.A], d-1), true
	case bytecode.OpGetGVar:
		return line("s%d = vm.gvar(%q)", d, g.iseq.Names[in.A]), true
	case bytecode.OpSplatToArray:
		return line("s%d = vm.aotSplat(s%d)", d-1, d-1), true
	case bytecode.OpConcatArray:
		return line("s%d = aotConcat(s%d, s%d)", d-2, d-2, d-1), true
	case bytecode.OpRegexp:
		// Frozen, matching the interpreter's OpRegexp (Ruby 3.0+ freezes literals).
		return line("s%d = vm.compileLiteralRegexp(%q, %q)", d, g.iseq.Names[in.A], g.iseq.Names[in.B]), true
	case bytecode.OpBlockGiven:
		return line("s%d = object.Bool(block != nil)", d), true
	case bytecode.OpInvokeBlock:
		return line("s%d = vm.aotYield(block, []object.Value{%s})", d-in.A, slotRange(d-in.A, in.A)), true
	case bytecode.OpReturn:
		return line("return s%d", d-1), true
	}
	return "", false // an opcode this stage does not lower
}

// stackDepths computes the operand-stack depth on entry to each pc by forward
// propagation over the (well-formed, stack-disciplined) bytecode.
func stackDepths(iseq *bytecode.ISeq) (depth []int, max int, known []bool) {
	n := len(iseq.Insns)
	depth = make([]int, n)
	known = make([]bool, n)
	depth[0], known[0] = 0, true
	changed := true
	for changed {
		changed = false
		for pc := 0; pc < n; pc++ {
			if !known[pc] {
				continue
			}
			set := func(target, nd int) {
				if nd > max {
					max = nd
				}
				if !known[target] {
					depth[target], known[target] = nd, true
					changed = true
				}
			}
			in := iseq.Insns[pc]
			after := depth[pc] + delta(in)
			switch in.Op {
			case bytecode.OpJump:
				set(in.A, after)
			case bytecode.OpBranchIf, bytecode.OpBranchUnless, bytecode.OpBranchNil:
				set(in.A, after)
				set(pc+1, after)
			case bytecode.OpReturn:
				// no successor
			default:
				set(pc+1, after)
			}
		}
	}
	return depth, max, known
}

// delta is the net operand-stack change of an instruction (0 for an opcode this
// stage does not model — its depths are discarded, since emit then bails).
func delta(in bytecode.Instr) int {
	switch in.Op {
	case bytecode.OpPushConst, bytecode.OpPushNil, bytecode.OpPushTrue,
		bytecode.OpPushFalse, bytecode.OpPushSelf, bytecode.OpGetLocal, bytecode.OpDup,
		bytecode.OpGetIvar, bytecode.OpGetConst, bytecode.OpGetGVar, bytecode.OpRegexp,
		bytecode.OpBlockGiven:
		return 1
	case bytecode.OpPop, bytecode.OpBranchIf, bytecode.OpBranchUnless,
		bytecode.OpBranchNil, bytecode.OpReturn, bytecode.OpNewRange, bytecode.OpConcatArray,
		bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv,
		bytecode.OpMod, bytecode.OpLt, bytecode.OpGt, bytecode.OpLe,
		bytecode.OpGe, bytecode.OpEq, bytecode.OpNeq:
		return -1
	case bytecode.OpSetLocal, bytecode.OpJump, bytecode.OpNeg, bytecode.OpNot, bytecode.OpTruthy,
		bytecode.OpSetIvar, bytecode.OpSetConst, bytecode.OpSplatToArray:
		return 0
	case bytecode.OpSend:
		return -in.B // pop argc args + recv, push result
	case bytecode.OpNewArray, bytecode.OpInvokeBlock:
		return 1 - in.A
	case bytecode.OpNewHash:
		return 1 - 2*in.A
	}
	return 0
}

// jumpTargets returns the set of pcs any branch/jump can land on, so a Go label
// is emitted there.
func jumpTargets(iseq *bytecode.ISeq) map[int]bool {
	t := map[int]bool{}
	for _, in := range iseq.Insns {
		switch in.Op {
		case bytecode.OpJump, bytecode.OpBranchIf, bytecode.OpBranchUnless, bytecode.OpBranchNil:
			t[in.A] = true
		}
	}
	return t
}

// constExpr reconstructs a Go expression building the constant value, or
// ok=false for a kind this stage does not lower (e.g. Bignum).
func constExpr(v object.Value) (string, bool) {
	switch x := v.(type) {
	case object.Integer:
		return "object.Integer(" + strconv.FormatInt(int64(x), 10) + ")", true
	case object.Float:
		return "object.Float(" + strconv.FormatFloat(float64(x), 'g', -1, 64) + ")", true
	case *object.String:
		return "object.NewString(" + strconv.Quote(x.Str()) + ")", true
	case object.Symbol:
		return "object.Symbol(" + strconv.Quote(string(x)) + ")", true
	}
	return "", false
}

// slotRange returns the comma-separated stack slots s<from> … s<from+count-1>.
func slotRange(from, count int) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = "s" + strconv.Itoa(from+i)
	}
	return strings.Join(parts, ", ")
}

func localDecls(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "l" + strconv.Itoa(i)
	}
	return strings.Join(parts, ", ")
}

func stackDecls(max int) string {
	parts := make([]string, max)
	for i := range parts {
		parts[i] = "s" + strconv.Itoa(i)
	}
	return strings.Join(parts, ", ")
}

// opName maps an arithmetic/comparison opcode to its bytecode constant name, for
// emitting `bytecode.OpAdd` etc.
func opName(op bytecode.Op) string {
	switch op {
	case bytecode.OpAdd:
		return "OpAdd"
	case bytecode.OpSub:
		return "OpSub"
	case bytecode.OpMul:
		return "OpMul"
	case bytecode.OpDiv:
		return "OpDiv"
	case bytecode.OpMod:
		return "OpMod"
	case bytecode.OpLt:
		return "OpLt"
	case bytecode.OpGt:
		return "OpGt"
	case bytecode.OpLe:
		return "OpLe"
	case bytecode.OpGe:
		return "OpGe"
	case bytecode.OpEq:
		return "OpEq"
	case bytecode.OpNeq:
		return "OpNeq"
	}
	return "" // unreachable for the arithmetic/comparison opcodes emit passes in
}
