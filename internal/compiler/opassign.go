// Compound assignment to an index (`a[i] op= v`) and to an attribute
// (`a.x op= v`), lowered so the receiver and the index arguments are evaluated
// exactly once and the expression's value is the assigned value — never what
// the `[]=` / `x=` setter happens to return.
//
// The parser desugars both forms textually: `a[i] op= v` arrives as
// `a.[]=(i, a.[](i) op v)` and `a.x op= v` as `a.x=(a.x op v)`, with the
// receiver and index nodes SHARED (same AST pointers) between the read and the
// write. That shape is correct for value but wrong for effect: compiled
// literally it evaluates the receiver twice and every index argument twice. MRI
// does not: compile.c's compile_op_asgn1 evaluates the receiver and index once
// and reuses them with `dupn` (ruby/ruby v3_4_0 compile.c:9401), and
// compile_op_asgn2 does the same for an attribute with `dup` (compile.c:9535).
// Both then overwrite the reserved result slot with `setn`, so the expression
// yields the right-hand value (or, when `||=` / `&&=` short-circuits, the value
// already there).
//
// rbgo's stack has no `dupn`/`setn`/`topn`, so the same effect is obtained with
// anonymous local slots as temporaries.
package compiler

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-ruby-parser/parser/ast"
)

// indexOpAssign recognises the parser's desugaring of `recv[idx…] op= rhs`,
// returning the operator/right-hand BinaryExpr and the index argument nodes.
// Recognition is by AST pointer identity between the write's receiver/index
// nodes and the read's: the parser builds the read from exactly those nodes, so
// identity is what distinguishes a desugared compound assignment from a
// hand-written `a[i] = (a[i] || v)` (whose two subtrees are distinct nodes and
// which Ruby does evaluate twice).
func indexOpAssign(v *ast.Call) (*ast.BinaryExpr, []ast.Node, bool) {
	if v.Name != "[]=" || v.Recv == nil || v.Block != nil || v.Safe || len(v.Args) == 0 {
		return nil, nil, false
	}
	idx := v.Args[:len(v.Args)-1]
	be, ok := v.Args[len(v.Args)-1].(*ast.BinaryExpr)
	if !ok {
		return nil, nil, false
	}
	read, ok := be.Left.(*ast.Call)
	if !ok || read.Name != "[]" || read.Recv != v.Recv || read.Block != nil || len(read.Args) != len(idx) {
		return nil, nil, false
	}
	for i := range idx {
		if read.Args[i] != idx[i] {
			return nil, nil, false
		}
	}
	return be, idx, true
}

// attrOpAssign recognises the parser's desugaring of `recv.name op= rhs`
// (`recv.name=(recv.name op rhs)`), returning the operator/right-hand
// BinaryExpr. As for the index form, recognition is by pointer identity of the
// shared receiver node. The caller has already established that v is a setter
// call, so the trailing `=` is an attribute writer and not an operator method.
func attrOpAssign(v *ast.Call) (*ast.BinaryExpr, bool) {
	if v.Name == "[]=" || v.Recv == nil || v.Block != nil || v.Safe || len(v.Args) != 1 {
		return nil, false
	}
	base := v.Name[:len(v.Name)-1]
	be, ok := v.Args[0].(*ast.BinaryExpr)
	if !ok {
		return nil, false
	}
	read, ok := be.Left.(*ast.Call)
	if !ok || read.Name != base || read.Recv != v.Recv || read.Block != nil || len(read.Args) != 0 {
		return nil, false
	}
	return be, true
}

// stash evaluates node and parks its value in a fresh anonymous local slot,
// leaving the stack as it found it. OpSetLocal keeps the value on the stack, so
// the pop is explicit.
func (c *Compiler) stash(node ast.Node) int {
	b := c.cur()
	c.compileNode(node)
	slot := b.localSlot("")
	b.emit(bytecode.OpSetLocal, slot, 0)
	b.emit(bytecode.OpPop, 0, 0)
	return slot
}

// stashTop parks the value already on top of the stack in a fresh anonymous
// local slot, consuming it.
func (c *Compiler) stashTop() int {
	b := c.cur()
	slot := b.localSlot("")
	b.emit(bytecode.OpSetLocal, slot, 0)
	b.emit(bytecode.OpPop, 0, 0)
	return slot
}

// compileIndexOpAssign lowers `recv[idx…] op= rhs` with the receiver and the
// index arguments evaluated exactly once, leaving the assignment's value on the
// stack. Mirrors MRI compile.c compile_op_asgn1 (v3_4_0:9401).
func (c *Compiler) compileIndexOpAssign(v *ast.Call, be *ast.BinaryExpr, idx []ast.Node) {
	b := c.cur()
	explicit := isExplicitRecv(v.Recv)
	recvSlot := c.stash(v.Recv)

	// The index arguments are evaluated once. A `*splat` among them forces the
	// dynamic array path (and so a single #to_a call), exactly as MRI's
	// VM_CALL_ARGS_SPLAT branch does; otherwise each argument gets its own slot.
	splat := hasSplat(idx) || hasTrailingKwSplat(idx)
	var argSlots []int
	arrSlot := -1
	if splat {
		c.compileSplatItems(idx)
		arrSlot = c.stashTop()
	} else {
		argSlots = make([]int, len(idx))
		for i, a := range idx {
			argSlots[i] = c.stash(a)
		}
	}

	// read: recv[idx…]
	pushArgs := func() {
		b.emit(bytecode.OpGetLocal, recvSlot, 0)
		if splat {
			b.emit(bytecode.OpGetLocal, arrSlot, 0)
			return
		}
		for _, s := range argSlots {
			b.emit(bytecode.OpGetLocal, s, 0)
		}
	}
	pushArgs()
	if splat {
		c.sendExplicit(b.emit(bytecode.OpSendArray, b.addName("[]"), 0), explicit)
	} else {
		c.sendExplicit(b.emit(bytecode.OpSend, b.addName("[]"), len(idx)), explicit)
	}

	// write emits the `[]=` send for the value held in valSlot, consuming
	// nothing from the stack and leaving nothing behind.
	write := func(valSlot int) {
		pushArgs()
		b.emit(bytecode.OpGetLocal, valSlot, 0)
		if splat {
			// Append the assigned value to a fresh copy of the index array;
			// OpConcatArray allocates, so the stashed array is left intact.
			b.emit(bytecode.OpNewArray, 1, 0)
			b.emit(bytecode.OpConcatArray, 0, 0)
			c.sendExplicit(b.emit(bytecode.OpSendArray, b.addName("[]="), 0), explicit)
		} else {
			c.sendExplicit(b.emit(bytecode.OpSend, b.addName("[]="), len(idx)+1), explicit)
		}
		b.emit(bytecode.OpPop, 0, 0) // the setter's own return value is discarded
	}
	c.finishOpAssign(be, write)
}

// compileAttrOpAssign lowers `recv.name op= rhs` with the receiver evaluated
// exactly once. Mirrors MRI compile.c compile_op_asgn2 (v3_4_0:9535).
func (c *Compiler) compileAttrOpAssign(v *ast.Call, be *ast.BinaryExpr) {
	b := c.cur()
	explicit := isExplicitRecv(v.Recv)
	recvSlot := c.stash(v.Recv)
	base := v.Name[:len(v.Name)-1]

	b.emit(bytecode.OpGetLocal, recvSlot, 0)
	c.sendExplicit(b.emit(bytecode.OpSend, b.addName(base), 0), explicit)

	write := func(valSlot int) {
		b.emit(bytecode.OpGetLocal, recvSlot, 0)
		b.emit(bytecode.OpGetLocal, valSlot, 0)
		c.sendExplicit(b.emit(bytecode.OpSend, b.addName(v.Name), 1), explicit)
		b.emit(bytecode.OpPop, 0, 0)
	}
	c.finishOpAssign(be, write)
}

// finishOpAssign completes a compound assignment whose current value is already
// on top of the stack. It computes the new value from be (short-circuiting for
// `||=` / `&&=`), hands it to write, and leaves the expression's value — the
// assigned value, or the untouched current value when the short circuit is
// taken — on the stack.
func (c *Compiler) finishOpAssign(be *ast.BinaryExpr, write func(valSlot int)) {
	b := c.cur()
	if be.Op == "||" || be.Op == "&&" {
		b.emit(bytecode.OpDup, 0, 0)
		var short int
		if be.Op == "||" {
			short = b.emit(bytecode.OpBranchIf, 0, 0)
		} else {
			short = b.emit(bytecode.OpBranchUnless, 0, 0)
		}
		b.emit(bytecode.OpPop, 0, 0) // discard the current value; it is being replaced
		valSlot := c.stash(be.Right)
		write(valSlot)
		b.emit(bytecode.OpGetLocal, valSlot, 0)
		done := b.emit(bytecode.OpJump, 0, 0)
		b.patch(short, b.here()) // short circuit: the current value stays, and is the result
		b.patch(done, b.here())
		return
	}
	// An arithmetic/other operator: combine the current value with the right-hand
	// side, store that, and yield it.
	c.compileNode(be.Right)
	if op, ok := fastBinOp(be.Op); ok {
		b.emit(op, 0, 0)
	} else {
		b.emit(bytecode.OpSend, b.addName(be.Op), 1)
	}
	valSlot := c.stashTop()
	write(valSlot)
	b.emit(bytecode.OpGetLocal, valSlot, 0)
}

// isExplicitRecv reports whether a receiver node counts as an explicit receiver
// for the private-visibility check — every receiver except a literal `self`,
// matching compileCall.
func isExplicitRecv(recv ast.Node) bool {
	_, isSelf := recv.(*ast.SelfLit)
	return !isSelf
}

// sendExplicit marks the send at instruction index at as having an explicit
// receiver, when it had one.
func (c *Compiler) sendExplicit(at int, explicit bool) int {
	if explicit {
		c.cur().insns[at].Flags |= bytecode.FlagSendExplicit
	}
	return at
}

// scopedConstOrAssign recognises the parser's desugaring of `Mod::C ||= rhs`,
// which is `(defined?(Mod::C) && Mod::C) || (Mod::C = rhs)` with the SAME
// ScopedConst node in all three positions — so the module part `Mod` would be
// evaluated up to three times. MRI evaluates it once and keeps it on the stack
// (compile.c compile_op_cdecl, ruby/ruby v3_4_0:9657: the `cref` is compiled
// once, then `dup`/`defined`/`getconstant`/`setconstant` all read that one
// copy). Only a qualified constant has a module part to evaluate; a bare
// `C ||= v` has none.
func scopedConstOrAssign(v *ast.BinaryExpr) (*ast.ScopedConst, ast.Node, bool) {
	if v.Op != "||" {
		return nil, nil, false
	}
	asg, ok := v.Right.(*ast.ScopedConstAssign)
	if !ok {
		return nil, nil, false
	}
	sc, ok := asg.Target.(*ast.ScopedConst)
	if !ok || sc.Recv == nil {
		return nil, nil, false
	}
	guard, ok := v.Left.(*ast.BinaryExpr)
	if !ok || guard.Op != "&&" || guard.Right != ast.Node(sc) {
		return nil, nil, false
	}
	d, ok := guard.Left.(*ast.Call)
	if !ok || d.Name != "defined?" || d.Recv != nil || len(d.Args) != 1 || d.Args[0] != ast.Node(sc) {
		return nil, nil, false
	}
	return sc, asg.Value, true
}

// scopedConstOpAssign recognises `Mod::C op= rhs` for every operator but `||`
// — the parser writes it as `Mod::C = (Mod::C op rhs)`, again sharing the
// ScopedConst node and so the module part.
func scopedConstOpAssign(v *ast.ScopedConstAssign) (*ast.ScopedConst, *ast.BinaryExpr, bool) {
	sc, ok := v.Target.(*ast.ScopedConst)
	if !ok || sc.Recv == nil {
		return nil, nil, false
	}
	be, ok := v.Value.(*ast.BinaryExpr)
	if !ok || be.Left != ast.Node(sc) {
		return nil, nil, false
	}
	return sc, be, true
}

// compileScopedConstOrAssign lowers `Mod::C ||= rhs` with the module part
// evaluated exactly once. The constant is read only when it is defined, so an
// undefined one assigns without raising; the right-hand side is evaluated only
// on the assigning path, so a module part that raises never reaches it.
func (c *Compiler) compileScopedConstOrAssign(sc *ast.ScopedConst, rhs ast.Node) {
	b := c.cur()
	modSlot := c.stash(sc.Recv)
	name := b.addName(sc.Name)
	b.emit(bytecode.OpGetLocal, modSlot, 0)
	b.emit(bytecode.OpDefinedScopedConst, name, 0)
	assign := b.emit(bytecode.OpBranchUnless, 0, 0)
	b.emit(bytecode.OpGetLocal, modSlot, 0)
	b.emit(bytecode.OpGetScopedConst, name, 0)
	b.emit(bytecode.OpDup, 0, 0)
	keep := b.emit(bytecode.OpBranchIf, 0, 0) // already truthy: that value is the result
	b.emit(bytecode.OpPop, 0, 0)
	b.patch(assign, b.here())
	valSlot := c.stash(rhs)
	b.emit(bytecode.OpGetLocal, modSlot, 0)
	b.emit(bytecode.OpGetLocal, valSlot, 0)
	b.emit(bytecode.OpSetScopedConst, name, 0) // keeps the value as the result
	b.patch(keep, b.here())
}

// compileScopedConstOpAssign lowers `Mod::C op= rhs` (including `&&=`) with the
// module part evaluated exactly once.
func (c *Compiler) compileScopedConstOpAssign(sc *ast.ScopedConst, be *ast.BinaryExpr) {
	b := c.cur()
	modSlot := c.stash(sc.Recv)
	name := b.addName(sc.Name)
	b.emit(bytecode.OpGetLocal, modSlot, 0)
	b.emit(bytecode.OpGetScopedConst, name, 0)
	c.finishOpAssign(be, func(valSlot int) {
		b.emit(bytecode.OpGetLocal, modSlot, 0)
		b.emit(bytecode.OpGetLocal, valSlot, 0)
		b.emit(bytecode.OpSetScopedConst, name, 0)
		b.emit(bytecode.OpPop, 0, 0)
	})
}
