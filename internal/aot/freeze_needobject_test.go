package aot

import (
	"go/parser"
	"go/token"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// noUnusedImports is the general guard, not an assertion about one package: it
// parses the generated file and requires that every import it declares is
// actually named somewhere in the body. Go rejects an unused import outright, so
// a gate that is wrong in the permissive direction turns the nested `go build`
// of `rbgo build --closed` into a hard failure -- which is exactly what #717
// was. Written against the emitted source rather than against a list of package
// names kept here, so a new gated import is covered the day it is added.
func noUnusedImports(t *testing.T, src string) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "gen.go", src, 0)
	if err != nil {
		t.Fatalf("generated source does not parse: %v\n%s", err, src)
	}
	_, body, _ := strings.Cut(src, "\n)\n\n")
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("unquoting import %s: %v", imp.Path.Value, err)
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if !strings.Contains(body, name+".") {
			t.Errorf("import %q is declared but never used in the body -- go build would reject this file\n%s", path, src)
		}
	}
	if len(f.Imports) == 0 {
		t.Errorf("expected at least the bytecode import, got none\n%s", src)
	}
}

// TestFreezeObjectImportIsGated pins #717 in both directions. internal/object
// reaches the generated source only through the constant pool and through the
// frozenFloat helper, unlike internal/bytecode which the constructor's own
// signature always names. Before the fix the import was emitted
// unconditionally, so a program whose frozen bytecode holds no constant at all
// -- `p ARGV` is enough -- produced a file the nested go build rejected.
func TestFreezeObjectImportIsGated(t *testing.T) {
	const objectImport = `"github.com/go-embedded-ruby/ruby/internal/object"`

	bare := &bytecode.ISeq{Name: "bare", SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1}
	out := FreezeISeq(bare, "main", "embeddedProgram", "")
	if strings.Contains(out, objectImport) {
		t.Errorf("an ISeq with no constant pool must not import internal/object\n%s", out)
	}
	noUnusedImports(t, out)

	// The other direction, one case per emitting path, so a gate that is wrong
	// the STRICT way -- omitting an import the body needs -- fails too. Float is
	// listed separately because it reaches object.Float through the frozenFloat
	// helper rather than through writeConst's own output.
	for _, tc := range []struct {
		name string
		v    object.Value
	}{
		{"Integer", object.IntValue(7)},
		{"Symbol", object.Symbol("sym")},
		{"String", object.NewString("s")},
		{"Float", object.Float(1.5)},
		{"Bignum", &object.Bignum{I: big.NewInt(3)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			iseq := &bytecode.ISeq{Name: tc.name, SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1, Consts: []object.Value{tc.v}}
			got := FreezeISeq(iseq, "main", "embeddedProgram", "")
			if !strings.Contains(got, objectImport) {
				t.Errorf("a %s constant needs internal/object, but the import was omitted\n%s", tc.name, got)
			}
			noUnusedImports(t, got)
		})
	}

	// An EMPTY but non-nil pool still emits `Consts: []object.Value{}`, so it
	// needs the import although it carries no value to emit it for. This is the
	// case a gate keyed on "did writeConst run" would get wrong.
	empty := &bytecode.ISeq{Name: "empty", SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1, Consts: []object.Value{}}
	if got := FreezeISeq(empty, "main", "embeddedProgram", ""); !strings.Contains(got, objectImport) {
		t.Errorf("an empty-but-non-nil constant pool still names object.Value\n%s", got)
	}

	// A CHILD's constant pool must pull the import in as well: the flag is set
	// during body construction and read when the header is written, so a child
	// visited after the parent still counts.
	kid := &bytecode.ISeq{Name: "kid", SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1, Consts: []object.Value{object.IntValue(1)}}
	parent := &bytecode.ISeq{Name: "parent", SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1, Children: []*bytecode.ISeq{kid}}
	got := FreezeISeq(parent, "main", "embeddedProgram", "")
	if !strings.Contains(got, objectImport) {
		t.Errorf("a child's constant pool must pull internal/object in\n%s", got)
	}
	noUnusedImports(t, got)
}
