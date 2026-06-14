package compiler

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/ast"
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
)

func TestBinOpUnknownPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected binOp to panic on an unknown operator")
		}
	}()
	binOp("?")
}

// compileNode's default fires for a node it does not handle (e.g. *ast.Program,
// which is compiled via compileBody, never compileNode).
func TestCompileNodeDefault(t *testing.T) {
	c := &Compiler{}
	c.push(newBuilder("t", nil))
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected compileNode to panic on an unhandled node")
		}
		if _, ok := r.(compileError); !ok {
			t.Fatalf("expected compileError, got %#v", r)
		}
	}()
	c.compileNode(&ast.Program{})
}

func TestCompileUndefinedLocal(t *testing.T) {
	_, err := Compile(&ast.Program{Body: []ast.Node{&ast.VarRef{Name: "ghost"}}})
	if err == nil || !strings.Contains(err.Error(), "undefined local") {
		t.Fatalf("expected undefined-local error, got %v", err)
	}
}

func TestCompileEmptyProgram(t *testing.T) {
	iseq, err := Compile(&ast.Program{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty body compiles to push_nil; leave; return.
	if len(iseq.Insns) == 0 || iseq.Insns[0].Op != bytecode.OpPushNil {
		t.Fatalf("expected leading push_nil, got %v", iseq.Insns)
	}
}
