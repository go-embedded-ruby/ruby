package compiler

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
	"github.com/go-ruby-parser/parser/ast"
)

// compileSrc parses and compiles src, failing the test on either error.
func compileSrc(t *testing.T, src string) *bytecode.ISeq {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	iseq, err := Compile(prog)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	return iseq
}

// hasLocal reports whether iseq's local table holds name.
func hasLocal(iseq *bytecode.ISeq, name string) bool {
	for _, l := range iseq.Locals {
		if l == name {
			return true
		}
	}
	return false
}

// forBlock returns the child ISeq a `for` compiles its body into. A `for` emits
// exactly one child at the top level of these sources.
func forBlock(t *testing.T, iseq *bytecode.ISeq) *bytecode.ISeq {
	t.Helper()
	if len(iseq.Children) != 1 {
		t.Fatalf("expected exactly one child ISeq, got %d", len(iseq.Children))
	}
	return iseq.Children[0]
}

// Every shape of loop variable declares its plain locals in the scope AROUND
// the loop, so they outlive it — `for` opens no scope. The shapes come in
// through two different parser fields: the simple lists through For.Vars, and
// everything Vars cannot spell through For.Target.
func TestForDeclaresLoopVariablesInTheEnclosingScope(t *testing.T) {
	cases := []struct {
		src   string
		want  []string
		unset []string
	}{
		{src: "for i in [1]; end", want: []string{"i"}},
		{src: "for a, b in [[1,2]]; end", want: []string{"a", "b"}},
		{src: "for i, in [[1,2]]; end", want: []string{"i"}},
		{src: "for i, * in [[1,2]]; end", want: []string{"i"}},
		{src: "for i, *j in [[1,2]]; end", want: []string{"i", "j"}},
		{src: "for i, *j, k in [[1,2,3,4]]; end", want: []string{"i", "j", "k"}},
		{src: "for (i, j, k) in [[1,2,3]]; end", want: []string{"i", "j", "k"}},
		{src: "for i, (j, k) in [[1,[2,3]]]; end", want: []string{"i", "j", "k"}},
		// Non-local targets bind nothing in the local table.
		{src: "for @v in [1]; end", unset: []string{"@v", "v"}},
		{src: "for $v in [1]; end", unset: []string{"$v", "v"}},
		{src: "for @@v in [1]; end", unset: []string{"@@v", "v"}},
		{src: "for V in [1]; end", unset: []string{"V"}},
		// A variable FIRST ASSIGNED in the body belongs to the scope around the
		// loop too, and hops out of an enclosing `for` as well.
		{src: "for a in [6]; c = a; end", want: []string{"a", "c"}},
		{src: "for a in [6]; for b in [7]; c = a * b; end; end", want: []string{"a", "b", "c"}},
		{src: "for a in [6]; d += 1; end", want: []string{"a", "d"}},
		{src: "for a in [6]; begin; raise; rescue => e; end; end", want: []string{"a", "e"}},
	}
	for _, tc := range cases {
		iseq := compileSrc(t, tc.src)
		for _, name := range tc.want {
			if !hasLocal(iseq, name) {
				t.Errorf("%s: %q is not a local of the enclosing scope (locals %v)", tc.src, name, iseq.Locals)
			}
		}
		for _, name := range tc.unset {
			if hasLocal(iseq, name) {
				t.Errorf("%s: %q should not be a local (locals %v)", tc.src, name, iseq.Locals)
			}
		}
	}
}

// An enclosing BLOCK is a scope, so a `for` inside one declares into the block,
// not into the method around it. The for_spec example "can be nested with blocks
// in between" pins exactly this.
func TestForInsideABlockDeclaresIntoTheBlock(t *testing.T) {
	iseq := compileSrc(t, "[1].each { for c in [3]; c1 = c; end }")
	if hasLocal(iseq, "c") || hasLocal(iseq, "c1") {
		t.Fatalf("c/c1 leaked past the block into the top level: %v", iseq.Locals)
	}
	blk := iseq.Children[0]
	if !hasLocal(blk, "c") || !hasLocal(blk, "c1") {
		t.Fatalf("c/c1 are not locals of the enclosing block: %v", blk.Locals)
	}
}

// The each-block's hidden parameter is shaped by the kind of loop variable: a
// lone local takes one REQUIRED argument, everything else collects the yielded
// values into a REST Array. That is what lets `for x, y in o` see both values of
// an `each` that yields two.
func TestForBlockParameterShape(t *testing.T) {
	cases := []struct {
		src      string
		required int
		splat    int
	}{
		{"for i in [1]; end", 1, -1},
		{"for a, b in [[1,2]]; end", 0, 0},
		{"for i, in [[1,2]]; end", 0, 0},
		{"for @v in [1]; end", 0, 0},
	}
	for _, tc := range cases {
		blk := forBlock(t, compileSrc(t, tc.src))
		if blk.NumRequired != tc.required || blk.SplatIndex != tc.splat {
			t.Errorf("%s: NumRequired=%d SplatIndex=%d, want %d/%d",
				tc.src, blk.NumRequired, blk.SplatIndex, tc.required, tc.splat)
		}
	}
}

// A single non-local target takes the FIRST yielded value and drops the rest,
// which MRI spells `expandarray 1, 0` on the rest Array. A multi-target instead
// runs the length test that picks between args[0] and args.
func TestForTargetLowering(t *testing.T) {
	blk := forBlock(t, compileSrc(t, "for @v in [1]; end"))
	var sawExpand, sawSetIvar bool
	for _, in := range blk.Insns {
		if in.Op == bytecode.OpExpandArray && in.A == 1 && in.B == 0 && in.C == 0 {
			sawExpand = true
		}
		if in.Op == bytecode.OpSetIvar {
			sawSetIvar = true
		}
	}
	if !sawExpand || !sawSetIvar {
		t.Fatalf("for @v: expandarray(1,0)=%v setivar=%v", sawExpand, sawSetIvar)
	}

	blk = forBlock(t, compileSrc(t, "for a, b in [[1,2]]; end"))
	var sawLength, sawAref bool
	for _, in := range blk.Insns {
		if in.Op == bytecode.OpSend {
			switch blk.Names[in.A] {
			case "length":
				sawLength = true
			case "[]":
				sawAref = true
			}
		}
	}
	if !sawLength || !sawAref {
		t.Fatalf("multi-target for: length=%v aref=%v", sawLength, sawAref)
	}
}

// `for o&.x in m` must skip the setter on a nil receiver instead of raising, so
// the store arm emits the same nil guard an ordinary `o&.x = v` does. A
// multiple assignment never reaches that branch — `a&.x, b = …` is a SyntaxError.
func TestForSafeNavigationTargetGuardsOnNil(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{"o = nil; for o&.x in [1]; end", true},
		{"o = nil; for o.x in [1]; end", false},
	} {
		blk := forBlock(t, compileSrc(t, tc.src))
		var got bool
		for _, in := range blk.Insns {
			if in.Op == bytecode.OpBranchNil {
				got = true
			}
		}
		if got != tc.want {
			t.Errorf("%s: BranchNil present = %v, want %v", tc.src, got, tc.want)
		}
	}
}

// An index or attribute target is written through its setter, with the receiver
// and the index evaluated inside the block — once per iteration, as MRI does.
func TestForIndexAndAttributeTargets(t *testing.T) {
	for _, tc := range []struct{ src, setter string }{
		{"arr = []; for arr[1] in [1]; end", "[]="},
		{"o = nil; for o.x in [1]; end", "x="},
	} {
		blk := forBlock(t, compileSrc(t, tc.src))
		var got bool
		for _, in := range blk.Insns {
			if in.Op == bytecode.OpSend && blk.Names[in.A] == tc.setter {
				got = true
			}
		}
		if !got {
			t.Errorf("%s: no %q send in the each-block", tc.src, tc.setter)
		}
	}
}

// declareForTargetLocals ignores a target kind that names no local. The parser
// never builds this shape (a For.Target is always an assignable node), so it is
// synthesized to reach the walk's default arm.
func TestDeclareForTargetLocalsIgnoresANonTarget(t *testing.T) {
	var got []string
	declareForTargetLocals(&ast.IntLit{Value: 1}, func(n string) { got = append(got, n) })
	declareForTargetLocals(nil, func(n string) { got = append(got, n) })
	if len(got) != 0 {
		t.Fatalf("expected no declarations, got %v", got)
	}
}

// `def f(...)` parks the forwarded arguments in the METHOD's locals, so a
// forwarding call written inside a block — a `for` body as much as any other —
// must read them from the enclosing frame. A depth of 0 indexes the block's own
// frame, which held one local and crashed.
func TestForwardingCallInsideABlockReadsTheMethodFrame(t *testing.T) {
	for _, src := range []string{
		"def f(...); [1].each { g(...) }; end",
		"def f(...); for x in [1]; g(...); end; end",
		"class C; def f(...); [1].each { super(...) }; end; end",
	} {
		blocks := blockISeqs(compileSrc(t, src))
		if len(blocks) == 0 {
			t.Fatalf("%s: no block ISeq compiled", src)
		}
		var sawDepth bool
		for _, blk := range blocks {
			for _, in := range blk.Insns {
				if in.Op == bytecode.OpGetLocal && in.B > 0 {
					sawDepth = true
				}
			}
		}
		if !sawDepth {
			t.Errorf("%s: the block reads no enclosing-frame local; the forward locals were resolved at depth 0", src)
		}
	}
}

// blockISeqs collects every block/for child ISeq in the tree.
func blockISeqs(iseq *bytecode.ISeq) []*bytecode.ISeq {
	var out []*bytecode.ISeq
	for _, ch := range iseq.Children {
		// A block ISeq is named for the FRAME it will be — "block in foo",
		// "block (2 levels) in foo" — since that name is the backtrace label
		// (calculate_iseq_label, vm_backtrace.c v3_4_0:229). It used to be the
		// literal "<block>"; matching on the prefix keeps this helper finding
		// them whatever the enclosing scope is called.
		if strings.HasPrefix(ch.Name, "block in ") || strings.HasPrefix(ch.Name, "block (") || ch.Name == "<for>" {
			out = append(out, ch)
		}
		out = append(out, blockISeqs(ch)...)
	}
	return out
}

// mustResolve reports a compiler bug rather than returning a bogus slot when the
// forwarding locals are not in scope.
func TestMustResolveOutsideAForwardingMethod(t *testing.T) {
	c := &Compiler{}
	c.push(newBuilder("t", nil))
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected mustResolve to panic outside a def(...) method")
		}
		if e, ok := r.(compileError); !ok || !strings.Contains(e.msg, "argument forwarding") {
			t.Fatalf("expected an argument-forwarding compileError, got %#v", r)
		}
	}()
	c.mustResolve(fwdRestName)
}

// Block-local variables (`{ |a; b| … }`) are slots of the BLOCK, not parameters,
// and shadow any enclosing binding of the same name.
func TestBlockLocalsTakeTheirOwnSlots(t *testing.T) {
	iseq := compileSrc(t, "x = 1; [1].each { |q; x, y| x = 9; y = 8 }")
	blk := iseq.Children[0]
	for _, name := range []string{"q", "x", "y"} {
		if !hasLocal(blk, name) {
			t.Fatalf("%q is not a local of the block: %v", name, blk.Locals)
		}
	}
	if blk.NumRequired != 1 {
		t.Fatalf("block locals must not be parameters: NumRequired=%d", blk.NumRequired)
	}
	// The write to x must reach the block's own slot (depth 0), not the outer one.
	for _, in := range blk.Insns {
		if in.Op == bytecode.OpSetLocal && in.B != 0 {
			t.Fatalf("a block-local write escaped to depth %d", in.B)
		}
	}
}

// `defined?` inspects its operand instead of compiling it, but the assignments
// inside it still DECLARE — MRI builds its local table in the parser. Without
// that, the next mention of the name has no slot and fails to compile.
func TestDefinedDeclaresTheLocalsItsOperandIntroduces(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{"defined?(a += 1)", []string{"a"}},
		{"defined?(a = 1)", []string{"a"}},
		{"defined?(a, b = 1, 2)", []string{"a", "b"}},
		{"defined?(a, (b, c) = 1, [2, 3])", []string{"a", "b", "c"}},
		{"defined?(a, * = 1, 2)", []string{"a"}},
		{"defined?(a = (b = 1))", []string{"a", "b"}},
		{"defined?(a += (b = 1))", []string{"a", "b"}},
		{"defined?(foo(a = 1))", []string{"a"}},
		{"defined?(foo.bar(a = 1))", []string{"a"}},
		{"defined?(1 + (a = 1))", []string{"a"}},
		{"defined?(-(a = 1))", []string{"a"}},
		{"defined?(@v = (a = 1))", []string{"a"}},
		{"defined?(@@v = (a = 1))", []string{"a"}},
		{"defined?($v = (a = 1))", []string{"a"}},
		{"defined?(V = (a = 1))", []string{"a"}},
		{"defined?(Object::V = (a = 1))", []string{"a"}},
		{"defined?(a = 1); defined?(a = 2)", []string{"a"}},
	}
	for _, tc := range cases {
		iseq := compileSrc(t, tc.src)
		for _, name := range tc.want {
			if !hasLocal(iseq, name) {
				t.Errorf("%s: %q got no slot (locals %v)", tc.src, name, iseq.Locals)
			}
		}
	}
	// And the point of it: the name is then readable as a local.
	compileSrc(t, "defined?(a += 1); a")
	compileSrc(t, "defined?(a += 1); defined?(a.b += 1)")
}

// A defined? operand that assigns nothing declares nothing.
func TestDefinedDeclaresNothingForAPlainOperand(t *testing.T) {
	iseq := compileSrc(t, "defined?(1 + 2)")
	if len(iseq.Locals) != 0 {
		t.Fatalf("expected no locals, got %v", iseq.Locals)
	}
}

// A compound assignment to an attribute or an index arrives as the parser's
// desugaring into a setter Call, but MRI tags it "assignment", not "method". A
// PLAIN `a[0] = 1` is an ordinary attrasgn call and stays "method".
func TestDefinedTagsDesugaredCompoundAssignment(t *testing.T) {
	for _, src := range []string{
		"a = nil; defined?(a.b += 1)",
		"a = nil; defined?(a.b ||= 1)",
		"a = nil; defined?(a.b &&= 1)",
		"a = nil; defined?(a[:b] += 1)",
		"a = nil; defined?(a[:b] ||= 1)",
		"a = nil; defined?(a[:b] &&= 1)",
	} {
		iseq := compileSrc(t, src)
		if !hasDefinedTag(iseq, "assignment") {
			t.Errorf("%s: no \"assignment\" tag (tags %v)", src, definedTags(iseq))
		}
		if hasOp(iseq, bytecode.OpDefinedMethod) {
			t.Errorf("%s: probed for a method; MRI answers from the syntax alone", src)
		}
	}
	// A plain index or attribute assignment is NOT in MRI's DEFINED_ASGN group.
	for _, src := range []string{"a = []; defined?(a[0] = 1)", "a = nil; defined?(a.b)"} {
		iseq := compileSrc(t, src)
		if hasDefinedTag(iseq, "assignment") {
			t.Errorf("%s: tagged \"assignment\"; MRI says \"method\"", src)
		}
		// "method" is not a compile-time constant: OpDefinedMethod produces it at
		// run time from the response test, so the probe itself is the witness.
		if !hasOp(iseq, bytecode.OpDefinedMethod) {
			t.Errorf("%s: expected a method probe (tags %v)", src, definedTags(iseq))
		}
	}
}

// definedTags collects every defined? tag String pushed anywhere in the tree.
func definedTags(iseq *bytecode.ISeq) []string {
	var out []string
	for _, v := range iseq.Consts {
		if s, ok := v.(*object.String); ok {
			switch s.Str() {
			case "assignment", "method", "expression", "local-variable":
				out = append(out, s.Str())
			}
		}
	}
	for _, ch := range iseq.Children {
		out = append(out, definedTags(ch)...)
	}
	return out
}

func hasDefinedTag(iseq *bytecode.ISeq, tag string) bool {
	for _, t := range definedTags(iseq) {
		if t == tag {
			return true
		}
	}
	return false
}

func hasOp(iseq *bytecode.ISeq, op bytecode.Op) bool {
	for _, in := range iseq.Insns {
		if in.Op == op {
			return true
		}
	}
	for _, ch := range iseq.Children {
		if hasOp(ch, op) {
			return true
		}
	}
	return false
}

// `A ||= v` is not an assignment NODE: the parser desugars it to
// `(defined?(A) && A) || A = v`, with the constant read shared between the
// probe, the `&&`'s right operand and (for the scoped form) the assignment's
// target. MRI still calls it an assignment. A hand-written expression of the
// same shape has three distinct reads and stays "expression".
func TestDefinedTagsDesugaredConstantOrAssign(t *testing.T) {
	for _, src := range []string{"defined?(A ||= 1)", "defined?(Object::A ||= 1)"} {
		if iseq := compileSrc(t, src); !hasDefinedTag(iseq, "assignment") {
			t.Errorf("%s: tags %v, want assignment", src, definedTags(iseq))
		}
	}
	for _, src := range []string{
		"X = 5; defined?((defined?(X) && X) || X = 1)",
		"defined?(1 || 2)",
		"defined?(1 && 2)",
		"y = 3; defined?(y || 4)",
	} {
		if iseq := compileSrc(t, src); hasDefinedTag(iseq, "assignment") {
			t.Errorf("%s: tagged assignment; MRI says expression", src)
		}
	}
}

// Every way the recogniser must say no. The parser builds none of these shapes
// for a `||=`, so they are synthesized; each one leaves the expression an
// ordinary `||`, which defined? tags "expression".
func TestConstOrAssignRejectsOtherShapes(t *testing.T) {
	read := &ast.ConstRef{Name: "A"}
	probe := func(arg ast.Node) *ast.Call { return &ast.Call{Name: "defined?", Args: []ast.Node{arg}} }
	guard := func(l, r ast.Node) *ast.BinaryExpr { return &ast.BinaryExpr{Op: "&&", Left: l, Right: r} }
	one := &ast.IntLit{Value: 1}
	cases := []struct {
		name string
		expr *ast.BinaryExpr
	}{
		{"not ||", &ast.BinaryExpr{Op: "&&", Left: guard(probe(read), read), Right: &ast.ConstAssign{Name: "A", Value: one}}},
		{"left is not a binary expression", &ast.BinaryExpr{Op: "||", Left: read, Right: &ast.ConstAssign{Name: "A", Value: one}}},
		{"left is not &&", &ast.BinaryExpr{Op: "||", Left: &ast.BinaryExpr{Op: "+", Left: probe(read), Right: read},
			Right: &ast.ConstAssign{Name: "A", Value: one}}},
		{"guard's left is not a call", &ast.BinaryExpr{Op: "||", Left: guard(read, read), Right: &ast.ConstAssign{Name: "A", Value: one}}},
		{"probe is not defined?", &ast.BinaryExpr{Op: "||", Left: guard(&ast.Call{Name: "frozen?", Args: []ast.Node{read}}, read),
			Right: &ast.ConstAssign{Name: "A", Value: one}}},
		{"probe has a receiver", &ast.BinaryExpr{Op: "||", Left: guard(&ast.Call{Recv: read, Name: "defined?", Args: []ast.Node{read}}, read),
			Right: &ast.ConstAssign{Name: "A", Value: one}}},
		{"probe takes no argument", &ast.BinaryExpr{Op: "||", Left: guard(&ast.Call{Name: "defined?"}, read),
			Right: &ast.ConstAssign{Name: "A", Value: one}}},
		{"probe reads a different node", &ast.BinaryExpr{Op: "||", Left: guard(probe(&ast.ConstRef{Name: "A"}), read),
			Right: &ast.ConstAssign{Name: "A", Value: one}}},
		{"right is not an assignment", &ast.BinaryExpr{Op: "||", Left: guard(probe(read), read), Right: one}},
		{"assigns a different constant", &ast.BinaryExpr{Op: "||", Left: guard(probe(read), read),
			Right: &ast.ConstAssign{Name: "B", Value: one}}},
		{"read is not a plain constant", func() *ast.BinaryExpr {
			sc := &ast.ScopedConst{Recv: &ast.ConstRef{Name: "Object"}, Name: "A"}
			return &ast.BinaryExpr{Op: "||", Left: guard(probe(sc), sc), Right: &ast.ConstAssign{Name: "A", Value: one}}
		}()},
		{"scoped assignment targets another node", func() *ast.BinaryExpr {
			sc := &ast.ScopedConst{Recv: &ast.ConstRef{Name: "Object"}, Name: "A"}
			return &ast.BinaryExpr{Op: "||", Left: guard(probe(sc), sc),
				Right: &ast.ScopedConstAssign{Target: &ast.ScopedConst{Recv: &ast.ConstRef{Name: "Object"}, Name: "A"}, Value: one}}
		}()},
	}
	for _, tc := range cases {
		if constOrAssign(tc.expr) {
			t.Errorf("%s: recognised as a constant ||=", tc.name)
		}
	}
	// …and the two shapes it must say yes to.
	sc := &ast.ScopedConst{Recv: &ast.ConstRef{Name: "Object"}, Name: "A"}
	for _, tc := range []struct {
		name string
		expr *ast.BinaryExpr
	}{
		{"A ||= 1", &ast.BinaryExpr{Op: "||", Left: guard(probe(read), read), Right: &ast.ConstAssign{Name: "A", Value: one}}},
		{"Object::A ||= 1", &ast.BinaryExpr{Op: "||", Left: guard(probe(sc), sc), Right: &ast.ScopedConstAssign{Target: sc, Value: one}}},
	} {
		if !constOrAssign(tc.expr) {
			t.Errorf("%s: not recognised", tc.name)
		}
	}
}
