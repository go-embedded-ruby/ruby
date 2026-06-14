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

// builder accumulates a single ISeq under construction.
type builder struct {
	name     string
	insns    []bytecode.Instr
	consts   []object.Value
	constIdx map[object.Value]int
	names    []string
	locals   []string
	params   []string
	children []*bytecode.ISeq
}

func newBuilder(name string, params []string) *builder {
	b := &builder{name: name, constIdx: map[object.Value]int{}, params: params}
	for _, p := range params {
		b.localSlot(p) // params occupy slots 0..n-1, in order
	}
	return b
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

// localSlot returns the slot for name, allocating one if new.
func (b *builder) localSlot(name string) int {
	for i, n := range b.locals {
		if n == name {
			return i
		}
	}
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
		slot, ok := b.localLookup(v.Name)
		if !ok {
			c.fail("undefined local variable %q", v.Name)
		}
		b.emit(bytecode.OpGetLocal, slot, 0)
	case *ast.Assign:
		c.compileNode(v.Value)
		b.emit(bytecode.OpSetLocal, b.localSlot(v.Name), 0)
	case *ast.UnaryExpr:
		c.compileNode(v.Operand)
		switch v.Op {
		case "-":
			b.emit(bytecode.OpNeg, 0, 0)
		case "!":
			b.emit(bytecode.OpNot, 0, 0)
		}
	case *ast.BinaryExpr:
		c.compileNode(v.Left)
		c.compileNode(v.Right)
		b.emit(binOp(v.Op), 0, 0)
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
	default:
		c.fail("cannot compile %T", n)
	}
}

func binOp(op string) bytecode.Op {
	switch op {
	case "+":
		return bytecode.OpAdd
	case "-":
		return bytecode.OpSub
	case "*":
		return bytecode.OpMul
	case "/":
		return bytecode.OpDiv
	case "%":
		return bytecode.OpMod
	case "<":
		return bytecode.OpLt
	case ">":
		return bytecode.OpGt
	case "<=":
		return bytecode.OpLe
	case ">=":
		return bytecode.OpGe
	case "==":
		return bytecode.OpEq
	case "!=":
		return bytecode.OpNeq
	}
	panic(compileError{msg: "unknown binary operator " + op})
}

func (c *Compiler) compileCall(v *ast.Call) {
	b := c.cur()
	if v.Recv != nil {
		c.compileNode(v.Recv)
	} else {
		b.emit(bytecode.OpPushSelf, 0, 0) // implicit receiver: self
	}
	for _, a := range v.Args {
		c.compileNode(a)
	}
	b.emit(bytecode.OpSend, b.addName(v.Name), len(v.Args))
}

func (c *Compiler) compileClass(v *ast.ClassDef) {
	c.push(newBuilder("<class:"+v.Name+">", nil))
	c.compileBody(v.Body)
	c.cur().emit(bytecode.OpReturn, 0, 0)
	child := c.pop().build()
	child.Super = v.Super

	parent := c.cur()
	childIdx := len(parent.children)
	parent.children = append(parent.children, child)
	parent.emit(bytecode.OpDefineClass, parent.addName(v.Name), childIdx)
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

func (c *Compiler) compileWhile(v *ast.While) {
	b := c.cur()
	start := b.here()
	c.compileNode(v.Cond)
	exit := b.emit(bytecode.OpBranchUnless, 0, 0)
	c.compileBody(v.Body)
	b.emit(bytecode.OpPop, 0, 0) // discard each iteration's value
	b.emit(bytecode.OpJump, start, 0)
	b.patch(exit, b.here())
	b.emit(bytecode.OpPushNil, 0, 0) // while evaluates to nil
}

func (c *Compiler) compileMethodDef(v *ast.MethodDef) {
	c.push(newBuilder(v.Name, v.Params))
	c.compileBody(v.Body)
	c.cur().emit(bytecode.OpReturn, 0, 0)
	child := c.pop().build()

	parent := c.cur()
	childIdx := len(parent.children)
	parent.children = append(parent.children, child)
	parent.emit(bytecode.OpDefineMethod, parent.addName(v.Name), childIdx)
}
