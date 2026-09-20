package compiler

import (
	"strings"
	"testing"

	"github.com/go-ruby-parser/parser/ast"
)

// preEvalMasgnTarget skips a receiver-less setter call rather than trying to
// evaluate a receiver that is not there, leaving storeMultiTarget to report it.
// The parser only ever emits setter targets with an explicit receiver, so the
// node is synthesized. This is the companion of
// TestStoreMultiTargetReceiverlessCall: with a splat argument the target now
// reaches the pre-evaluation walk first.
func TestPreEvalMasgnTargetReceiverlessSplatCall(t *testing.T) {
	prog := &ast.Program{Body: []ast.Node{
		&ast.MultiAssign{
			Names:      []string{""},
			Targets:    []ast.Node{&ast.Call{Name: "[]=", Args: []ast.Node{&ast.SplatArg{Value: &ast.VarRef{Name: "i"}}}}},
			SplatIndex: -1,
			Values:     []ast.Node{&ast.IntLit{Value: 1}},
		},
	}}
	_, err := Compile(prog)
	if err == nil || !strings.Contains(err.Error(), "receiver-less call") {
		t.Fatalf("expected a receiver-less-call masgn error, got %v", err)
	}
}
