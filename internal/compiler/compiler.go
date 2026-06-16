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
	params      []string
	numRequired int
	splatIndex  int
	kwNames     []string
	kwRequired  []bool
	kwRestSlot  int
	blockSlot   int
	children []*bytecode.ISeq
	parent   *builder
	isBlock  bool
}

func newBuilder(name string, params []string) *builder {
	b := &builder{name: name, constIdx: map[object.Value]int{}, params: params, numRequired: len(params), splatIndex: -1, kwRestSlot: -1, blockSlot: -1}
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
		Params:      b.params,
		NumRequired: b.numRequired,
		SplatIndex:  b.splatIndex,
		KwNames:     b.kwNames,
		KwRequired:  b.kwRequired,
		KwRestSlot:  b.kwRestSlot,
		BlockSlot:   b.blockSlot,
		NumLocals: len(b.locals),
		Children:  b.children,
	}
}

// Compiler walks the AST, maintaining a stack of builders.
type Compiler struct {
	ctxs         []*loopCtx // innermost-last stack of break/next targets
	retryTargets []int      // innermost-last stack of begin-body PCs for `retry`
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
	case *ast.BignumLit:
		b.emit(bytecode.OpPushConst, b.addConst(&object.Bignum{I: v.Val}), 0)
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
	case *ast.RegexpLit:
		// The source and flags travel in the name pool; the VM compiles the
		// pattern (translating the Ruby flags to an inline prefix) at runtime.
		b.emit(bytecode.OpRegexp, b.addName(v.Source), b.addName(v.Flags))
	case *ast.ArrayLit:
		if hasSplat(v.Elems) {
			c.compileSplatItems(v.Elems)
		} else {
			for _, e := range v.Elems {
				c.compileNode(e)
			}
			b.emit(bytecode.OpNewArray, len(v.Elems), 0)
		}
	case *ast.HashLit:
		if hasHashSplat(v) {
			c.compileHashWithSplat(v)
			break
		}
		for i := range v.Keys {
			c.compileNode(v.Keys[i])
			c.compileNode(v.Values[i])
		}
		b.emit(bytecode.OpNewHash, len(v.Keys), 0)
	case *ast.RangeLit:
		c.compileRangeEnd(v.Lo) // nil → beginless/endless bound
		c.compileRangeEnd(v.Hi)
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
	case *ast.MultiAssign:
		// Build the right-hand side as one Array (a single value is splatted into
		// its elements; multiple values are collected).
		if len(v.Values) == 1 {
			c.compileNode(v.Values[0])
			b.emit(bytecode.OpSplatToArray, 0, 0)
		} else {
			for _, val := range v.Values {
				c.compileNode(val)
			}
			b.emit(bytecode.OpNewArray, len(v.Values), 0)
		}
		b.emit(bytecode.OpDup, 0, 0) // keep the array as the expression's value
		pre, post, splat := len(v.Names), 0, 0
		if v.SplatIndex >= 0 {
			pre = v.SplatIndex
			post = len(v.Names) - v.SplatIndex - 1
			splat = 1
		}
		at := b.emit(bytecode.OpExpandArray, pre, post)
		b.insns[at].C = splat
		// OpExpandArray pushed the target values with Names[0]'s on top; store
		// each (OpSetLocal leaves the value, so pop it).
		for _, name := range v.Names {
			if depth, slot, ok := b.resolve(name); ok {
				b.emit(bytecode.OpSetLocal, slot, depth)
			} else {
				b.emit(bytecode.OpSetLocal, b.localSlot(name), 0)
			}
			b.emit(bytecode.OpPop, 0, 0)
		}
	case *ast.Begin:
		c.compileBegin(v)
	case *ast.Case:
		c.compileCase(v)
	case *ast.CaseIn:
		c.compileCaseIn(v)
	case *ast.MatchPattern:
		// One-line match: store the subject, test it against the pattern.
		subj := b.localSlot("")
		c.compileNode(v.Subject)
		b.emit(bytecode.OpSetLocal, subj, 0)
		b.emit(bytecode.OpPop, 0, 0)
		c.compilePattern(v.Pattern, subj) // leaves a boolean
		if v.Bool {
			break // `in`: the boolean is the result
		}
		// `=>`: nil on success, NoMatchingPatternError on failure.
		ok := b.emit(bytecode.OpBranchIf, 0, 0)
		b.emit(bytecode.OpGetLocal, subj, 0)
		b.emit(bytecode.OpRaiseNoMatch, 0, 0)
		b.patch(ok, b.here())
		b.emit(bytecode.OpPushNil, 0, 0)
	case *ast.Retry:
		if len(c.retryTargets) == 0 {
			c.fail("Invalid retry")
		}
		b.emit(bytecode.OpJump, c.retryTargets[len(c.retryTargets)-1], 0)
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
	case *ast.ConstAssign:
		c.compileNode(v.Value)
		b.emit(bytecode.OpSetConst, b.addName(v.Name), 0)
	case *ast.GVarRef:
		b.emit(bytecode.OpGetGVar, b.addName(v.Name), 0)
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
	// Safe navigation (recv&.m): if the receiver is nil, skip the send and leave
	// nil as the result. The guard branches past whichever send is emitted below.
	safeBranch := -1
	if v.Safe {
		b.emit(bytecode.OpDup, 0, 0)
		safeBranch = b.emit(bytecode.OpBranchNil, 0, 0)
	}
	patchSafe := func() {
		if safeBranch >= 0 {
			b.patch(safeBranch, b.here())
		}
	}
	// A trailing `&expr` block-pass is carried as the last arg; pull it out so
	// the ordinary args compile cleanly and the block value lands on top.
	args := v.Args
	var blockPass ast.Node
	if n := len(args); n > 0 {
		if bp, ok := args[n-1].(*ast.BlockPass); ok {
			blockPass = bp.Value
			args = args[:n-1]
		}
	}
	if hasSplat(args) { // dynamic argument count: build an array and splat-send
		c.compileSplatItems(args)
		if blockPass != nil {
			c.compileNode(blockPass)
			b.emit(bytecode.OpSendArrayBlockArg, b.addName(v.Name), 0)
			patchSafe()
			return
		}
		at := b.emit(bytecode.OpSendArray, b.addName(v.Name), 0)
		if v.Block != nil {
			b.insns[at].C = c.compileBlock(v.Block) + 1
		}
		patchSafe()
		return
	}
	for _, a := range args {
		c.compileNode(a)
	}
	if blockPass != nil {
		c.compileNode(blockPass)
		b.emit(bytecode.OpSendBlockArg, b.addName(v.Name), len(args))
		patchSafe()
		return
	}
	at := b.emit(bytecode.OpSend, b.addName(v.Name), len(args))
	if v.Block != nil {
		b.insns[at].C = c.compileBlock(v.Block) + 1 // C-1 indexes Children
	}
	patchSafe()
}

// hasSplat reports whether any item is a *splat.
func hasSplat(items []ast.Node) bool {
	for _, it := range items {
		if _, ok := it.(*ast.SplatArg); ok {
			return true
		}
	}
	return false
}

// compileSplatItems leaves one Array on the stack holding all items, with
// *splat elements spliced in.
func (c *Compiler) compileSplatItems(items []ast.Node) {
	b := c.cur()
	b.emit(bytecode.OpNewArray, 0, 0) // accumulator
	for _, it := range items {
		if sp, ok := it.(*ast.SplatArg); ok {
			c.compileNode(sp.Value)
			b.emit(bytecode.OpSplatToArray, 0, 0)
		} else {
			c.compileNode(it)
			b.emit(bytecode.OpNewArray, 1, 0)
		}
		b.emit(bytecode.OpConcatArray, 0, 0)
	}
}

// hasHashSplat reports whether any hash entry is a **splat (key == nil).
func hasHashSplat(v *ast.HashLit) bool {
	for _, k := range v.Keys {
		if k == nil {
			return true
		}
	}
	return false
}

// compileHashWithSplat builds a Hash incrementally so that **splat entries
// (key == nil) merge their hash in order, matching Ruby's last-wins semantics.
func (c *Compiler) compileHashWithSplat(v *ast.HashLit) {
	b := c.cur()
	b.emit(bytecode.OpNewHash, 0, 0) // accumulator
	for i, k := range v.Keys {
		if k == nil { // **splat
			c.compileNode(v.Values[i])
			b.emit(bytecode.OpHashMerge, 0, 0)
			continue
		}
		c.compileNode(k)
		c.compileNode(v.Values[i])
		b.emit(bytecode.OpHashSetPair, 0, 0)
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

// compileRangeEnd pushes a range endpoint, or nil for a beginless/endless bound.
func (c *Compiler) compileRangeEnd(end ast.Node) {
	if end != nil {
		c.compileNode(end)
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

// compileCaseIn compiles pattern matching (`case … in …`). The subject is
// evaluated once into a hidden slot; each clause tests its pattern (and guard)
// against that slot, branching to the clause body on a match. When no clause
// matches and there is no else, a NoMatchingPatternError is raised.
func (c *Compiler) compileCaseIn(v *ast.CaseIn) {
	b := c.cur()
	subj := b.localSlot("")
	c.compileNode(v.Subject)
	b.emit(bytecode.OpSetLocal, subj, 0)
	b.emit(bytecode.OpPop, 0, 0)

	var endJumps []int
	for _, clause := range v.Clauses {
		c.compilePattern(clause.Pattern, subj)
		if clause.Guard != nil {
			// Pattern match AND guard: only evaluate the guard when the pattern
			// matched (the pattern's bindings are visible to the guard).
			ok := b.emit(bytecode.OpBranchUnless, 0, 0)
			c.compileNode(clause.Guard)
			if clause.GuardNeg {
				b.emit(bytecode.OpNot, 0, 0)
			}
			matched := b.emit(bytecode.OpBranchIf, 0, 0)
			skipNoGuard := b.emit(bytecode.OpJump, 0, 0)
			b.patch(ok, b.here())
			// Pattern failed → fall through to the next clause.
			noMatch := b.emit(bytecode.OpJump, 0, 0)
			b.patch(matched, b.here())
			c.compileBody(clause.Body)
			endJumps = append(endJumps, b.emit(bytecode.OpJump, 0, 0))
			b.patch(skipNoGuard, b.here())
			b.patch(noMatch, b.here())
			continue
		}
		skip := b.emit(bytecode.OpBranchUnless, 0, 0)
		c.compileBody(clause.Body)
		endJumps = append(endJumps, b.emit(bytecode.OpJump, 0, 0))
		b.patch(skip, b.here())
	}
	if v.Else != nil {
		c.compileBody(v.Else)
	} else {
		// No clause matched and no else: raise NoMatchingPatternError, inspecting
		// the subject for the message (MRI reports the subject).
		b.emit(bytecode.OpGetLocal, subj, 0)
		b.emit(bytecode.OpRaiseNoMatch, 0, 0)
	}
	for _, j := range endJumps {
		b.patch(j, b.here())
	}
}

// compilePattern emits code that tests the value in local slot subj against pat
// and leaves a single boolean (match success) on the stack, performing variable
// bindings as a side effect.
func (c *Compiler) compilePattern(pat ast.Pattern, subj int) {
	b := c.cur()
	switch p := pat.(type) {
	case *ast.BindPattern:
		// Bind the whole subject; always matches.
		b.emit(bytecode.OpGetLocal, subj, 0)
		c.storeLocal(p.Name)
		b.emit(bytecode.OpPop, 0, 0)
		b.emit(bytecode.OpPushTrue, 0, 0)
	case *ast.ValuePattern:
		// value === subject
		c.compileNode(p.Value)
		b.emit(bytecode.OpGetLocal, subj, 0)
		b.emit(bytecode.OpSend, b.addName("==="), 1)
		b.emit(bytecode.OpTruthy, 0, 0)
	case *ast.ConstPattern:
		// subject.is_a?(Const)
		b.emit(bytecode.OpGetLocal, subj, 0)
		c.compileNode(p.Const)
		b.emit(bytecode.OpSend, b.addName("is_a?"), 1)
		b.emit(bytecode.OpTruthy, 0, 0)
	case *ast.BindingPattern:
		// Match the sub-pattern, then bind the subject to name on success.
		c.compilePattern(p.Sub, subj)
		skip := b.emit(bytecode.OpBranchUnless, 0, 0)
		b.emit(bytecode.OpGetLocal, subj, 0)
		c.storeLocal(p.Name)
		b.emit(bytecode.OpPop, 0, 0)
		b.emit(bytecode.OpPushTrue, 0, 0)
		end := b.emit(bytecode.OpJump, 0, 0)
		b.patch(skip, b.here())
		b.emit(bytecode.OpPushFalse, 0, 0)
		b.patch(end, b.here())
	case *ast.ArrayPattern:
		c.compileArrayPattern(p, subj)
	case *ast.HashPattern:
		c.compileHashPattern(p, subj)
	case *ast.FindPattern:
		c.compileFindPattern(p, subj)
	case *ast.AltPattern:
		// Alternative: true if any branch matches (short-circuit on the first).
		var hits []int
		for _, alt := range p.Alts {
			c.compilePattern(alt, subj)
			hits = append(hits, b.emit(bytecode.OpBranchIf, 0, 0))
		}
		b.emit(bytecode.OpPushFalse, 0, 0)
		done := b.emit(bytecode.OpJump, 0, 0)
		for _, h := range hits {
			b.patch(h, b.here())
		}
		b.emit(bytecode.OpPushTrue, 0, 0)
		b.patch(done, b.here())
	default:
		c.fail("cannot compile pattern %T", pat)
	}
}

// compileArrayPattern emits the deconstruct-protocol match for an array pattern.
// It checks the optional constant, that the subject responds to :deconstruct,
// the resulting Array's length, then each element against its sub-pattern.
func (c *Compiler) compileArrayPattern(p *ast.ArrayPattern, subj int) {
	b := c.cur()
	arr := b.localSlot("") // the deconstructed Array
	// Result accumulator: start true, AND each test, short-circuiting via jumps
	// to a shared failure label.
	var fails []int
	// Optional constant guard: subject.is_a?(Const).
	if p.Const != nil {
		b.emit(bytecode.OpGetLocal, subj, 0)
		c.compileNode(p.Const)
		b.emit(bytecode.OpSend, b.addName("is_a?"), 1)
		fails = append(fails, b.emit(bytecode.OpBranchUnless, 0, 0))
	}
	// respond_to?(:deconstruct)
	b.emit(bytecode.OpGetLocal, subj, 0)
	b.emit(bytecode.OpPushConst, b.addConst(object.Symbol("deconstruct")), 0)
	b.emit(bytecode.OpSend, b.addName("respond_to?"), 1)
	fails = append(fails, b.emit(bytecode.OpBranchUnless, 0, 0))
	// arr = subject.deconstruct
	b.emit(bytecode.OpGetLocal, subj, 0)
	b.emit(bytecode.OpSend, b.addName("deconstruct"), 0)
	b.emit(bytecode.OpSetLocal, arr, 0)
	b.emit(bytecode.OpPop, 0, 0)
	// Length check: == (pre+post) without splat, >= (pre+post) with.
	b.emit(bytecode.OpGetLocal, arr, 0)
	b.emit(bytecode.OpSend, b.addName("length"), 0)
	b.emit(bytecode.OpPushConst, b.addConst(object.Integer(int64(len(p.Pre)+len(p.Post)))), 0)
	if p.HasSplat {
		b.emit(bytecode.OpGe, 0, 0)
	} else {
		b.emit(bytecode.OpEq, 0, 0)
	}
	fails = append(fails, b.emit(bytecode.OpBranchUnless, 0, 0))
	// Pre elements: arr[i].
	for i, sub := range p.Pre {
		elem := b.localSlot("")
		b.emit(bytecode.OpGetLocal, arr, 0)
		b.emit(bytecode.OpPushConst, b.addConst(object.Integer(int64(i))), 0)
		b.emit(bytecode.OpSend, b.addName("[]"), 1)
		b.emit(bytecode.OpSetLocal, elem, 0)
		b.emit(bytecode.OpPop, 0, 0)
		c.compilePattern(sub, elem)
		fails = append(fails, b.emit(bytecode.OpBranchUnless, 0, 0))
	}
	// Post elements: arr[length-post+i], computed at runtime.
	for i, sub := range p.Post {
		elem := b.localSlot("")
		b.emit(bytecode.OpGetLocal, arr, 0)
		// index = arr.length - post + i
		b.emit(bytecode.OpGetLocal, arr, 0)
		b.emit(bytecode.OpSend, b.addName("length"), 0)
		b.emit(bytecode.OpPushConst, b.addConst(object.Integer(int64(len(p.Post)-i))), 0)
		b.emit(bytecode.OpSub, 0, 0)
		b.emit(bytecode.OpSend, b.addName("[]"), 1)
		b.emit(bytecode.OpSetLocal, elem, 0)
		b.emit(bytecode.OpPop, 0, 0)
		c.compilePattern(sub, elem)
		fails = append(fails, b.emit(bytecode.OpBranchUnless, 0, 0))
	}
	// Splat capture: arr[pre...length-post] bound to SplatName (if named).
	if p.HasSplat && p.SplatName != "" {
		b.emit(bytecode.OpGetLocal, arr, 0)
		b.emit(bytecode.OpPushConst, b.addConst(object.Integer(int64(len(p.Pre)))), 0)
		// length - post
		b.emit(bytecode.OpGetLocal, arr, 0)
		b.emit(bytecode.OpSend, b.addName("length"), 0)
		b.emit(bytecode.OpPushConst, b.addConst(object.Integer(int64(len(p.Post)))), 0)
		b.emit(bytecode.OpSub, 0, 0)
		b.emit(bytecode.OpNewRange, 1, 0) // exclusive: pre...(length-post)
		b.emit(bytecode.OpSend, b.addName("[]"), 1)
		c.storeLocal(p.SplatName)
		b.emit(bytecode.OpPop, 0, 0)
	}
	// All checks passed.
	b.emit(bytecode.OpPushTrue, 0, 0)
	end := b.emit(bytecode.OpJump, 0, 0)
	for _, f := range fails {
		b.patch(f, b.here())
	}
	b.emit(bytecode.OpPushFalse, 0, 0)
	b.patch(end, b.here())
}

// compileHashPattern emits the deconstruct_keys-protocol match for a hash
// pattern. It checks the optional constant, that the subject responds to
// :deconstruct_keys, then for each key that it is present and its value matches
// the sub-pattern (or binds it). `**nil` forbids extra keys; `**name` captures
// them.
// compileFindPattern emits the array find pattern `[*pre, mid…, *post]`: it
// deconstructs the subject, then scans for the first window where every Mid
// matches, binding pre/post. It leaves a boolean (matched) on the stack.
func (c *Compiler) compileFindPattern(p *ast.FindPattern, subj int) {
	b := c.cur()
	arr := b.localSlot("")
	var fails []int
	if p.Const != nil {
		b.emit(bytecode.OpGetLocal, subj, 0)
		c.compileNode(p.Const)
		b.emit(bytecode.OpSend, b.addName("is_a?"), 1)
		fails = append(fails, b.emit(bytecode.OpBranchUnless, 0, 0))
	}
	// respond_to?(:deconstruct), then arr = subject.deconstruct.
	b.emit(bytecode.OpGetLocal, subj, 0)
	b.emit(bytecode.OpPushConst, b.addConst(object.Symbol("deconstruct")), 0)
	b.emit(bytecode.OpSend, b.addName("respond_to?"), 1)
	fails = append(fails, b.emit(bytecode.OpBranchUnless, 0, 0))
	b.emit(bytecode.OpGetLocal, subj, 0)
	b.emit(bytecode.OpSend, b.addName("deconstruct"), 0)
	b.emit(bytecode.OpSetLocal, arr, 0)
	b.emit(bytecode.OpPop, 0, 0)
	// n = arr.length
	n := b.localSlot("")
	b.emit(bytecode.OpGetLocal, arr, 0)
	b.emit(bytecode.OpSend, b.addName("length"), 0)
	b.emit(bytecode.OpSetLocal, n, 0)
	b.emit(bytecode.OpPop, 0, 0)
	k := len(p.Mid)
	// i = 0; matched = false
	i := b.localSlot("")
	matched := b.localSlot("")
	c.setLocalInt(i, 0)
	b.emit(bytecode.OpPushFalse, 0, 0)
	b.emit(bytecode.OpSetLocal, matched, 0)
	b.emit(bytecode.OpPop, 0, 0)
	// loop while i + k <= n
	loopTop := b.here()
	b.emit(bytecode.OpGetLocal, i, 0)
	b.emit(bytecode.OpPushConst, b.addConst(object.Integer(int64(k))), 0)
	b.emit(bytecode.OpAdd, 0, 0)
	b.emit(bytecode.OpGetLocal, n, 0)
	b.emit(bytecode.OpLe, 0, 0)
	loopExhausted := b.emit(bytecode.OpBranchUnless, 0, 0)
	// Test the window arr[i .. i+k): every Mid must match.
	var winFails []int
	for j, mid := range p.Mid {
		elem := b.localSlot("")
		b.emit(bytecode.OpGetLocal, arr, 0)
		b.emit(bytecode.OpGetLocal, i, 0)
		b.emit(bytecode.OpPushConst, b.addConst(object.Integer(int64(j))), 0)
		b.emit(bytecode.OpAdd, 0, 0)
		b.emit(bytecode.OpSend, b.addName("[]"), 1)
		b.emit(bytecode.OpSetLocal, elem, 0)
		b.emit(bytecode.OpPop, 0, 0)
		c.compilePattern(mid, elem)
		winFails = append(winFails, b.emit(bytecode.OpBranchUnless, 0, 0))
	}
	// Window matched: bind pre = arr[0...i] and post = arr[(i+k)...n].
	if p.PreName != "" {
		b.emit(bytecode.OpGetLocal, arr, 0)
		b.emit(bytecode.OpPushConst, b.addConst(object.Integer(0)), 0)
		b.emit(bytecode.OpGetLocal, i, 0)
		b.emit(bytecode.OpNewRange, 1, 0)
		b.emit(bytecode.OpSend, b.addName("[]"), 1)
		c.storeLocal(p.PreName)
		b.emit(bytecode.OpPop, 0, 0)
	}
	if p.PostName != "" {
		b.emit(bytecode.OpGetLocal, arr, 0)
		b.emit(bytecode.OpGetLocal, i, 0)
		b.emit(bytecode.OpPushConst, b.addConst(object.Integer(int64(k))), 0)
		b.emit(bytecode.OpAdd, 0, 0)
		b.emit(bytecode.OpGetLocal, n, 0)
		b.emit(bytecode.OpNewRange, 1, 0)
		b.emit(bytecode.OpSend, b.addName("[]"), 1)
		c.storeLocal(p.PostName)
		b.emit(bytecode.OpPop, 0, 0)
	}
	b.emit(bytecode.OpPushTrue, 0, 0)
	b.emit(bytecode.OpSetLocal, matched, 0)
	b.emit(bytecode.OpPop, 0, 0)
	doneFromMatch := b.emit(bytecode.OpJump, 0, 0)
	// Window failed: advance i and retry.
	for _, wf := range winFails {
		b.patch(wf, b.here())
	}
	b.emit(bytecode.OpGetLocal, i, 0)
	b.emit(bytecode.OpPushConst, b.addConst(object.Integer(1)), 0)
	b.emit(bytecode.OpAdd, 0, 0)
	b.emit(bytecode.OpSetLocal, i, 0)
	b.emit(bytecode.OpPop, 0, 0)
	b.patch(b.emit(bytecode.OpJump, 0, 0), loopTop)
	// Loop finished (exhausted or matched).
	b.patch(loopExhausted, b.here())
	b.patch(doneFromMatch, b.here())
	b.emit(bytecode.OpGetLocal, matched, 0)
	endJump := b.emit(bytecode.OpJump, 0, 0)
	for _, f := range fails {
		b.patch(f, b.here())
	}
	b.emit(bytecode.OpPushFalse, 0, 0)
	b.patch(endJump, b.here())
}

// setLocalInt sets a local slot to an integer constant (and discards the value).
func (c *Compiler) setLocalInt(slot int, v int64) {
	b := c.cur()
	b.emit(bytecode.OpPushConst, b.addConst(object.Integer(v)), 0)
	b.emit(bytecode.OpSetLocal, slot, 0)
	b.emit(bytecode.OpPop, 0, 0)
}

func (c *Compiler) compileHashPattern(p *ast.HashPattern, subj int) {
	b := c.cur()
	h := b.localSlot("") // the deconstructed Hash
	var fails []int
	// respond_to?(:deconstruct_keys)
	b.emit(bytecode.OpGetLocal, subj, 0)
	b.emit(bytecode.OpPushConst, b.addConst(object.Symbol("deconstruct_keys")), 0)
	b.emit(bytecode.OpSend, b.addName("respond_to?"), 1)
	fails = append(fails, b.emit(bytecode.OpBranchUnless, 0, 0))
	// h = subject.deconstruct_keys(nil) — we pass nil (full hash) for simplicity,
	// which is always a valid request under the protocol.
	b.emit(bytecode.OpGetLocal, subj, 0)
	b.emit(bytecode.OpPushNil, 0, 0)
	b.emit(bytecode.OpSend, b.addName("deconstruct_keys"), 1)
	b.emit(bytecode.OpSetLocal, h, 0)
	b.emit(bytecode.OpPop, 0, 0)
	// `**nil`, or the empty pattern `{}`: the hash must have exactly the named
	// keys and no extras (an empty `{}` thus matches only an empty hash).
	if p.RestNil || (len(p.Keys) == 0 && !p.HasRest) {
		b.emit(bytecode.OpGetLocal, h, 0)
		b.emit(bytecode.OpSend, b.addName("size"), 0)
		b.emit(bytecode.OpPushConst, b.addConst(object.Integer(int64(len(p.Keys)))), 0)
		b.emit(bytecode.OpEq, 0, 0)
		fails = append(fails, b.emit(bytecode.OpBranchUnless, 0, 0))
	}
	for i, key := range p.Keys {
		// h.key?(:key)
		b.emit(bytecode.OpGetLocal, h, 0)
		b.emit(bytecode.OpPushConst, b.addConst(object.Symbol(key)), 0)
		b.emit(bytecode.OpSend, b.addName("key?"), 1)
		fails = append(fails, b.emit(bytecode.OpBranchUnless, 0, 0))
		// value = h[:key]
		if p.Values[i] == nil {
			// Shorthand `{key:}` binds local key to the value.
			b.emit(bytecode.OpGetLocal, h, 0)
			b.emit(bytecode.OpPushConst, b.addConst(object.Symbol(key)), 0)
			b.emit(bytecode.OpSend, b.addName("[]"), 1)
			c.storeLocal(key)
			b.emit(bytecode.OpPop, 0, 0)
		} else {
			elem := b.localSlot("")
			b.emit(bytecode.OpGetLocal, h, 0)
			b.emit(bytecode.OpPushConst, b.addConst(object.Symbol(key)), 0)
			b.emit(bytecode.OpSend, b.addName("[]"), 1)
			b.emit(bytecode.OpSetLocal, elem, 0)
			b.emit(bytecode.OpPop, 0, 0)
			c.compilePattern(p.Values[i], elem)
			fails = append(fails, b.emit(bytecode.OpBranchUnless, 0, 0))
		}
	}
	// `**rest` captures the keys not named in the pattern: h.except(:k1, …).
	if p.HasRest && p.RestName != "" {
		b.emit(bytecode.OpGetLocal, h, 0)
		for _, key := range p.Keys {
			b.emit(bytecode.OpPushConst, b.addConst(object.Symbol(key)), 0)
		}
		b.emit(bytecode.OpSend, b.addName("except"), len(p.Keys))
		c.storeLocal(p.RestName)
		b.emit(bytecode.OpPop, 0, 0)
	}
	b.emit(bytecode.OpPushTrue, 0, 0)
	end := b.emit(bytecode.OpJump, 0, 0)
	for _, f := range fails {
		b.patch(f, b.here())
	}
	b.emit(bytecode.OpPushFalse, 0, 0)
	b.patch(end, b.here())
}

// storeLocal emits a SetLocal for name, resolving an existing local or
// allocating a fresh slot, mirroring ast.Assign.
func (c *Compiler) storeLocal(name string) {
	b := c.cur()
	if depth, slot, ok := b.resolve(name); ok {
		b.emit(bytecode.OpSetLocal, slot, depth)
	} else {
		b.emit(bytecode.OpSetLocal, b.localSlot(name), 0)
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
	c.retryTargets = append(c.retryTargets, h) // `retry` re-enters the begin body
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
	c.retryTargets = c.retryTargets[:len(c.retryTargets)-1]
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
	b := c.cur()
	b.splatIndex = v.SplatIndex
	// Keyword params get local slots right after the positionals, so the body
	// resolves them by name; record their names/required flags for the VM.
	kwBase := len(v.Params)
	for _, kp := range v.KwParams {
		b.localSlot(kp.Name)
		b.kwNames = append(b.kwNames, kp.Name)
		b.kwRequired = append(b.kwRequired, kp.Default == nil)
	}
	if v.KwRest != "" {
		b.kwRestSlot = b.localSlot(v.KwRest)
	}
	if v.BlockParam != "" {
		b.blockSlot = b.localSlot(v.BlockParam)
	}
	nreq := len(v.Params)
	if v.SplatIndex >= 0 {
		nreq = v.SplatIndex // the splat and anything after it are not required
	}
	for i, d := range v.Defaults {
		if d == nil {
			continue
		}
		if i < nreq {
			nreq = i // first optional param marks the required count
		}
		// if argument i was not supplied, evaluate its default into the slot
		b.emit(bytecode.OpArgGiven, i, 0)
		skip := b.emit(bytecode.OpBranchIf, 0, 0)
		c.compileNode(d)
		b.emit(bytecode.OpSetLocal, i, 0)
		b.emit(bytecode.OpPop, 0, 0)
		b.patch(skip, b.here())
	}
	b.numRequired = nreq
	// Optional keyword params: the VM binds the supplied ones natively, so the
	// prologue only fills in defaults for the absent ones.
	for i, kp := range v.KwParams {
		if kp.Default == nil {
			continue
		}
		b.emit(bytecode.OpKwGiven, i, 0)
		skip := b.emit(bytecode.OpBranchIf, 0, 0)
		c.compileNode(kp.Default)
		b.emit(bytecode.OpSetLocal, kwBase+i, 0)
		b.emit(bytecode.OpPop, 0, 0)
		b.patch(skip, b.here())
	}
	savedCtxs := c.ctxs
	c.ctxs = nil
	c.compileBody(v.Body)
	c.ctxs = savedCtxs
	c.cur().emit(bytecode.OpReturn, 0, 0)
	child := c.pop().build()

	parent := c.cur()
	childIdx := len(parent.children)
	parent.children = append(parent.children, child)
	op := bytecode.OpDefineMethod
	if v.Singleton {
		op = bytecode.OpDefineSMethod
	}
	parent.emit(op, parent.addName(v.Name), childIdx)
}
