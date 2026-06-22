package aot

import (
	"errors"
	"go/format"
	"math/big"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// TestFreezeRealProgram freezes a real compiled program and asserts the output
// is valid, gofmt-clean Go that reconstructs the constant pool and nested method.
func TestFreezeRealProgram(t *testing.T) {
	prog, err := parser.Parse("x = 5\ndef sq(n) = n * n\nputs sq(x) + 1.5\n")
	if err != nil {
		t.Fatal(err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatal(err)
	}
	out := FreezeISeq(iseq, "vm", "embeddedProgram", "rbgo_closed")

	if _, err := format.Source([]byte(out)); err != nil {
		t.Fatalf("output is not valid Go: %v\n%s", err, out)
	}
	for _, want := range []string{
		"//go:build rbgo_closed",
		"package vm",
		"func embeddedProgram() *bytecode.ISeq {",
		"object.Integer(5)",
		"frozenFloat(0x3ff8000000000000)", // 1.5 bit-exact
		`Names:       []string{"sq", "puts"}`,
		"SplatIndex:  -1,",
		"func frozenFloat(bits uint64) object.Float {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

// TestFreezeAllConstKinds exercises every constant the emitter supports plus the
// remaining structural branches (frozen string, bignum, nested child, the -1
// defaults, no build tag).
func TestFreezeAllConstKinds(t *testing.T) {
	bigVal, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	child := &bytecode.ISeq{
		Name:       "inner",
		Insns:      []bytecode.Instr{{Op: 1}},
		Params:     []string{"a"},
		KwNames:    []string{"k"},
		KwRequired: []bool{true},
		SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1,
	}
	iseq := &bytecode.ISeq{
		Name: "top",
		// Exercises every writeInstr branch: Op+A+B+C set, and an all-zero instr.
		Insns: []bytecode.Instr{{Op: 1, A: 2, B: 3, C: 4}, {}},
		Consts: []object.Value{
			object.Integer(-7),
			object.Symbol("sym"),
			object.NewString("hi\n"),
			&object.String{B: []byte("frozen"), Frozen: true},
			object.Float(2.5),
			&object.Bignum{I: bigVal},
		},
		Names:      []string{"m"},
		SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1,
		Children: []*bytecode.ISeq{child},
		Super:    "Base",
	}
	out := FreezeISeq(iseq, "main", "embeddedProgram", "")

	if _, err := format.Source([]byte(out)); err != nil {
		t.Fatalf("output is not valid Go: %v\n%s", err, out)
	}
	if strings.Contains(out, "//go:build") {
		t.Errorf("no build tag was requested, but one was emitted\n%s", out)
	}
	// gofmt realigns struct-field columns, so collapse whitespace before matching.
	norm := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		"object.Integer(-7)",
		`object.Symbol("sym")`,
		`object.NewString("hi\n")`,
		`&object.String{B: []byte("frozen"), Frozen: true}`,
		"frozenFloat(0x4004000000000000)", // 2.5
		`&object.Bignum{I: frozenBig("123456789012345678901234567890")}`,
		`frozenBig(s string) *big.Int`,
		`"math/big"`,
		`Super: "Base"`,
		`KwRequired: []bool{true}`,
		`Name: "inner"`,
	} {
		if !strings.Contains(norm, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

// TestFreezeNilAndEmpty: a nil ISeq round-trips to `nil`; nil slices are omitted
// (so the zero value reconstructs them), distinguishing nil from empty.
func TestFreezeNilAndEmpty(t *testing.T) {
	if got := FreezeISeq(nil, "vm", "f", ""); !strings.Contains(got, "return nil") {
		t.Errorf("nil ISeq should freeze to `return nil`, got\n%s", got)
	}

	// An ISeq whose slices are all nil must not emit Insns/Consts/Names/etc.
	bare := &bytecode.ISeq{Name: "bare", SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1}
	out := FreezeISeq(bare, "vm", "f", "")
	// Tokens unambiguous against always-emitted fields ("Locals:"⊂"NumLocals:",
	// "Names:"⊂"KwNames:" — so those two are validated indirectly).
	for _, absent := range []string{"Insns:", "Consts:", "Params:", "KwNames:", "KwRequired:", "Children:"} {
		if strings.Contains(out, absent) {
			t.Errorf("nil slice should be omitted, but found %q\n%s", absent, out)
		}
	}
}

// TestFreezeUnsupportedConst panics: a value the compiler never pools cannot be
// frozen, and a silent skip would corrupt the program.
func TestFreezeUnsupportedConst(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected a panic for an unsupported constant")
		}
	}()
	iseq := &bytecode.ISeq{
		Consts:     []object.Value{&object.Array{}},
		SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1,
	}
	FreezeISeq(iseq, "vm", "f", "")
}

// TestFreezeFormatFailure: if go/format rejects the source (only possible via a
// serializer bug), FreezeISeq returns the raw source so the build fails loudly.
func TestFreezeFormatFailure(t *testing.T) {
	orig := formatSource
	defer func() { formatSource = orig }()
	formatSource = func([]byte) ([]byte, error) { return nil, errors.New("boom") }

	iseq := &bytecode.ISeq{Name: "x", SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1}
	out := FreezeISeq(iseq, "vm", "f", "")
	if !strings.Contains(out, "package vm") || !strings.Contains(out, "DO NOT EDIT") {
		t.Errorf("format failure should return raw source, got\n%s", out)
	}
}
