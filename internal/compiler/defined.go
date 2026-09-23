package compiler

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
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
		c.pushDefinedTag(bytecode.DefinedNil)
	case *ast.BoolLit:
		if v.Value {
			c.pushDefinedTag(bytecode.DefinedTrue)
		} else {
			c.pushDefinedTag(bytecode.DefinedFalse)
		}
	case *ast.SelfLit:
		c.pushDefinedTag(bytecode.DefinedSelf)
	case *ast.Super:
		// NODE_SUPER and NODE_ZSUPER are both DEFINED_ZSUPER (compile.c
		// v3_4_0:6139-6143): a `putnil` then `defined ZSUPER`, which means the
		// super ARGUMENTS are never evaluated — only the existence of the method
		// `super` would reach is tested. The VM half answers it (vm_defined's
		// DEFINED_ZSUPER, vm_insnhelper.c v3_4_0:5505-5518).
		b.emit(bytecode.OpDefinedSuper, 0, 0)
	case *ast.ArrayLit:
		c.compileDefinedElements(v.Elems)
	case *ast.HashLit:
		// NODE_HASH walks its nd_head, a flat LIST of key, value, key, value…
		// (compile.c v3_4_0:5975-5993), so keys are probed like values.
		pairs := make([]ast.Node, 0, len(v.Keys)+len(v.Values))
		for i, k := range v.Keys {
			pairs = append(pairs, k, v.Values[i])
		}
		c.compileDefinedElements(pairs)
	case *ast.SplatArg:
		// NODE_SPLAT probes the splatted expression, then reports "expression"
		// (compile.c v3_4_0:6011-6019).
		c.compileDefinedElements([]ast.Node{v.Value})
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
		c.pushDefinedTag(bytecode.DefinedLvar)
	case *ast.ScopedConst:
		c.compileDefinedScopedConst(v)
	case *ast.Assign, *ast.OpAssign, *ast.MultiAssign, *ast.ConstAssign,
		*ast.ScopedConstAssign, *ast.IvarAssign, *ast.CVarAssign, *ast.GVarAssign:
		c.pushDefinedTag(bytecode.DefinedAsgn)
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
				c.pushDefinedTag(bytecode.DefinedAsgn)
				return
			}
			if _, ok := attrOpAssign(v); ok {
				c.pushDefinedTag(bytecode.DefinedAsgn)
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
		// Literals (numbers, strings, ranges, regexps, …) and any other expression
		// are "expression" — compile.c's `default:` arm (v3_4_0:6003-6008).
		c.pushDefinedTag(bytecode.DefinedExprTag)
	}
}

// isSourceKeyword reports whether name is one of the three source-position
// pseudo-variables the parser hands over as a bare call. They are keywords in
// MRI's grammar (parse.y v3_4_0: `keyword__FILE__`, `keyword__LINE__`,
// `keyword__ENCODING__` in `var_ref`), never method calls, so defined? must not
// probe self for a method of that name.
func isSourceKeyword(name string) bool {
	switch name {
	case "__FILE__", "__LINE__", "__ENCODING__":
		return true
	}
	return false
}

// compileDefinedElements lowers the container forms — an array literal, a hash
// literal, a splat — whose answer is "expression" but only once EVERY part is
// itself defined. MRI probes each element with defined_expr0 and branches to the
// whole expression's nil label on the first undefined one (compile.c
// v3_4_0:5975-6019); `defined?([NonExistentConstant, Array])` is nil, not
// "expression". An empty container has nothing to probe and is plain
// "expression" (NODE_ZLIST).
//
// Each probe is a full defined? of the element, so nothing is evaluated for its
// value and a raising sub-expression is already guarded by the element's own
// lowering.
func (c *Compiler) compileDefinedElements(elems []ast.Node) {
	if len(elems) == 0 {
		c.pushDefinedTag(bytecode.DefinedExprTag)
		return
	}
	b := c.cur()
	undef := make([]int, 0, len(elems))
	for _, e := range elems {
		c.compileDefined(e)
		undef = append(undef, b.emit(bytecode.OpBranchNil, 0, 0))
	}
	c.pushDefinedTag(bytecode.DefinedExprTag)
	done := b.emit(bytecode.OpJump, 0, 0)
	for _, at := range undef {
		b.patch(at, b.here())
	}
	b.emit(bytecode.OpPushNil, 0, 0)
	b.patch(done, b.here())
}

// unaryMethodName maps a unary operator to the method name MRI checks for. Only
// `-` differs (the `-@` unary-minus method); `!` and `~` are their own names.
func unaryMethodName(op string) string {
	if op == "-" {
		return "-@"
	}
	return op
}

// pushDefinedTag pushes a constant tag String. The tag comes from
// bytecode.DefinedTag, the one constructor the VM half uses too, so it is
// FROZEN: MRI answers defined? with rb_iseq_defined_string, an fstring
// (iseq.c v3_4_0:3692), and language/defined_spec.rb asserts `.frozen?` on
// every tag it names.
func (c *Compiler) pushDefinedTag(tag string) {
	b := c.cur()
	b.emit(bytecode.OpPushConst, b.addConst(bytecode.DefinedTag(tag)), 0)
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
	// A nested `defined?` is not a method call on self: MRI parses it to
	// NODE_DEFINED, which is not among the cases defined_expr0 names and so
	// falls through to its `default:` arm — "expression" (compile.c
	// v3_4_0:6003-6008). The operand's locals are still declared, as compiling it
	// normally would do.
	if v.Recv == nil && v.Block == nil && v.Name == "defined?" && len(v.Args) == 1 {
		c.declareDefinedLocals(v.Args[0])
		c.pushDefinedTag(bytecode.DefinedExprTag)
		return
	}
	if v.Recv == nil && v.Block == nil && len(v.Args) == 0 {
		if _, _, ok := b.resolve(v.Name); ok {
			c.pushDefinedTag(bytecode.DefinedLvar)
			return
		}
		// __FILE__, __LINE__ and __ENCODING__ are keywords, not methods: the
		// parser hands them over as bare calls, but MRI parses them to
		// NODE_FILE / NODE_LINE / NODE_ENCODING, which are listed with the
		// literals that fall through to DEFINED_EXPR (compile.c v3_4_0:5996-6008).
		if isSourceKeyword(v.Name) {
			c.pushDefinedTag(bytecode.DefinedExprTag)
			return
		}
	}
	c.compileDefinedReceiverMethod(v.Recv, v.Name)
}

// compileDefinedBinary handles `defined?(a OP b)`. `&&`/`||` are "expression";
// every other binary operator is a method on the left operand.
func (c *Compiler) compileDefinedBinary(v *ast.BinaryExpr) {
	if v.Op == "&&" || v.Op == "||" {
		// `A ||= v` and `A::B ||= v` are not written as an assignment node: the
		// parser desugars them to `(defined?(A) && A) || A = v`, so they arrive
		// here as a `||`. MRI still calls them assignments — PM_CONSTANT_OR_WRITE
		// and PM_CONSTANT_PATH_OR_WRITE are in the DEFINED_ASGN group
		// (prism_compile.c v3_4_0:4059 and 4063; compile.c's NODE_OP_CDECL at
		// v3_4_0:6162 says the same) — and ruby/spec pins it.
		if constOrAssign(v) {
			c.pushDefinedTag(bytecode.DefinedAsgn)
			return
		}
		c.pushDefinedTag(bytecode.DefinedExprTag)
		return
	}
	c.compileDefinedReceiverMethod(v.Left, v.Op)
}

// constOrAssign recognises the parser's desugaring of `A ||= v` / `A::B ||= v`:
// `(defined?(A) && A) || A = v`, with the constant READ shared (same AST
// pointer) between the `defined?` probe, the `&&`'s right operand and — for the
// scoped form — the assignment's target. That identity is what tells the
// desugaring apart from a hand-written `(defined?(A) && A) || A = v`, whose
// three reads are distinct nodes; it is the same test opassign.go uses for the
// index and attribute compound assignments.
func constOrAssign(v *ast.BinaryExpr) bool {
	if v.Op != "||" {
		return false
	}
	guard, ok := v.Left.(*ast.BinaryExpr)
	if !ok || guard.Op != "&&" {
		return false
	}
	probe, ok := guard.Left.(*ast.Call)
	if !ok || probe.Name != "defined?" || probe.Recv != nil || len(probe.Args) != 1 {
		return false
	}
	if probe.Args[0] != guard.Right {
		return false
	}
	switch w := v.Right.(type) {
	case *ast.ConstAssign:
		read, ok := guard.Right.(*ast.ConstRef)
		return ok && read.Name == w.Name
	case *ast.ScopedConstAssign:
		return w.Target == guard.Right
	}
	return false
}

// compileDefinedReceiverMethod emits, under the guard, the response probe for
// `recv.name`. recv == nil means an implicit self receiver. An explicit receiver
// is itself subject to defined? first (MRI: `defined?(@x.m)` is nil when `@x` is
// undefined, even though nil responds to m), so the receiver's own defined-check
// short-circuits to nil before it is evaluated for the response test.
func (c *Compiler) compileDefinedReceiverMethod(recv ast.Node, name string) {
	c.guarded(func(gb *builder) {
		if recv == nil {
			// No receiver written: MRI's DEFINED_FUNC, which tests the response at
			// ANY visibility (rb_ec_obj_respond_to with the include-private flag),
			// so a private method called bare is still "method". B = 0 says so.
			gb.emit(bytecode.OpPushSelf, 0, 0)
			gb.emit(bytecode.OpDefinedMethod, gb.addName(name), 0)
			return
		}
		// Inner defined? on the receiver: nil ⇒ whole expression nil.
		c.compileDefined(recv)
		recvUndef := gb.emit(bytecode.OpBranchNil, 0, 0)
		c.compileNode(recv)
		// A receiver was written: MRI's DEFINED_METHOD, which screens the method
		// entry's visibility — private is not defined?, protected only for a kin
		// self (compile.c v3_4_0:6099-6111 picks the tag; vm_insnhelper.c
		// v3_4_0:5476-5498 applies the rule). B = 1 says so.
		gb.emit(bytecode.OpDefinedMethod, gb.addName(name), 1)
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
	case *ast.ArrayLit:
		for _, e := range v.Elems {
			c.declareDefinedLocals(e)
		}
	case *ast.HashLit:
		for i, k := range v.Keys {
			c.declareDefinedLocals(k)
			c.declareDefinedLocals(v.Values[i])
		}
	case *ast.SplatArg:
		c.declareDefinedLocals(v.Value)
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
