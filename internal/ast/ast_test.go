package ast

import "testing"

// TestNodeMarkers exercises the no-op node() marker methods so the interface
// witnesses are covered.
func TestNodeMarkers(t *testing.T) {
	nodes := []Node{
		&Program{}, &IntLit{}, &FloatLit{}, &StringLit{}, &BoolLit{}, &NilLit{},
		&SelfLit{}, &VarRef{}, &Assign{}, &BinaryExpr{}, &UnaryExpr{}, &Call{},
		&If{}, &While{}, &MethodDef{}, &Return{},
		&ConstRef{}, &IvarRef{}, &IvarAssign{}, &ClassDef{},
		&ModuleDef{}, &Super{}, &Yield{}, &SymbolLit{}, &ArrayLit{}, &HashLit{}, &RangeLit{},
		&Break{}, &Next{}, &OpAssign{}, &Begin{}, &StrInterp{}, &Case{}, &Retry{}, &SplatArg{},
		&BlockPass{}, &ConstAssign{},
	}
	for _, n := range nodes {
		n.node()
	}
	if len(nodes) != 37 {
		t.Fatalf("expected 37 node kinds, got %d", len(nodes))
	}
}
