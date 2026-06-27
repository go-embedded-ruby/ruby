package compiler

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-ruby-parser/parser/ast"
)

func TestFastBinOpUnknown(t *testing.T) {
	if _, ok := fastBinOp("?"); ok {
		t.Fatal("expected fastBinOp to report no fast-path opcode for an unknown operator")
	}
	if op, ok := fastBinOp("+"); !ok || op != bytecode.OpAdd {
		t.Fatalf("fastBinOp(+) = %v,%v want OpAdd,true", op, ok)
	}
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

// A ScopedConstAssign whose Target is not a *ast.ScopedConst (which no parser
// produces) trips compileNode's guard.
func TestCompileScopedConstAssignBadTarget(t *testing.T) {
	c := &Compiler{}
	c.push(newBuilder("t", nil))
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected compileNode to panic on a malformed ScopedConstAssign")
		}
		if _, ok := r.(compileError); !ok {
			t.Fatalf("expected compileError, got %#v", r)
		}
	}()
	c.compileNode(&ast.ScopedConstAssign{Target: &ast.IntLit{Value: 1}, Value: &ast.IntLit{Value: 2}})
}

// compilePattern's default fires for a pattern it does not handle; a nil
// ast.Pattern (which no parser produces) exercises that safety net.
func TestCompilePatternDefault(t *testing.T) {
	c := &Compiler{}
	c.push(newBuilder("t", nil))
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected compilePattern to panic on an unhandled pattern")
		}
		if _, ok := r.(compileError); !ok {
			t.Fatalf("expected compileError, got %#v", r)
		}
	}()
	c.compilePattern(nil, 0)
}

func TestCompileUndefinedLocal(t *testing.T) {
	_, err := Compile(&ast.Program{Body: []ast.Node{&ast.VarRef{Name: "ghost"}}})
	if err == nil || !strings.Contains(err.Error(), "undefined local") {
		t.Fatalf("expected undefined-local error, got %v", err)
	}
}

// storeMultiTarget's default fires for a masgn target the parser does not
// currently produce (here a SelfLit), exercising the safety net.
func TestStoreMultiTargetDefault(t *testing.T) {
	prog := &ast.Program{Body: []ast.Node{
		&ast.MultiAssign{
			Names:      []string{""},
			Targets:    []ast.Node{&ast.SelfLit{}},
			SplatIndex: -1,
			Values:     []ast.Node{&ast.IntLit{Value: 1}},
		},
	}}
	_, err := Compile(prog)
	if err == nil || !strings.Contains(err.Error(), "cannot assign to") {
		t.Fatalf("expected a masgn-target error, got %v", err)
	}
}

// storeMultiTarget rejects a receiver-less call as a masgn target. The parser
// only ever emits setter-call targets with an explicit receiver, so this safety
// net is exercised directly from a synthesized AST.
func TestStoreMultiTargetReceiverlessCall(t *testing.T) {
	prog := &ast.Program{Body: []ast.Node{
		&ast.MultiAssign{
			Names:      []string{""},
			Targets:    []ast.Node{&ast.Call{Name: "x=", Args: []ast.Node{}}},
			SplatIndex: -1,
			Values:     []ast.Node{&ast.IntLit{Value: 1}},
		},
	}}
	_, err := Compile(prog)
	if err == nil || !strings.Contains(err.Error(), "receiver-less call") {
		t.Fatalf("expected a receiver-less-call masgn error, got %v", err)
	}
}

// mustResolve fails when a `...` forward is compiled outside a def(...) method
// (no synthetic forward locals are in scope). The parser never produces this,
// so it is exercised directly here.
func TestForwardOutsideDef(t *testing.T) {
	prog := &ast.Program{Body: []ast.Node{
		&ast.Call{Name: "g", Args: []ast.Node{&ast.ForwardArgs{}}},
	}}
	_, err := Compile(prog)
	if err == nil || !strings.Contains(err.Error(), "argument forwarding") {
		t.Fatalf("expected an argument-forwarding error, got %v", err)
	}
}

// rewriteAnonArgs (via anonLocal) fails on a bare anonymous `&` block-pass when
// no enclosing method declares a matching anonymous parameter. This drives the
// BlockPass arm of rewriteAnonArgs; the parser only emits a bare-`&` BlockPass
// inside such a method, so the shape is synthesized directly here.
func TestAnonBlockPassOutsideMethod(t *testing.T) {
	prog := &ast.Program{Body: []ast.Node{
		&ast.Call{Name: "g", Args: []ast.Node{&ast.BlockPass{Value: nil}}},
	}}
	_, err := Compile(prog)
	if err == nil || !strings.Contains(err.Error(), "anonymous argument forwarding") {
		t.Fatalf("expected an anonymous-argument-forwarding error, got %v", err)
	}
}

// rewriteAnonArgs (via anonLocal) fails on a bare anonymous `**` keyword-splat
// when no enclosing method declares a matching anonymous parameter. This drives
// the HashLit/isAnonKwSplat arm of rewriteAnonArgs. A HashLit with a nil key and
// a nil value is the bare-`**` forward the parser only emits inside such a method.
func TestAnonKwSplatForwardOutsideMethod(t *testing.T) {
	prog := &ast.Program{Body: []ast.Node{
		&ast.Call{Name: "g", Args: []ast.Node{
			&ast.HashLit{Keys: []ast.Node{nil}, Values: []ast.Node{nil}},
		}},
	}}
	_, err := Compile(prog)
	if err == nil || !strings.Contains(err.Error(), "anonymous argument forwarding") {
		t.Fatalf("expected an anonymous-argument-forwarding error, got %v", err)
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

// anonLocal fails when a bare anonymous-forward marker (`*` / `**` / `&`) is
// compiled outside a method that declares a matching anonymous parameter. The
// parser only produces these inside such methods, so the safety net is driven
// directly from a synthesized AST.
func TestAnonForwardOutsideDef(t *testing.T) {
	prog := &ast.Program{Body: []ast.Node{
		&ast.Call{Name: "g", Args: []ast.Node{&ast.SplatArg{Value: nil}}},
	}}
	_, err := Compile(prog)
	if err == nil || !strings.Contains(err.Error(), "anonymous argument forwarding") {
		t.Fatalf("expected an anonymous-forwarding error, got %v", err)
	}
}

// rationalValue and numericValue panic on a literal node shape the parser never
// produces (a non-numeric Value), exercising their safety nets.
func TestNumericLiteralValuePanics(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func()
	}{
		{"rationalValue", func() { rationalValue(&ast.StringLit{Value: "x"}) }},
		{"numericValue", func() { numericValue(&ast.StringLit{Value: "x"}) }},
	} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("%s: expected a panic on a non-numeric literal", tc.name)
				}
			}()
			tc.fn()
		}()
	}
}
