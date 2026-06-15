// Package compiler lowers the AST to bytecode (an ISeq tree).
//
// Each method body and the program top level becomes one ISeq. Locals are
// resolved to flat slot indices here (Phase 0 has no closures, so each ISeq has
// a single flat local table; depth-addressed envs arrive with blocks in
// Phase 1, plan §6).
package compiler

import (
	"fmt"

	"github.com/go-embedded-ruby/ruby/internal/ast"
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

type compileError struct{ msg string }

func (e compileError) Error() string { return e.msg }

// builder accumulates a single ISeq under construction. For a block, parent
// links to the enclosing builder and isBlock is true, so local resolution can
// reach enclosing locals by depth.
type builder struct {
	name     string
	insns    []bytecode.Instr
	consts   []object.Value
	constIdx map[object.Value]int
	names    []string
	locals   []string
	params   []string
	children []*bytecode.ISeq
	parent   *builder
	isBlock  bool
}

func newBuilder(name string, params []string) *builder {
	b := &builder{name: name, constIdx: map[object.Value]int{}, params: params}
	for _, p := range params {
		b.localSlot(p) // params occupy slots 0..n-1, in order
	}
	return b
}

func newBlockBuilder(name string, params []string, parent *builder) *builder {
	b := newBuilder(name, params)
	b.parent = parent
	b.isBlock = true
	return b
}

// resolve finds a local by walking block scopes outward; depth 0 is the current
// builder. It stops at the first non-block (method/class/top) boundary.
func (b *builder) resolve(name string) (depth, index int, ok bool) {
	for cur, d := b, 0; cur != nil; cur, d = cur.parent, d+1 {
		if i, found := cur.localLookup(name); found {
			return d, i, true
		}
		if !cur.isBlock {
			break
		}
	}
	return 0, 0, false
}

func (b *builder) emit(op bytecode.Op, a, bb int) int {
	b.insns = append(b.insns, bytecode.Instr{Op: op, A: a, B: bb})
	return len(b.insns) - 1
}

func (b *builder) here() int { return len(b.insns) }

func (b *builder) patch(at, target int) { b.insns[at].A = target }

func (b *builder) addConst(v object.Value) int {
	if i, ok := b.constIdx[v]; ok {
		return i
	}
	i := len(b.consts)
	b.consts = append(b.consts, v)
	b.constIdx[v] = i
	return i
}

func (b *builder) addName(n string) int {
	for i, s := range b.names {
		if s == n {
			return i
		}
	}
	b.names = append(b.names, n)
	return len(b.names) - 1
}

// localSlot allocates a fresh slot for name and returns its index. It is only
// called for names known to be new in this scope (parameters, or an assignment
// that did not resolve to an existing local), so it does not deduplicate.
func (b *builder) localSlot(name string) int {
	b.locals = append(b.locals, name)
	return len(b.locals) - 1
}

// localLookup returns the slot for an existing local, or (-1, false).
func (b *builder) localLookup(name string) (int, bool) {
	for i, n := range b.locals {
		if n == name {
			return i, true
		}
	}
	return -1, false
}

func (b *builder) build() *bytecode.ISeq {
	return &bytecode.ISeq{
		Name:      b.name,
		Insns:     b.insns,
		Consts:    b.consts,
		Names:     b.names,
		Params:    b.params,
		NumLocals: len(b.locals),
		Children:  b.children,
	}
}

// Compiler walks the AST, maintaining a stack of builders.
type Compiler struct {
	ctxs []*loopCtx // innermost-last stack of break/next targets
	stack []*builder
}

// Compile lowers a Program into the top-level ISeq.
func Compile(prog *ast.Program) (iseq *bytecode.ISeq, err error) {
	defer func() {
		if r := recover(); r != nil {
			// Unchecked: a non-compileError is an internal bug and re-panics as a
			// conversion error rather than leaving an uncovered re-panic branch.
			iseq, err = nil, r.(compileError)
		}
	}()
	c := &Compiler{}
	c.push(newBuilder("<main>", nil))
	c.compileBody(prog.Body)
	c.cur().emit(bytecode.OpReturn, 0, 0)
	return c.pop().build(), nil
}

func (c *Compiler) cur() *builder  { return c.stack[len(c.stack)-1] }
func (c *Compiler) push(b *builder) { c.stack = append(c.stack, b) }
func (c *Compiler) pop() *builder {
	b := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	return b
}

func (c *Compiler) fail(format string, args ...any) {
	panic(compileError{msg: "compile error: " + fmt.Sprintf(format, args...)})
}

// compileBody emits a sequence whose value is the value of its last node
// (nil for an empty body). Intermediate values are popped.
func (c *Compiler) compileBody(body []ast.Node) {
	if len(body) == 0 {
		c.cur().emit(bytecode.OpPushNil, 0, 0)
		return
	}
	for i, n := range body {
		c.compileNode(n)
		if i < len(body)-1 {
			c.cur().emit(bytecode.OpPop, 0, 0)
		}
	}
}

func (c *Compiler) compileNode(n ast.Node) {
	b := c.cur()
	switch v := n.(type) {
	case *ast.IntLit:
		b.emit(bytecode.OpPushConst, b.addConst(object.Integer(v.Value)), 0)
	case *ast.FloatLit:
		b.emit(bytecode.OpPushConst, b.addConst(object.Float(v.Value)), 0)
	case *ast.StringLit:
		b.emit(bytecode.OpPushConst, b.addConst(object.String(v.Value)), 0)
	case *ast.StrInterp:
		// Concatenate each part coerced with to_s onto a growing string.
		b.emit(bytecode.OpPushConst, b.addConst(object.String("")), 0)
		for _, part := range v.Parts {
			c.compileNode(part)
			b.emit(bytecode.OpSend, b.addName("to_s"), 0)
			b.emit(bytecode.OpAdd, 0, 0)
		}
	case *ast.SymbolLit:
		b.emit(bytecode.OpPushConst, b.addConst(object.Symbol(v.Name)), 0)
	case *ast.ArrayLit:
		for _, e := range v.Elems {
			c.compileNode(e)
		}
		b.emit(bytecode.OpNewArray, len(v.Elems), 0)
	case *ast.HashLit:
		for i := range v.Keys {
			c.compileNode(v.Keys[i])
			c.compileNode(v.Values[i])
		}
		b.emit(bytecode.OpNewHash, len(v.Keys), 0)
	case *ast.RangeLit:
		c.compileNode(v.Lo)
		c.compileNode(v.Hi)
		excl := 0
		if v.Exclusive {
			excl = 1
		}
		b.emit(bytecode.OpNewRange, excl, 0)
	case *ast.BoolLit:
		if v.Value {
			b.emit(bytecode.OpPushTrue, 0, 0)
		} else {
			b.emit(bytecode.OpPushFalse, 0, 0)
		}
	case *ast.NilLit:
		b.emit(bytecode.OpPushNil, 0, 0)
	case *ast.SelfLit:
		b.emit(bytecode.OpPushSelf, 0, 0)
	case *ast.VarRef:
		depth, slot, ok := b.resolve(v.Name)
		if !ok {
			c.fail("undefined local variable %q", v.Name)
		}
		b.emit(bytecode.OpGetLocal, slot, depth)
	case *ast.Assign:
		c.compileNode(v.Value)
		// Assign to an enclosing local if one is visible; otherwise create a
		// new local in the current scope.
		if depth, slot, ok := b.resolve(v.Name); ok {
			b.emit(bytecode.OpSetLocal, slot, depth)
		} else {
			b.emit(bytecode.OpSetLocal, b.localSlot(v.Name), 0)
		}
	case *ast.Begin:
		c.compileBegin(v)
	case *ast.Case:
		c.compileCase(v)
	case *ast.OpAssign:
		// Allocate the slot before the read so a fresh `x ||= v` sees nil
		// rather than failing to resolve.
		depth, slot, ok := b.resolve(v.Name)
		if !ok {
			slot, depth = b.localSlot(v.Name), 0
		}
		c.compileNode(&ast.BinaryExpr{Op: v.Op, Left: &ast.VarRef{Name: v.Name}, Right: v.Value})
		b.emit(bytecode.OpSetLocal, slot, depth)
	case *ast.UnaryExpr:
		c.compileNode(v.Operand)
		switch v.Op {
		case "-":
			b.emit(bytecode.OpNeg, 0, 0)
		case "!":
			b.emit(bytecode.OpNot, 0, 0)
		}
	case *ast.BinaryExpr:
		if v.Op == "&&" || v.Op == "||" {
			c.compileLogical(v)
			return
		}
		c.compileNode(v.Left)
		c.compileNode(v.Right)
		if op, ok := fastBinOp(v.Op); ok {
			b.emit(op, 0, 0)
		} else {
			// Operators without a fast-path opcode (e.g. <=>, <<) dispatch as
			// ordinary method calls so user classes can define them. Stack is
			// [recv, arg]: OpSend pops the arg then the receiver.
			b.emit(bytecode.OpSend, b.addName(v.Op), 1)
		}
	case *ast.Call:
		c.compileCall(v)
	case *ast.ConstRef:
		b.emit(bytecode.OpGetConst, b.addName(v.Name), 0)
	case *ast.IvarRef:
		b.emit(bytecode.OpGetIvar, b.addName(v.Name), 0)
	case *ast.IvarAssign:
		c.compileNode(v.Value)
		b.emit(bytecode.OpSetIvar, b.addName(v.Name), 0)
	case *ast.ClassDef:
		c.compileClass(v)
	case *ast.ModuleDef:
		c.compileModule(v)
	case *ast.Super:
		c.compileSuper(v)
	case *ast.Yield:
		for _, a := range v.Args {
			c.compileNode(a)
		}
		b.emit(bytecode.OpInvokeBlock, len(v.Args), 0)
	case *ast.If:
		c.compileIf(v)
	case *ast.While:
		c.compileWhile(v)
	case *ast.MethodDef:
		c.compileMethodDef(v)
	case *ast.Return:
		if v.Value == nil {
			b.emit(bytecode.OpPushNil, 0, 0)
		} else {
			c.compileNode(v.Value)
		}
		b.emit(bytecode.OpReturn, 0, 0)
	case *ast.Break:
		c.compileBreak(v)
	case *ast.Next:
		c.compileNext(v)
	default:
		c.fail("cannot compile %T", n)
	}
}

// compileLogical emits short-circuiting && / ||. The result is the operand that
// decided the outcome (Ruby semantics): `a && b` yields a when a is falsy, else
// b; `a || b` yields a when a is truthy, else b.
func (c *Compiler) compileLogical(v *ast.BinaryExpr) {
	b := c.cur()
	c.compileNode(v.Left)
	b.emit(bytecode.OpDup, 0, 0)
	var short int
	if v.Op == "&&" {
		short = b.emit(bytecode.OpBranchUnless, 0, 0) // left falsy → keep left
	} else {
		short = b.emit(bytecode.OpBranchIf, 0, 0) // left truthy → keep left
	}
	b.emit(bytecode.OpPop, 0, 0) // left decided nothing: drop it, value is right
	c.compileNode(v.Right)
	b.patch(short, b.here())
}

// fastBinOp maps an operator to its fast-path opcode. ok is false for operators
// that must dispatch as method calls (e.g. <=>, <<).
func fastBinOp(op string) (bytecode.Op, bool) {
	switch op {
	case "+":
		return bytecode.OpAdd, true
	case "-":
		return bytecode.OpSub, true
	case "*":
		return bytecode.OpMul, true
	case "/":
		return bytecode.OpDiv, true
	case "%":
		return bytecode.OpMod, true
	case "<":
		return bytecode.OpLt, true
	case ">":
		return bytecode.OpGt, true
	case "<=":
		return bytecode.OpLe, true
	case ">=":
		return bytecode.OpGe, true
	case "==":
		return bytecode.OpEq, true
	case "!=":
		return bytecode.OpNeq, true
	}
	return 0, false
}

func (c *Compiler) compileCall(v *ast.Call) {
	b := c.cur()
	// block_given? is a frame intrinsic, not a real dispatch.
	if v.Recv == nil && v.Block == nil && v.Name == "block_given?" && len(v.Args) == 0 {
		b.emit(bytecode.OpBlockGiven, 0, 0)
		return
	}
	if v.Recv != nil {
		c.compileNode(v.Recv)
	} else {
		b.emit(bytecode.OpPushSelf, 0, 0) // implicit receiver: self
	}
	for _, a := range v.Args {
		c.compileNode(a)
	}
	at := b.emit(bytecode.OpSend, b.addName(v.Name), len(v.Args))
	if v.Block != nil {
		b.insns[at].C = c.compileBlock(v.Block) + 1 // C-1 indexes Children
	}
}

// compileBlock compiles a literal block into a child ISeq of the current
// builder and returns its index. The child is a block builder, so its body can
// reach the enclosing locals by depth.
func (c *Compiler) compileBlock(blk *ast.Block) int {
	parent := c.cur()
	c.push(newBlockBuilder("<block>", blk.Params, parent))
	c.ctxs = append(c.ctxs, &loopCtx{kind: ctxBlock})
	c.compileBody(blk.Body)
	c.ctxs = c.ctxs[:len(c.ctxs)-1]
	c.cur().emit(bytecode.OpReturn, 0, 0)
	child := c.pop().build()
	idx := len(parent.children)
	parent.children = append(parent.children, child)
	return idx
}

func (c *Compiler) compileClass(v *ast.ClassDef) {
	c.push(newBuilder("<class:"+v.Name+">", nil))
	savedCtxs := c.ctxs
	c.ctxs = nil
	c.compileBody(v.Body)
	c.ctxs = savedCtxs
	c.cur().emit(bytecode.OpReturn, 0, 0)
	child := c.pop().build()
	child.Super = v.Super

	parent := c.cur()
	childIdx := len(parent.children)
	parent.children = append(parent.children, child)
	parent.emit(bytecode.OpDefineClass, parent.addName(v.Name), childIdx)
}

func (c *Compiler) compileModule(v *ast.ModuleDef) {
	c.push(newBuilder("<module:"+v.Name+">", nil))
	savedCtxs := c.ctxs
	c.ctxs = nil
	c.compileBody(v.Body)
	c.ctxs = savedCtxs
	c.cur().emit(bytecode.OpReturn, 0, 0)
	child := c.pop().build()

	parent := c.cur()
	childIdx := len(parent.children)
	parent.children = append(parent.children, child)
	parent.emit(bytecode.OpDefineModule, parent.addName(v.Name), childIdx)
}

func (c *Compiler) compileSuper(v *ast.Super) {
	b := c.cur()
	if v.Forward {
		b.emit(bytecode.OpInvokeSuper, 0, 1) // forward the frame's own args
		return
	}
	for _, a := range v.Args {
		c.compileNode(a)
	}
	b.emit(bytecode.OpInvokeSuper, len(v.Args), 0)
}

func (c *Compiler) compileIf(v *ast.If) {
	b := c.cur()
	c.compileNode(v.Cond)
	thisFalse := b.emit(bytecode.OpBranchUnless, 0, 0)
	c.compileBody(v.Then)
	endJumps := []int{b.emit(bytecode.OpJump, 0, 0)}

	for _, ei := range v.Elsifs {
		b.patch(thisFalse, b.here())
		c.compileNode(ei.Cond)
		thisFalse = b.emit(bytecode.OpBranchUnless, 0, 0)
		c.compileBody(ei.Body)
		endJumps = append(endJumps, b.emit(bytecode.OpJump, 0, 0))
	}

	b.patch(thisFalse, b.here()) // last failing cond falls through to else
	if v.Else != nil {
		c.compileBody(v.Else)
	} else {
		b.emit(bytecode.OpPushNil, 0, 0)
	}
	for _, j := range endJumps {
		b.patch(j, b.here())
	}
}

// ctxKind distinguishes a loop (break/next are jumps) from a block (break
// unwinds via OpBreak, next returns from the block frame).
type ctxKind int

const (
	ctxLoop ctxKind = iota
	ctxBlock
)

// loopCtx is one entry on the break/next target stack.
type loopCtx struct {
	kind       ctxKind
	contTarget int   // loop: jump here on `next` (re-evaluate the condition)
	breaks     []int // loop: OpJump placeholders patched to the loop exit
}

func (c *Compiler) innerCtx() *loopCtx {
	if len(c.ctxs) == 0 {
		return nil
	}
	return c.ctxs[len(c.ctxs)-1]
}

func (c *Compiler) compileBreak(v *ast.Break) {
	b := c.cur()
	ctx := c.innerCtx()
	if ctx == nil {
		c.fail("Invalid break")
	}
	if ctx.kind == ctxBlock {
		c.compileBreakValue(v.Value)
		b.emit(bytecode.OpBreak, 0, 0)
		return
	}
	ctx.breaks = append(ctx.breaks, b.emit(bytecode.OpJump, 0, 0))
}

func (c *Compiler) compileNext(v *ast.Next) {
	b := c.cur()
	ctx := c.innerCtx()
	if ctx == nil {
		c.fail("Invalid next")
	}
	if ctx.kind == ctxBlock {
		c.compileBreakValue(v.Value)
		b.emit(bytecode.OpReturn, 0, 0)
		return
	}
	b.emit(bytecode.OpJump, ctx.contTarget, 0)
}

// compileBreakValue pushes a break/next argument, or nil when there is none.
func (c *Compiler) compileBreakValue(val ast.Node) {
	if val != nil {
		c.compileNode(val)
	} else {
		c.cur().emit(bytecode.OpPushNil, 0, 0)
	}
}

// compileCase compiles case/when. With a subject, each when-condition matches
// via `cond === subject` (the subject evaluated once into a hidden slot);
// without one, each condition is tested for truthiness.
func (c *Compiler) compileCase(v *ast.Case) {
	b := c.cur()
	slot := -1
	if v.Subject != nil {
		slot = b.localSlot("")
		c.compileNode(v.Subject)
		b.emit(bytecode.OpSetLocal, slot, 0)
		b.emit(bytecode.OpPop, 0, 0)
	}
	var endJumps []int
	for _, clause := range v.Whens {
		var bodyJumps []int
		for _, cond := range clause.Conds {
			c.compileNode(cond)
			if slot >= 0 { // cond === subject
				b.emit(bytecode.OpGetLocal, slot, 0)
				b.emit(bytecode.OpSend, b.addName("==="), 1)
			}
			bodyJumps = append(bodyJumps, b.emit(bytecode.OpBranchIf, 0, 0))
		}
		skip := b.emit(bytecode.OpJump, 0, 0) // no condition matched → next clause
		for _, j := range bodyJumps {
			b.patch(j, b.here())
		}
		c.compileBody(clause.Body)
		endJumps = append(endJumps, b.emit(bytecode.OpJump, 0, 0))
		b.patch(skip, b.here())
	}
	if v.Else != nil {
		c.compileBody(v.Else)
	} else {
		b.emit(bytecode.OpPushNil, 0, 0)
	}
	for _, j := range endJumps {
		b.patch(j, b.here())
	}
}

// compileBegin compiles begin/rescue/else/ensure. When an ensure clause is
// present it wraps the rescue handling in a second handler that runs ensure on
// both the normal and the propagating paths.
func (c *Compiler) compileBegin(v *ast.Begin) {
	if v.EnsureBody == nil {
		c.compileBeginRescue(v)
		return
	}
	b := c.cur()
	h := b.emit(bytecode.OpPushHandler, 0, 0)
	c.compileBeginRescue(v)
	b.emit(bytecode.OpPopHandler, 0, 0)
	c.compileBody(v.EnsureBody)
	b.emit(bytecode.OpPop, 0, 0) // discard ensure value; the begin value remains
	skip := b.emit(bytecode.OpJump, 0, 0)
	b.patch(h, b.here()) // ENSURE-on-exception: exception is on the stack
	c.compileBody(v.EnsureBody)
	b.emit(bytecode.OpPop, 0, 0)
	b.emit(bytecode.OpReThrow, 0, 0)
	b.patch(skip, b.here())
}

// compileBeginRescue compiles the body + rescue clauses + else (no ensure).
func (c *Compiler) compileBeginRescue(v *ast.Begin) {
	b := c.cur()
	if len(v.Rescues) == 0 {
		c.compileBody(v.Body)
		return
	}
	h := b.emit(bytecode.OpPushHandler, 0, 0)
	c.compileBody(v.Body)
	b.emit(bytecode.OpPopHandler, 0, 0)
	if v.ElseBody != nil { // else runs only with no exception, and is not protected
		b.emit(bytecode.OpPop, 0, 0)
		c.compileBody(v.ElseBody)
	}
	done := []int{b.emit(bytecode.OpJump, 0, 0)}
	b.patch(h, b.here()) // RESCUE: the exception object is on the stack
	for _, clause := range v.Rescues {
		var matched []int
		if len(clause.Classes) == 0 {
			b.emit(bytecode.OpDup, 0, 0)
			b.emit(bytecode.OpGetConst, b.addName("StandardError"), 0)
			b.emit(bytecode.OpSend, b.addName("is_a?"), 1)
			matched = append(matched, b.emit(bytecode.OpBranchIf, 0, 0))
		} else {
			for _, ce := range clause.Classes {
				b.emit(bytecode.OpDup, 0, 0)
				c.compileNode(ce)
				b.emit(bytecode.OpSend, b.addName("is_a?"), 1)
				matched = append(matched, b.emit(bytecode.OpBranchIf, 0, 0))
			}
		}
		skip := b.emit(bytecode.OpJump, 0, 0) // no class matched → next clause
		for _, m := range matched {
			b.patch(m, b.here())
		}
		if clause.Var != "" { // bind the exception (reusing an existing slot)
			if depth, slot, ok := b.resolve(clause.Var); ok {
				b.emit(bytecode.OpSetLocal, slot, depth)
			} else {
				b.emit(bytecode.OpSetLocal, b.localSlot(clause.Var), 0)
			}
		}
		b.emit(bytecode.OpPop, 0, 0) // drop the exception
		c.compileBody(clause.Body)
		done = append(done, b.emit(bytecode.OpJump, 0, 0))
		b.patch(skip, b.here())
	}
	b.emit(bytecode.OpReThrow, 0, 0) // no clause matched
	for _, d := range done {
		b.patch(d, b.here())
	}
}

func (c *Compiler) compileWhile(v *ast.While) {
	b := c.cur()
	start := b.here()
	c.compileNode(v.Cond)
	exit := b.emit(bytecode.OpBranchUnless, 0, 0)
	ctx := &loopCtx{kind: ctxLoop, contTarget: start}
	c.ctxs = append(c.ctxs, ctx)
	c.compileBody(v.Body)
	c.ctxs = c.ctxs[:len(c.ctxs)-1]
	b.emit(bytecode.OpPop, 0, 0) // discard each iteration's value
	b.emit(bytecode.OpJump, start, 0)
	b.patch(exit, b.here())
	for _, j := range ctx.breaks { // break lands on the loop's nil value
		b.patch(j, b.here())
	}
	b.emit(bytecode.OpPushNil, 0, 0) // while evaluates to nil
}

func (c *Compiler) compileMethodDef(v *ast.MethodDef) {
	c.push(newBuilder(v.Name, v.Params))
	savedCtxs := c.ctxs
	c.ctxs = nil
	c.compileBody(v.Body)
	c.ctxs = savedCtxs
	c.cur().emit(bytecode.OpReturn, 0, 0)
	child := c.pop().build()

	parent := c.cur()
	childIdx := len(parent.children)
	parent.children = append(parent.children, child)
	parent.emit(bytecode.OpDefineMethod, parent.addName(v.Name), childIdx)
}
