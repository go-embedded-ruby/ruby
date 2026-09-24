package compiler

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-ruby-parser/parser"
)

// compileString parses and compiles src, returning the first error either step
// gives. A compile error is what a SyntaxError is made of in the VM's eval
// paths, so this is the shape a refusal reaches Ruby in.
func compileString(src string) (*bytecode.ISeq, error) {
	prog, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	return Compile(prog)
}

// TestBlockLabelLevels drives blockLabel over every shape it has to name. It is
// calculate_iseq_label's block arm (vm_backtrace.c v3_4_0:229), so the counting
// rule is MRI's: one hop is "block in NAME", more is "block (N levels) in NAME".
func TestBlockLabelLevels(t *testing.T) {
	meth := &builder{name: "foo"}
	lvl1 := &builder{name: "block in foo", parent: meth, isBlock: true}
	lvl2 := &builder{name: "block (2 levels) in foo", parent: lvl1, isBlock: true}

	if got := blockLabel(meth); got != "block in foo" {
		t.Errorf("one level: got %q", got)
	}
	if got := blockLabel(lvl1); got != "block (2 levels) in foo" {
		t.Errorf("two levels: got %q", got)
	}
	if got := blockLabel(lvl2); got != "block (3 levels) in foo" {
		t.Errorf("three levels: got %q", got)
	}
	// A block with no enclosing scope at all is written at the top level.
	if got := blockLabel(nil); got != "block in <main>" {
		t.Errorf("no parent: got %q", got)
	}
}

// TestBlockLabelSkipsForScopes: `for` opens no Ruby scope, so a block inside a
// `for` body is one level deep, not two. The builder marks such a scope
// forScope, and blockLabel walks through it without counting it.
func TestBlockLabelSkipsForScopes(t *testing.T) {
	meth := &builder{name: "foo"}
	forB := &builder{name: "<for>", parent: meth, isBlock: true, forScope: true}
	if got := blockLabel(forB); got != "block in foo" {
		t.Errorf("block inside a for body: got %q, want %q", got, "block in foo")
	}
	// A real block inside the `for` body still adds its own level.
	inner := &builder{name: "block in foo", parent: forB, isBlock: true}
	if got := blockLabel(inner); got != "block (2 levels) in foo" {
		t.Errorf("block inside a block inside a for: got %q", got)
	}
}

// TestBlockLabelEndToEnd checks the label reaches the compiled child ISeq, which
// is what the VM reads at backtrace time.
func TestBlockLabelEndToEnd(t *testing.T) {
	iseq := compileSrc(t, "def foo\n  [1].each do\n    [2].each do\n      x = 1\n    end\n  end\nend\n")
	var names []string
	var walk func(s *bytecode.ISeq)
	walk = func(s *bytecode.ISeq) {
		for _, ch := range s.Children {
			names = append(names, ch.Name)
			walk(ch)
		}
	}
	walk(iseq)
	joined := strings.Join(names, ",")
	for _, want := range []string{"block in foo", "block (2 levels) in foo"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no child named %q among %v", want, names)
		}
	}
}

// TestRefuseKeywordAssign: MRI refuses an assignment to any of the three
// pseudo-variable keywords in the grammar — ruby 4.0.5 answers `__LINE__ = 1`
// with "Can't assign to __LINE__", a SyntaxError — and line_spec.rb pins it.
func TestRefuseKeywordAssign(t *testing.T) {
	for _, name := range []string{"__LINE__", "__FILE__", "__ENCODING__"} {
		_, err := compileString(name + " = 1")
		if err == nil {
			t.Errorf("%s = 1: compiled without error", name)
			continue
		}
		if !strings.Contains(err.Error(), "Can't assign to "+name) {
			t.Errorf("%s = 1: got %q", name, err)
		}
	}
	// An ordinary local of a similar shape is untouched.
	if _, err := compileString("__LINE__X = 1"); err != nil {
		t.Errorf("a name merely resembling a keyword was refused: %v", err)
	}
	if _, err := compileString("line = 1"); err != nil {
		t.Errorf("an ordinary local was refused: %v", err)
	}
}
