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
		&ModuleDef{}, &Super{}, &Yield{},
	}
	for _, n := range nodes {
		n.node()
	}
	if len(nodes) != 23 {
		t.Fatalf("expected 23 node kinds, got %d", len(nodes))
	}
}
