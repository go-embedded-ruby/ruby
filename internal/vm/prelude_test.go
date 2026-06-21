package vm

import (
	"io"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser/ast"
)

// bareVM builds a bootstrapped VM without loading the prelude, so prelude
// loader behaviour can be exercised in isolation.
func bareVM() *VM {
	vm := &VM{out: io.Discard, main: object.NewMain(), consts: map[string]object.Value{}}
	vm.bootstrap()
	return vm
}

func TestLoadSourceParseError(t *testing.T) {
	if err := bareVM().loadSource("def"); err == nil || !strings.Contains(err.Error(), "parse error") {
		t.Fatalf("got %v, want a parse error", err)
	}
}

func TestLoadSourceRuntimeError(t *testing.T) {
	// nil has no such method → NoMethodError surfaces as a returned error.
	if err := bareVM().loadSource("nil.no_such_method"); err == nil || !strings.Contains(err.Error(), "NoMethodError") {
		t.Fatalf("got %v, want NoMethodError", err)
	}
}

func TestLoadSourceSuccess(t *testing.T) {
	if err := bareVM().loadSource("x = 1 + 1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A compile error is only reachable from a hand-built AST (the parser turns
// every unknown bareword into a method call), so loadAST is tested directly.
func TestLoadASTCompileError(t *testing.T) {
	prog := &ast.Program{Body: []ast.Node{&ast.VarRef{Name: "ghost"}}}
	if err := bareVM().loadAST(prog); err == nil || !strings.Contains(err.Error(), "undefined local") {
		t.Fatalf("got %v, want an undefined-local compile error", err)
	}
}

func TestLoadPreludePanicsOnBadSource(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected loadPrelude to panic on broken source")
		}
	}()
	bareVM().loadPrelude("def")
}

// An internal raise whose class name isn't registered falls back to a
// StandardError-classed object (so a bare rescue still catches it).
func TestExceptionObjectFallback(t *testing.T) {
	vm := bareVM() // bootstrap registers the exception classes
	obj := vm.exceptionObject(RubyError{Class: "NoSuchError", Message: "x"})
	if got := vm.classOf(obj).name; got != "StandardError" {
		t.Fatalf("fallback class = %q, want StandardError", got)
	}
}

func TestRubyEqualValueTypes(t *testing.T) {
	if !rubyEqual(object.Integer(5), object.Integer(5)) {
		t.Error("5 == 5 should be true")
	}
	if rubyEqual(object.Integer(5), object.NewString("x")) {
		t.Error("5 == \"x\" should be false")
	}
}
