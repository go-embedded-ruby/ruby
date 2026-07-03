package aot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// CompileSpecialized tries to lower iseq as a level-3 "integer kernel": when the
// whole body is integer arithmetic/comparison on Integer parameters (with
// self-recursion and integer constants), it emits three functions —
//
//	<goName>_l1  the sound level-1 body (the universal fall-back)
//	<goName>_k   the unboxed int64 kernel (the fast path)
//	<goName>     a boxed entry that guards the args are Integer and recovers a
//	             kernel deopt (overflow / divide-by-zero), re-running via _l1
//
// so the registered entry runs entirely on int64 in the common case and remains
// correct for every input. ok is false when the body is not a pure integer
// kernel, leaving the caller to use the level-1 Compile.
func CompileSpecialized(iseq *bytecode.ISeq, goName, rubyName string) (string, bool) {
	// The level-1 body is both the deopt fall-back and the eligibility floor: the
	// kernel handles a strict subset of what Compile does, so if Compile declines
	// (bad params, unreachable code, an unlowerable opcode) so must the kernel.
	l1, ok := Compile(iseq, goName+"_l1", rubyName)
	if !ok {
		return "", false
	}
	// Compile succeeded ⇒ the base guards hold and every instruction is reachable.
	depth, maxDepth, _ := stackDepths(iseq)
	body, ok := emitKernel(iseq, goName, rubyName, depth)
	if !ok {
		return "", false // lowerable at level 1, but not a pure integer kernel
	}

	var b strings.Builder
	b.WriteString(l1)
	b.WriteString("\n")
	b.WriteString(kernelSignature(iseq, goName, maxDepth))
	b.WriteString(body)
	b.WriteString("}\n\n")
	b.WriteString(wrapper(iseq, goName))
	return b.String(), true
}

// kernelSignature emits the `func (vm *VM) <goName>_k(...) int64 {` header plus
// the int64 local and stack declarations.
func kernelSignature(iseq *bytecode.ISeq, goName string, maxDepth int) string {
	var params []string
	for i := 0; i < iseq.NumRequired; i++ {
		params = append(params, "l"+strconv.Itoa(i))
	}
	var b strings.Builder
	if len(params) > 0 {
		fmt.Fprintf(&b, "func (vm *VM) %s_k(%s int64) int64 {\n", goName, strings.Join(params, ", "))
	} else {
		fmt.Fprintf(&b, "func (vm *VM) %s_k() int64 {\n", goName)
	}
	// Locals beyond the parameters (loop counters etc.) start at zero.
	if iseq.NumLocals > iseq.NumRequired {
		var extra []string
		for i := iseq.NumRequired; i < iseq.NumLocals; i++ {
			extra = append(extra, "l"+strconv.Itoa(i))
		}
		fmt.Fprintf(&b, "\tvar %s int64\n", strings.Join(extra, ", "))
	}
	for i := 0; i < iseq.NumLocals; i++ {
		fmt.Fprintf(&b, "\t_ = l%d\n", i)
	}
	fmt.Fprintf(&b, "\tvar %s int64\n", int64StackDecls(maxDepth))
	return b.String()
}

// wrapper emits the boxed entry: guard each arg is an Integer (else deopt to the
// level-1 body), then run the kernel under a recover that turns an aotDeopt
// (overflow / divide-by-zero) into the level-1 result.
func wrapper(iseq *bytecode.ISeq, goName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "func (vm *VM) %s(self object.Value, args []object.Value, block *Proc) (res object.Value) {\n", goName)
	var kargs []string
	for i := 0; i < iseq.NumRequired; i++ {
		fmt.Fprintf(&b, "\ti%d, ok%d := args[%d].(object.Integer)\n", i, i, i)
		fmt.Fprintf(&b, "\tif !ok%d {\n\t\treturn vm.%s_l1(self, args, block)\n\t}\n", i, goName)
		kargs = append(kargs, fmt.Sprintf("int64(i%d)", i))
	}
	fmt.Fprintf(&b, "\tdefer func() {\n")
	fmt.Fprintf(&b, "\t\tif r := recover(); r != nil {\n")
	fmt.Fprintf(&b, "\t\t\tif _, d := r.(aotDeopt); d {\n")
	fmt.Fprintf(&b, "\t\t\t\tres = vm.%s_l1(self, args, block)\n\t\t\t\treturn\n\t\t\t}\n", goName)
	fmt.Fprintf(&b, "\t\t\tpanic(r)\n\t\t}\n\t}()\n")
	fmt.Fprintf(&b, "\t_ = self\n\t_ = args\n\t_ = block\n")
	fmt.Fprintf(&b, "\treturn object.IntValue(vm.%s_k(%s))\n}\n", goName, strings.Join(kargs, ", "))
	return b.String()
}

// emitKernel lowers the body to int64 statements, returning ok=false on any
// instruction outside the integer-kernel subset.
func emitKernel(iseq *bytecode.ISeq, goName, rubyName string, depth []int) (string, bool) {
	targets := jumpTargets(iseq)
	isSelf := map[int]bool{}
	var b strings.Builder
	line := func(format string, a ...any) { b.WriteString("\t" + fmt.Sprintf(format, a...) + "\n") }

	for pc := 0; pc < len(iseq.Insns); pc++ {
		if targets[pc] {
			fmt.Fprintf(&b, "L%d:\n", pc)
		}
		in := iseq.Insns[pc]
		d := depth[pc]
		delete(isSelf, d)

		switch in.Op {
		case bytecode.OpPushConst:
			n, isInt := iseq.Consts[in.A].(object.Integer)
			if !isInt {
				return "", false
			}
			line("k%d = %d", d, int64(n))
		case bytecode.OpGetLocal:
			// Compile (run first) already rejected outer-scope locals (B != 0).
			line("k%d = l%d", d, in.A)
		case bytecode.OpSetLocal:
			line("l%d = k%d", in.A, d-1)
		case bytecode.OpPushSelf:
			isSelf[d] = true // the receiver of a self-recursive call; no int64 value
		case bytecode.OpPushNil:
			// The only nil an integer kernel tolerates is a loop's discarded value
			// (`while … end` evaluates to nil), which the compiler immediately pops.
			// A nil that is used or returned is not an int64, so leave it to level 1.
			if pc+1 >= len(iseq.Insns) || iseq.Insns[pc+1].Op != bytecode.OpPop {
				return "", false
			}
			line("k%d = 0", d) // placeholder int64; the next OpPop discards it
		case bytecode.OpPop:
			// abandon the slot
		case bytecode.OpAdd:
			delete(isSelf, d-2)
			line("k%d = aotAdd(k%d, k%d)", d-2, d-2, d-1)
		case bytecode.OpSub:
			delete(isSelf, d-2)
			line("k%d = aotSub(k%d, k%d)", d-2, d-2, d-1)
		case bytecode.OpMul:
			delete(isSelf, d-2)
			line("k%d = aotMul(k%d, k%d)", d-2, d-2, d-1)
		case bytecode.OpDiv:
			delete(isSelf, d-2)
			line("k%d = aotDiv(k%d, k%d)", d-2, d-2, d-1)
		case bytecode.OpMod:
			delete(isSelf, d-2)
			line("k%d = aotMod(k%d, k%d)", d-2, d-2, d-1)
		case bytecode.OpNeg:
			delete(isSelf, d-1)
			line("k%d = aotNeg(k%d)", d-1, d-1)
		case bytecode.OpLt, bytecode.OpGt, bytecode.OpLe, bytecode.OpGe,
			bytecode.OpEq, bytecode.OpNeq:
			// A comparison is valid only as the test of an immediately following
			// conditional branch (handled there); on its own it is not an int64.
			if pc+1 >= len(iseq.Insns) || !isCondBranch(iseq.Insns[pc+1].Op) {
				return "", false
			}
		case bytecode.OpBranchIf, bytecode.OpBranchUnless:
			if pc == 0 || !isCompare(iseq.Insns[pc-1].Op) {
				return "", false
			}
			cmp := iseq.Insns[pc-1]
			cd := depth[pc-1] // operands of the compare live at cd-2, cd-1
			cond := fmt.Sprintf("k%d %s k%d", cd-2, goCmp(cmp.Op), cd-1)
			if in.Op == bytecode.OpBranchUnless {
				cond = "!(" + cond + ")"
			}
			line("if %s { goto L%d }", cond, in.A)
		case bytecode.OpJump:
			line("goto L%d", in.A)
		case bytecode.OpSend:
			// Compile (run first) already rejected sends carrying a block (C != 0).
			argc := in.B
			recvSlot := d - argc - 1
			if !isSelf[recvSlot] || iseq.Names[in.A] != rubyName {
				return "", false // only self-recursion is an integer-kernel send
			}
			kargs := make([]string, argc)
			for i := 0; i < argc; i++ {
				kargs[i] = fmt.Sprintf("k%d", recvSlot+1+i)
			}
			delete(isSelf, recvSlot) // the slot now holds the int64 result, not self
			line("k%d = vm.%s_k(%s)", recvSlot, goName, strings.Join(kargs, ", "))
		case bytecode.OpReturn:
			if isSelf[d-1] {
				return "", false // `return self` is not an int64
			}
			line("return k%d", d-1)
		default:
			return "", false
		}
	}
	return b.String(), true
}

func isCompare(op bytecode.Op) bool {
	switch op {
	case bytecode.OpLt, bytecode.OpGt, bytecode.OpLe, bytecode.OpGe,
		bytecode.OpEq, bytecode.OpNeq:
		return true
	}
	return false
}

func isCondBranch(op bytecode.Op) bool {
	return op == bytecode.OpBranchIf || op == bytecode.OpBranchUnless
}

// goCmp maps a comparison opcode to the Go operator on int64.
func goCmp(op bytecode.Op) string {
	switch op {
	case bytecode.OpLt:
		return "<"
	case bytecode.OpGt:
		return ">"
	case bytecode.OpLe:
		return "<="
	case bytecode.OpGe:
		return ">="
	case bytecode.OpEq:
		return "=="
	default: // OpNeq
		return "!="
	}
}

// int64StackDecls is stackDecls for the kernel's int64 operand slots.
func int64StackDecls(maxDepth int) string {
	parts := make([]string, maxDepth)
	for i := range parts {
		parts[i] = "k" + strconv.Itoa(i)
	}
	return strings.Join(parts, ", ")
}
