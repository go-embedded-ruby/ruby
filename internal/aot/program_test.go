package aot

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	rparser "github.com/go-ruby-parser/parser"
)

// topISeq compiles a whole program and returns its top-level ISeq.
func topISeq(t *testing.T, src string) *bytecode.ISeq {
	t.Helper()
	prog, err := rparser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return iseq
}

func TestCompileProgram(t *testing.T) {
	// add: lowerable and uses arithmetic (forces the bytecode import).
	// names: lowerable but emits no operator (exercises the no-bytecode import).
	// dup redefinition of add: only the first is compiled.
	// bad: a splat param the stage cannot lower — skipped.
	src := "def add(a, b) = a + b\n" +
		"def names = [1, 2, 3]\n" +
		"def add(a, b) = a - b\n" +
		"def bad(*a) = a\n"
	content, keys, ok := CompileProgram(topISeq(t, src))
	if !ok {
		t.Fatal("CompileProgram should have lowered some methods")
	}

	if _, err := parser.ParseFile(token.NewFileSet(), "gen.go", content, 0); err != nil {
		t.Fatalf("generated program file does not parse: %v\n%s", err, content)
	}
	for _, want := range []string{
		"package vm",
		"func (vm *VM) aotm0(",
		"func (vm *VM) aotm1(",
		"func init() {",
		`RegisterCompiled("Object#add", (*VM).aotm0)`,
		`RegisterCompiled("Object#names", (*VM).aotm1)`,
		`"github.com/go-embedded-ruby/ruby/internal/bytecode"`, // add uses an operator
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated file missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "aotm2") || strings.Contains(content, "Object#bad") {
		t.Errorf("redefinition or unlowerable method should be excluded:\n%s", content)
	}
	if len(keys) != 2 || keys[0] != "Object#add" || keys[1] != "Object#names" {
		t.Errorf("keys = %v, want [Object#add Object#names]", keys)
	}
}

// TestCompileProgramNoBytecodeImport: a program whose only lowered method emits
// no operator must omit the bytecode import (else it would not compile).
func TestCompileProgramNoBytecodeImport(t *testing.T) {
	content, _, ok := CompileProgram(topISeq(t, "def names = [1, 2, 3]\n"))
	if !ok {
		t.Fatal("expected a lowered method")
	}
	if strings.Contains(content, "internal/bytecode") {
		t.Errorf("bytecode import should be omitted when unused:\n%s", content)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "gen.go", content, 0); err != nil {
		t.Fatalf("does not parse: %v\n%s", err, content)
	}
}

// TestCompileProgramNothing: when no top-level method can be lowered, ok is false.
func TestCompileProgramNothing(t *testing.T) {
	if content, keys, ok := CompileProgram(topISeq(t, "def bad(*a) = a\nputs 1\n")); ok || content != "" || keys != nil {
		t.Errorf("expected (\"\", nil, false), got (%q, %v, %v)", content, keys, ok)
	}
}
