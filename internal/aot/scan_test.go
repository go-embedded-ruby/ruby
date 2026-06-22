package aot

import (
	"reflect"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

func frontendUsesOf(t *testing.T, src string) []string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatal(err)
	}
	return FrontendUses(iseq)
}

func TestFrontendUses(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{"puts 1 + 2\n", nil},
		{`eval("1")` + "\n", []string{"eval"}},
		// sorted + de-duplicated, found at top level and inside a nested body.
		{"require \"json\"\ndef f\n  eval(\"x\")\n  eval(\"y\")\nend\n", []string{"eval", "require"}},
		// bare `binding` is an intrinsic (works closed); only Binding#eval (the
		// `eval` selector) needs the front-end.
		{"b = binding\nrequire_relative \"x\"\nb.eval(\"1\")\n", []string{"eval", "require_relative"}},
	}
	for _, c := range cases {
		got := frontendUsesOf(t, c.src)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("FrontendUses(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}

// TestFrontendUsesNilChild: a nil child in the tree is skipped, not a panic.
func TestFrontendUsesNilChild(t *testing.T) {
	iseq := &bytecode.ISeq{
		Names:    []string{"eval"},
		Children: []*bytecode.ISeq{nil},
	}
	if got := FrontendUses(iseq); !reflect.DeepEqual(got, []string{"eval"}) {
		t.Errorf("got %v, want [eval]", got)
	}
}
