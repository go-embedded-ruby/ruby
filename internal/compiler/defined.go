package compiler

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser/ast"
)

// compileDefined lowers `defined?(operand)`. Unlike a normal call it does NOT
// evaluate its operand for its value; it inspects the operand's syntactic kind
// and emits code that pushes the matching MRI tag String, or nil, without ever
// raising a NameError. Method/receiver forms (which MRI does evaluate, to test
// the response) run inside an OpDefinedGuard child so an undefined
// sub-expression maps to nil rather than propagating.
func (c *Compiler) compileDefined(operand ast.Node) {
	b := c.cur()
	switch v := operand.(type) {
	case *ast.NilLit:
		c.pushDefinedTag("nil")
	case *ast.BoolLit:
		if v.Value {
			c.pushDefinedTag("true")
		} else {
			c.pushDefinedTag("false")
		}
	case *ast.SelfLit:
		c.pushDefinedTag("self")
	case *ast.Yield:
		b.emit(bytecode.OpDefinedYield, 0, 0)
	case *ast.IvarRef:
		b.emit(bytecode.OpDefinedIvar, b.addName(v.Name), 0)
	case *ast.CVarRef:
		b.emit(bytecode.OpDefinedCVar, b.addName(v.Name), 0)
	case *ast.GVarRef:
		b.emit(bytecode.OpDefinedGVar, b.addName(v.Name), 0)
	case *ast.ConstRef:
		b.emit(bytecode.OpDefinedConst, b.addName(v.Name), 0)
	case *ast.VarRef:
		// The parser already classified this as a known local read.
		c.pushDefinedTag("local-variable")
	case *ast.ScopedConst:
		c.compileDefinedScopedConst(v)
	case *ast.Assign, *ast.OpAssign, *ast.MultiAssign, *ast.ConstAssign,
		*ast.ScopedConstAssign, *ast.IvarAssign, *ast.CVarAssign, *ast.GVarAssign:
		c.pushDefinedTag("assignment")
	case *ast.Call:
		c.compileDefinedCall(v)
	case *ast.BinaryExpr:
		c.compileDefinedBinary(v)
	case *ast.UnaryExpr:
		// `!x` is a method (`!`) on x; `-x`/`~x` dispatch as methods too. MRI tags
		// all of them "method" once the operand is defined.
		c.compileDefinedReceiverMethod(v.Operand, unaryMethodName(v.Op))
	default:
		// Literals (numbers, strings, arrays, ranges, regexps, hashes, …) and any
		// other expression are "expression".
		c.pushDefinedTag("expression")
	}
}

// unaryMethodName maps a unary operator to the method name MRI checks for. Only
// `-` differs (the `-@` unary-minus method); `!` and `~` are their own names.
func unaryMethodName(op string) string {
	if op == "-" {
		return "-@"
	}
	return op
}

// pushDefinedTag pushes a constant tag String.
func (c *Compiler) pushDefinedTag(tag string) {
	b := c.cur()
	b.emit(bytecode.OpPushConst, b.addConst(object.NewString(tag)), 0)
}

// compileDefinedScopedConst handles `defined?(A::B)` and `defined?(::B)`.
// A leading `::B` is just a top-level constant. For `A::B`, the base `A` is
// evaluated (under the guard, so an undefined base yields nil) and probed for
// the trailing constant.
func (c *Compiler) compileDefinedScopedConst(v *ast.ScopedConst) {
	b := c.cur()
	if v.Recv == nil { // leading `::Name`
		b.emit(bytecode.OpDefinedConst, b.addName(v.Name), 0)
		return
	}
	c.guarded(func(gb *builder) {
		c.compileNode(v.Recv)
		gb.emit(bytecode.OpDefinedScopedConst, gb.addName(v.Name), 0)
	})
}

// compileDefinedCall handles `defined?(call)`. A bare name that resolves to a
// local is "local-variable"; otherwise it is a method check on the (possibly
// implicit) receiver, with the receiver and arguments evaluated.
func (c *Compiler) compileDefinedCall(v *ast.Call) {
	b := c.cur()
	if v.Recv == nil && v.Block == nil && len(v.Args) == 0 {
		if _, _, ok := b.resolve(v.Name); ok {
			c.pushDefinedTag("local-variable")
			return
		}
	}
	c.compileDefinedReceiverMethod(v.Recv, v.Name)
}

// compileDefinedBinary handles `defined?(a OP b)`. `&&`/`||` are "expression";
// every other binary operator is a method on the left operand.
func (c *Compiler) compileDefinedBinary(v *ast.BinaryExpr) {
	if v.Op == "&&" || v.Op == "||" {
		c.pushDefinedTag("expression")
		return
	}
	c.compileDefinedReceiverMethod(v.Left, v.Op)
}

// compileDefinedReceiverMethod emits, under the guard, the response probe for
// `recv.name`. recv == nil means an implicit self receiver. An explicit receiver
// is itself subject to defined? first (MRI: `defined?(@x.m)` is nil when `@x` is
// undefined, even though nil responds to m), so the receiver's own defined-check
// short-circuits to nil before it is evaluated for the response test.
func (c *Compiler) compileDefinedReceiverMethod(recv ast.Node, name string) {
	c.guarded(func(gb *builder) {
		if recv == nil {
			gb.emit(bytecode.OpPushSelf, 0, 0)
			gb.emit(bytecode.OpDefinedMethod, gb.addName(name), 0)
			return
		}
		// Inner defined? on the receiver: nil ⇒ whole expression nil.
		c.compileDefined(recv)
		recvUndef := gb.emit(bytecode.OpBranchNil, 0, 0)
		c.compileNode(recv)
		gb.emit(bytecode.OpDefinedMethod, gb.addName(name), 0)
		done := gb.emit(bytecode.OpJump, 0, 0)
		gb.patch(recvUndef, gb.here())
		gb.emit(bytecode.OpPushNil, 0, 0)
		gb.patch(done, gb.here())
	})
}

// guarded compiles body into a child ISeq run under OpDefinedGuard: it shares
// the current scope (so locals/ivars/self/yield resolve as in the enclosing
// frame) and any raise inside maps to nil. The child leaves exactly one value.
func (c *Compiler) guarded(body func(gb *builder)) {
	parent := c.cur()
	c.push(newBlockBuilder("<defined?>", nil, parent))
	gb := c.cur()
	body(gb)
	gb.emit(bytecode.OpReturn, 0, 0)
	child := c.pop().build()
	idx := len(parent.children)
	parent.children = append(parent.children, child)
	parent.emit(bytecode.OpDefinedGuard, idx, 0)
}
