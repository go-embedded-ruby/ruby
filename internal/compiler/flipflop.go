// The flip-flop operator: a two-sided range literal written where a condition
// is expected (`x if (i == 4)..(i == 7)`). It is not a Range object but a piece
// of state that turns on when the left condition holds and off after the right
// one does — awk's and sed's range addressing.
//
// MRI's parser marks such a range NODE_FLIP2 (`..`) or NODE_FLIP3 (`...`) by
// its syntactic position, and compile.c's compile_flip_flop (ruby/ruby
// v3_4_0:4607) lowers it against a per-occurrence slot allocated in the
// enclosing *local* iseq — ISEQ_FLIP_CNT_INCREMENT(ISEQ_BODY(iseq)->local_iseq)
// — so a block shares its state with every other invocation of that block, and
// two textually identical flip-flops in different iseqs do not interfere.
//
// The generated sequence is MRI's, with one difference: MRI compiles it
// straight into the surrounding branch's then/else labels, while rbgo's
// conditions are ordinary values that the caller branches on, so this pushes
// true or false instead. rbgo's local slots are already depth-addressed and
// resolution stops at the first non-block scope, which is exactly MRI's
// local_iseq, so a slot allocated there has the lifetime the operator needs.
package compiler

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-ruby-parser/parser/ast"
)

// hasFlipFlop reports whether node is a flip-flop in condition position — a
// two-sided range literal, or a `!` / `&&` / `||` combination containing one.
// A beginless or endless range (`..x`, `x..`) is a Range value, not a
// flip-flop, and is left alone.
func hasFlipFlop(node ast.Node) bool {
	switch v := node.(type) {
	case *ast.RangeLit:
		return v.Lo != nil && v.Hi != nil
	case *ast.BinaryExpr:
		return (v.Op == "&&" || v.Op == "||") && (hasFlipFlop(v.Left) || hasFlipFlop(v.Right))
	case *ast.UnaryExpr:
		return v.Op == "!" && hasFlipFlop(v.Operand)
	}
	return false
}

// compileCondition compiles node in condition position, leaving a value for the
// caller to branch on. Without a flip-flop anywhere in it that is exactly
// compileNode; with one, the logical operators around it are re-emitted here so
// each operand is itself compiled in condition position (`a...b or c...d`).
func (c *Compiler) compileCondition(node ast.Node) {
	b := c.cur()
	switch v := node.(type) {
	case *ast.RangeLit:
		if v.Lo != nil && v.Hi != nil {
			c.compileFlipFlop(v)
			return
		}
	case *ast.BinaryExpr:
		// `&&` / `||`, short-circuiting on the left operand's value, as
		// compileLogical does for the ordinary case. Only re-emitted here when an
		// operand really is a flip-flop; otherwise the ordinary path handles it.
		if (v.Op == "&&" || v.Op == "||") && (hasFlipFlop(v.Left) || hasFlipFlop(v.Right)) {
			c.compileCondition(v.Left)
			b.emit(bytecode.OpDup, 0, 0)
			var short int
			if v.Op == "&&" {
				short = b.emit(bytecode.OpBranchUnless, 0, 0)
			} else {
				short = b.emit(bytecode.OpBranchIf, 0, 0)
			}
			b.emit(bytecode.OpPop, 0, 0)
			c.compileCondition(v.Right)
			b.patch(short, b.here())
			return
		}
	case *ast.UnaryExpr: // `!cond`, which is also how `unless` and `until` arrive
		if v.Op == "!" && hasFlipFlop(v.Operand) {
			c.compileCondition(v.Operand)
			b.emit(bytecode.OpNot, 0, 0)
			return
		}
	}
	c.compileNode(node)
}

// flipFlopSlot allocates this occurrence's state slot in the nearest enclosing
// non-block scope — MRI's local_iseq — and returns how many block scopes out
// that is, so the emitted GetLocal/SetLocal address it from here. The walk stops
// short of a borrowed scope (a Binding's locals, under eval): that frame is
// already sized, so an eval'd flip-flop keeps its state in the eval's own frame,
// which a proc built there captures and every later call of it shares.
func (c *Compiler) flipFlopSlot() (depth, slot int) {
	root, d := c.cur(), 0
	for root.isBlock && root.parent != nil && !root.parent.borrowed {
		root, d = root.parent, d+1
	}
	// Anonymous, like every other compiler temporary: nothing ever resolves it
	// by name, and it must not show up in Binding#local_variables.
	return d, root.localSlot("")
}

// compileFlipFlop emits the operator, leaving true or false on the stack. The
// slot starts nil (falsy), so the first evaluation tests the left condition.
//
// Off, and the left condition false: false, and the right condition is never
// evaluated. Off, and the left condition true: the state turns on, and `..`
// then tests the right condition on this same pass (MRI's `again`) while `...`
// does not — the one difference between the two spellings. On: the right
// condition decides whether this is the last true pass; either way the value is
// true.
func (c *Compiler) compileFlipFlop(v *ast.RangeLit) {
	b := c.cur()
	depth, slot := c.flipFlopSlot()

	b.emit(bytecode.OpGetLocal, slot, depth)
	alreadyOn := b.emit(bytecode.OpBranchIf, 0, 0)

	c.compileCondition(v.Lo)
	stayOff := b.emit(bytecode.OpBranchUnless, 0, 0)
	b.emit(bytecode.OpPushTrue, 0, 0)
	b.emit(bytecode.OpSetLocal, slot, depth)
	b.emit(bytecode.OpPop, 0, 0)
	turnedOn := -1
	if v.Exclusive {
		turnedOn = b.emit(bytecode.OpJump, 0, 0) // `...` skips the right condition on the turn-on pass
	}

	b.patch(alreadyOn, b.here())
	c.compileCondition(v.Hi)
	stayOn := b.emit(bytecode.OpBranchUnless, 0, 0)
	b.emit(bytecode.OpPushFalse, 0, 0)
	b.emit(bytecode.OpSetLocal, slot, depth)
	b.emit(bytecode.OpPop, 0, 0)
	lastPass := b.emit(bytecode.OpJump, 0, 0)

	b.patch(stayOff, b.here())
	b.emit(bytecode.OpPushFalse, 0, 0)
	done := b.emit(bytecode.OpJump, 0, 0)

	b.patch(stayOn, b.here())
	if turnedOn >= 0 {
		b.patch(turnedOn, b.here())
	}
	b.patch(lastPass, b.here())
	b.emit(bytecode.OpPushTrue, 0, 0)
	b.patch(done, b.here())
}
