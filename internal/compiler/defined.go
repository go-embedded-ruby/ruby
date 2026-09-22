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
	c.declareDefinedLocals(operand)
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
		// A compound assignment to an attribute or an index (`a.b += 1`,
		// `a[:b] ||= 1`) reaches the compiler as the parser's textual desugaring
		// into a setter Call, but MRI tags it "assignment", not "method":
		// PM_CALL_OPERATOR_WRITE_NODE / PM_CALL_OR_WRITE_NODE /
		// PM_CALL_AND_WRITE_NODE and their PM_INDEX_* counterparts are all in the
		// DEFINED_ASGN group (prism_compile.c v3_4_0:4052-4093, matching
		// compile.c's NODE_OP_ASGN1/NODE_OP_ASGN2 at v3_4_0:6151-6164). A PLAIN
		// `a[0] = 1` is not in that group — it is an ordinary attrasgn call, and
		// stays "method". The same shared-node recognisers compileCall uses tell
		// the two apart; see opassign.go for why identity is the test.
		if isSetterCall(v) {
			if _, _, ok := indexOpAssign(v); ok {
				c.pushDefinedTag("assignment")
				return
			}
			if _, ok := attrOpAssign(v); ok {
				c.pushDefinedTag("assignment")
				return
			}
		}
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
		b.emit(bytecode.OpDefinedConstTop, b.addName(v.Name), 0)
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

// declareDefinedLocals declares the locals an operand of `defined?` introduces.
//
// `defined?` inspects its operand's syntactic kind instead of compiling it, so
// the assignments inside it emit no code — but they still DECLARE. MRI builds
// its local table in the parser, which walks the operand like any other code,
// so `defined?(a += 1)` leaves `a` as a (nil) local of the enclosing scope and a
// later bare `a` is a local-variable read rather than a method call. rbgo
// declares locals as it compiles, so without this walk the name has no slot and
// the next mention of it fails to compile — which is where
// language/defined_spec.rb stopped.
//
// Only assignment targets are declared; nothing is evaluated. The walk descends
// through the value and receiver/argument positions an assignment can hide in,
// which is what the parser's own walk amounts to.
func (c *Compiler) declareDefinedLocals(n ast.Node) {
	switch v := n.(type) {
	case *ast.Assign:
		c.declareLocal(v.Name)
		c.declareDefinedLocals(v.Value)
	case *ast.OpAssign:
		c.declareLocal(v.Name)
		c.declareDefinedLocals(v.Value)
	case *ast.MultiAssign:
		for _, name := range v.Names {
			c.declareLocal(name)
		}
		for _, t := range v.Targets {
			c.declareDefinedLocals(t) // a nested group declares its own names
		}
		for _, val := range v.Values {
			c.declareDefinedLocals(val)
		}
	case *ast.IvarAssign:
		c.declareDefinedLocals(v.Value)
	case *ast.CVarAssign:
		c.declareDefinedLocals(v.Value)
	case *ast.GVarAssign:
		c.declareDefinedLocals(v.Value)
	case *ast.ConstAssign:
		c.declareDefinedLocals(v.Value)
	case *ast.ScopedConstAssign:
		c.declareDefinedLocals(v.Value)
	case *ast.Call:
		if v.Recv != nil {
			c.declareDefinedLocals(v.Recv)
		}
		for _, a := range v.Args {
			c.declareDefinedLocals(a)
		}
	case *ast.BinaryExpr:
		c.declareDefinedLocals(v.Left)
		c.declareDefinedLocals(v.Right)
	case *ast.UnaryExpr:
		c.declareDefinedLocals(v.Operand)
	}
}

// declareLocal gives name a slot if it does not already have one, without
// emitting anything. An empty name is the nameless `*` of `a, * = …`.
func (c *Compiler) declareLocal(name string) {
	if name == "" {
		return
	}
	b := c.cur()
	if _, _, ok := b.resolve(name); ok {
		return
	}
	owner, _ := b.declOwner()
	owner.localSlot(name)
}
