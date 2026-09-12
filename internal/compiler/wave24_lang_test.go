package compiler

import (
	"testing"

	"github.com/go-ruby-parser/parser/ast"
)

// The op-assign recognisers key on AST pointer identity between the write's
// receiver/index nodes and the read's, which is what the parser's desugaring
// produces. Every other shape must fall through to the ordinary setter path —
// notably a hand-written `a[i] = (a[i] || v)`, which Ruby really does evaluate
// twice.
func TestIndexOpAssignRejectsOtherShapes(t *testing.T) {
	recv := &ast.VarRef{Name: "a"}
	idx := &ast.IntLit{Value: 1}
	read := func(r ast.Node, args ...ast.Node) *ast.Call {
		return &ast.Call{Recv: r, Name: "[]", Args: args}
	}
	cases := []struct {
		name string
		call *ast.Call
	}{
		{"not a setter name", &ast.Call{Recv: recv, Name: "[]", Args: []ast.Node{idx}}},
		{"no receiver", &ast.Call{Name: "[]=", Args: []ast.Node{idx}}},
		{"carries a block", &ast.Call{Recv: recv, Name: "[]=", Args: []ast.Node{idx}, Block: &ast.Block{}}},
		{"safe navigation", &ast.Call{Recv: recv, Name: "[]=", Args: []ast.Node{idx}, Safe: true}},
		{"no arguments", &ast.Call{Recv: recv, Name: "[]="}},
		{"value is not a binary expression", &ast.Call{Recv: recv, Name: "[]=", Args: []ast.Node{idx, &ast.IntLit{Value: 2}}}},
		{"value's left side is not a call", &ast.Call{Recv: recv, Name: "[]=", Args: []ast.Node{idx,
			&ast.BinaryExpr{Op: "||", Left: &ast.IntLit{Value: 2}, Right: &ast.IntLit{Value: 3}}}}},
		{"read is not #[]", &ast.Call{Recv: recv, Name: "[]=", Args: []ast.Node{idx,
			&ast.BinaryExpr{Op: "||", Left: &ast.Call{Recv: recv, Name: "at", Args: []ast.Node{idx}}, Right: &ast.IntLit{Value: 3}}}}},
		{"read has a different receiver", &ast.Call{Recv: recv, Name: "[]=", Args: []ast.Node{idx,
			&ast.BinaryExpr{Op: "||", Left: read(&ast.VarRef{Name: "a"}, idx), Right: &ast.IntLit{Value: 3}}}}},
		{"read has a block", &ast.Call{Recv: recv, Name: "[]=", Args: []ast.Node{idx,
			&ast.BinaryExpr{Op: "||", Left: &ast.Call{Recv: recv, Name: "[]", Args: []ast.Node{idx}, Block: &ast.Block{}}, Right: &ast.IntLit{Value: 3}}}}},
		{"read has a different argument count", &ast.Call{Recv: recv, Name: "[]=", Args: []ast.Node{idx,
			&ast.BinaryExpr{Op: "||", Left: read(recv), Right: &ast.IntLit{Value: 3}}}}},
		{"read has a different index node", &ast.Call{Recv: recv, Name: "[]=", Args: []ast.Node{idx,
			&ast.BinaryExpr{Op: "||", Left: read(recv, &ast.IntLit{Value: 1}), Right: &ast.IntLit{Value: 3}}}}},
	}
	for _, tc := range cases {
		if _, _, ok := indexOpAssign(tc.call); ok {
			t.Errorf("indexOpAssign accepted %s", tc.name)
		}
	}
	// The shape the parser really produces is accepted.
	good := &ast.Call{Recv: recv, Name: "[]=", Args: []ast.Node{idx,
		&ast.BinaryExpr{Op: "||", Left: read(recv, idx), Right: &ast.IntLit{Value: 3}}}}
	be, got, ok := indexOpAssign(good)
	if !ok || be.Op != "||" || len(got) != 1 || got[0] != ast.Node(idx) {
		t.Fatalf("indexOpAssign rejected the parser's own shape: %v %v %v", be, got, ok)
	}
}

func TestAttrOpAssignRejectsOtherShapes(t *testing.T) {
	recv := &ast.VarRef{Name: "o"}
	cases := []struct {
		name string
		call *ast.Call
	}{
		{"index setter", &ast.Call{Recv: recv, Name: "[]=", Args: []ast.Node{&ast.IntLit{Value: 1}}}},
		{"no receiver", &ast.Call{Name: "x=", Args: []ast.Node{&ast.IntLit{Value: 1}}}},
		{"carries a block", &ast.Call{Recv: recv, Name: "x=", Args: []ast.Node{&ast.IntLit{Value: 1}}, Block: &ast.Block{}}},
		{"safe navigation", &ast.Call{Recv: recv, Name: "x=", Args: []ast.Node{&ast.IntLit{Value: 1}}, Safe: true}},
		{"two arguments", &ast.Call{Recv: recv, Name: "x=", Args: []ast.Node{&ast.IntLit{Value: 1}, &ast.IntLit{Value: 2}}}},
		{"value is not a binary expression", &ast.Call{Recv: recv, Name: "x=", Args: []ast.Node{&ast.IntLit{Value: 1}}}},
		{"value's left side is not a call", &ast.Call{Recv: recv, Name: "x=", Args: []ast.Node{
			&ast.BinaryExpr{Op: "||", Left: &ast.IntLit{Value: 1}, Right: &ast.IntLit{Value: 2}}}}},
		{"read names another method", &ast.Call{Recv: recv, Name: "x=", Args: []ast.Node{
			&ast.BinaryExpr{Op: "||", Left: &ast.Call{Recv: recv, Name: "y"}, Right: &ast.IntLit{Value: 2}}}}},
		{"read has a different receiver", &ast.Call{Recv: recv, Name: "x=", Args: []ast.Node{
			&ast.BinaryExpr{Op: "||", Left: &ast.Call{Recv: &ast.VarRef{Name: "o"}, Name: "x"}, Right: &ast.IntLit{Value: 2}}}}},
		{"read has a block", &ast.Call{Recv: recv, Name: "x=", Args: []ast.Node{
			&ast.BinaryExpr{Op: "||", Left: &ast.Call{Recv: recv, Name: "x", Block: &ast.Block{}}, Right: &ast.IntLit{Value: 2}}}}},
		{"read takes arguments", &ast.Call{Recv: recv, Name: "x=", Args: []ast.Node{
			&ast.BinaryExpr{Op: "||", Left: &ast.Call{Recv: recv, Name: "x", Args: []ast.Node{&ast.IntLit{Value: 1}}}, Right: &ast.IntLit{Value: 2}}}}},
	}
	for _, tc := range cases {
		if _, ok := attrOpAssign(tc.call); ok {
			t.Errorf("attrOpAssign accepted %s", tc.name)
		}
	}
	good := &ast.Call{Recv: recv, Name: "x=", Args: []ast.Node{
		&ast.BinaryExpr{Op: "+", Left: &ast.Call{Recv: recv, Name: "x"}, Right: &ast.IntLit{Value: 2}}}}
	if be, ok := attrOpAssign(good); !ok || be.Op != "+" {
		t.Fatalf("attrOpAssign rejected the parser's own shape: %v %v", be, ok)
	}
}

func TestScopedConstOpAssignRecognisers(t *testing.T) {
	sc := &ast.ScopedConst{Recv: &ast.ConstRef{Name: "M"}, Name: "C"}
	bare := &ast.ScopedConst{Name: "C"} // leading `::C`: no module part to evaluate
	rhs := &ast.IntLit{Value: 1}
	guard := func(n ast.Node) *ast.BinaryExpr {
		return &ast.BinaryExpr{Op: "&&", Left: &ast.Call{Name: "defined?", Args: []ast.Node{n}}, Right: n}
	}

	orCases := []struct {
		name string
		expr *ast.BinaryExpr
	}{
		{"not ||", &ast.BinaryExpr{Op: "&&", Left: guard(sc), Right: &ast.ScopedConstAssign{Target: sc, Value: rhs}}},
		{"right is not a scoped assign", &ast.BinaryExpr{Op: "||", Left: guard(sc), Right: rhs}},
		{"target is not a scoped const", &ast.BinaryExpr{Op: "||", Left: guard(sc),
			Right: &ast.ScopedConstAssign{Target: &ast.ConstRef{Name: "C"}, Value: rhs}}},
		{"no module part", &ast.BinaryExpr{Op: "||", Left: guard(bare),
			Right: &ast.ScopedConstAssign{Target: bare, Value: rhs}}},
		{"left is not a binary expression", &ast.BinaryExpr{Op: "||", Left: rhs,
			Right: &ast.ScopedConstAssign{Target: sc, Value: rhs}}},
		{"left is not &&", &ast.BinaryExpr{Op: "||", Left: &ast.BinaryExpr{Op: "||", Left: sc, Right: sc},
			Right: &ast.ScopedConstAssign{Target: sc, Value: rhs}}},
		{"guard reads another constant", &ast.BinaryExpr{Op: "||",
			Left:  &ast.BinaryExpr{Op: "&&", Left: &ast.Call{Name: "defined?", Args: []ast.Node{sc}}, Right: rhs},
			Right: &ast.ScopedConstAssign{Target: sc, Value: rhs}}},
		{"guard is not defined?", &ast.BinaryExpr{Op: "||",
			Left:  &ast.BinaryExpr{Op: "&&", Left: &ast.Call{Name: "frozen?", Args: []ast.Node{sc}}, Right: sc},
			Right: &ast.ScopedConstAssign{Target: sc, Value: rhs}}},
		{"guard is not a call", &ast.BinaryExpr{Op: "||",
			Left:  &ast.BinaryExpr{Op: "&&", Left: rhs, Right: sc},
			Right: &ast.ScopedConstAssign{Target: sc, Value: rhs}}},
		{"defined? has a receiver", &ast.BinaryExpr{Op: "||",
			Left:  &ast.BinaryExpr{Op: "&&", Left: &ast.Call{Recv: sc, Name: "defined?", Args: []ast.Node{sc}}, Right: sc},
			Right: &ast.ScopedConstAssign{Target: sc, Value: rhs}}},
		{"defined? has no argument", &ast.BinaryExpr{Op: "||",
			Left:  &ast.BinaryExpr{Op: "&&", Left: &ast.Call{Name: "defined?"}, Right: sc},
			Right: &ast.ScopedConstAssign{Target: sc, Value: rhs}}},
		{"defined? names another constant", &ast.BinaryExpr{Op: "||",
			Left:  &ast.BinaryExpr{Op: "&&", Left: &ast.Call{Name: "defined?", Args: []ast.Node{rhs}}, Right: sc},
			Right: &ast.ScopedConstAssign{Target: sc, Value: rhs}}},
	}
	for _, tc := range orCases {
		if _, _, ok := scopedConstOrAssign(tc.expr); ok {
			t.Errorf("scopedConstOrAssign accepted %s", tc.name)
		}
	}
	good := &ast.BinaryExpr{Op: "||", Left: guard(sc), Right: &ast.ScopedConstAssign{Target: sc, Value: rhs}}
	if got, val, ok := scopedConstOrAssign(good); !ok || got != sc || val != ast.Node(rhs) {
		t.Fatalf("scopedConstOrAssign rejected the parser's own shape: %v %v %v", got, val, ok)
	}

	opCases := []struct {
		name string
		expr *ast.ScopedConstAssign
	}{
		{"target is not a scoped const", &ast.ScopedConstAssign{Target: &ast.ConstRef{Name: "C"}, Value: rhs}},
		{"no module part", &ast.ScopedConstAssign{Target: bare, Value: &ast.BinaryExpr{Op: "+", Left: bare, Right: rhs}}},
		{"value is not a binary expression", &ast.ScopedConstAssign{Target: sc, Value: rhs}},
		{"value does not read the target", &ast.ScopedConstAssign{Target: sc,
			Value: &ast.BinaryExpr{Op: "+", Left: rhs, Right: rhs}}},
	}
	for _, tc := range opCases {
		if _, _, ok := scopedConstOpAssign(tc.expr); ok {
			t.Errorf("scopedConstOpAssign accepted %s", tc.name)
		}
	}
	goodOp := &ast.ScopedConstAssign{Target: sc, Value: &ast.BinaryExpr{Op: "&&", Left: sc, Right: rhs}}
	if got, be, ok := scopedConstOpAssign(goodOp); !ok || got != sc || be.Op != "&&" {
		t.Fatalf("scopedConstOpAssign rejected the parser's own shape: %v %v %v", got, be, ok)
	}
}

// A range is a flip-flop only where a condition is expected, and only when it
// has both bounds; hasFlipFlop is the gate that keeps every other expression on
// the untouched compileNode path.
func TestHasFlipFlop(t *testing.T) {
	two := &ast.RangeLit{Lo: &ast.IntLit{Value: 1}, Hi: &ast.IntLit{Value: 2}}
	cases := []struct {
		node ast.Node
		want bool
	}{
		{two, true},
		{&ast.RangeLit{Hi: &ast.IntLit{Value: 2}}, false},                   // beginless: a Range value
		{&ast.RangeLit{Lo: &ast.IntLit{Value: 1}}, false},                   // endless: a Range value
		{&ast.BinaryExpr{Op: "||", Left: two, Right: &ast.BoolLit{}}, true}, // left operand
		{&ast.BinaryExpr{Op: "&&", Left: &ast.BoolLit{}, Right: two}, true}, // right operand
		{&ast.BinaryExpr{Op: "+", Left: two, Right: two}, false},            // not a logical operator
		{&ast.BinaryExpr{Op: "||", Left: &ast.BoolLit{}, Right: &ast.BoolLit{}}, false},
		{&ast.UnaryExpr{Op: "!", Operand: two}, true},
		{&ast.UnaryExpr{Op: "-", Operand: two}, false},
		{&ast.IntLit{Value: 1}, false},
	}
	for i, tc := range cases {
		if got := hasFlipFlop(tc.node); got != tc.want {
			t.Errorf("case %d: hasFlipFlop = %v, want %v", i, got, tc.want)
		}
	}
}

// compileCondition's default arm: hasFlipFlop said yes for a node kind the
// switch does not name. No parser produces that, so it is built by hand.
func TestCompileConditionDefaultArm(t *testing.T) {
	c := &Compiler{}
	b := newBuilder("t", nil)
	c.push(b)
	// A `&&` whose operand is a flip-flop reaches the BinaryExpr arm, and the
	// non-flip-flop operand reaches compileNode through the guard.
	c.compileCondition(&ast.BinaryExpr{
		Op:    "&&",
		Left:  &ast.RangeLit{Lo: &ast.BoolLit{Value: true}, Hi: &ast.BoolLit{Value: true}},
		Right: &ast.IntLit{Value: 1},
	})
	if len(b.insns) == 0 {
		t.Fatal("compileCondition emitted nothing")
	}
}

// flipFlopSlot stops at a borrowed scope: under CompileWithLocals the parent
// holds a Binding's locals in a frame that is already sized, so the slot must
// land in the eval's own scope at depth 0.
func TestFlipFlopSlotStopsAtBorrowedScope(t *testing.T) {
	c := &Compiler{}
	parent := newBuilder("<binding>", nil)
	parent.locals = []string{"a", "b"}
	parent.borrowed = true
	c.push(parent)
	child := newBuilder("(eval)", nil)
	child.isBlock = true
	child.parent = parent
	c.push(child)
	depth, slot := c.flipFlopSlot()
	if depth != 0 {
		t.Fatalf("depth = %d, want 0 (the borrowed parent must not grow)", depth)
	}
	if slot != 0 || len(parent.locals) != 2 {
		t.Fatalf("slot = %d, parent locals = %v; want a fresh slot in the child", slot, parent.locals)
	}
	// An ordinary block scope, by contrast, is walked through to the method.
	c = &Compiler{}
	method := newBuilder("m", nil)
	c.push(method)
	blk := newBlockBuilder("b", nil, method)
	c.push(blk)
	if depth, _ := c.flipFlopSlot(); depth != 1 {
		t.Fatalf("depth = %d, want 1 (the enclosing method scope)", depth)
	}
}

func TestMagicEncodingNameValueTermination(t *testing.T) {
	cases := []struct{ line, want string }{
		{"# vim: filetype=ruby, fileencoding=big5, tabsize=3", "big5"}, // comma-separated vim form
		{"# coding: big5", "big5"},
		{"# coding:binary-*-", "binary"}, // `-*-` still closes the value
		{"# coding: binary;", "binary"},
		{"# -*- coding: euc-jp -*-", "euc-jp"},
		{"# coding: utf_8", "utf_8"}, // `_` is a name byte
		{"x = 1", ""},                // not a comment
		{"# a plain comment", ""},    // no coding field
	}
	for _, tc := range cases {
		if got := magicEncodingName(tc.line); got != tc.want {
			t.Errorf("magicEncodingName(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestIsEncNameByte(t *testing.T) {
	for _, b := range []byte{'a', 'z', 'A', 'Z', '0', '9', '-', '_'} {
		if !isEncNameByte(b) {
			t.Errorf("isEncNameByte(%q) = false, want true", b)
		}
	}
	for _, b := range []byte{' ', ',', ';', '*', ':', '"', '\t'} {
		if isEncNameByte(b) {
			t.Errorf("isEncNameByte(%q) = true, want false", b)
		}
	}
}
