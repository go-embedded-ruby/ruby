package compiler

import (
	"math"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
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

// compileDefinedCall tags a receiver-less, argument-less, block-less call as
// "local-variable" when the name resolves to a local in scope. The parser
// classifies a bare known local as a *ast.VarRef, so the Call shape that lands
// in compileDefinedCall and still resolves to a local (e.g. `foo()` after
// `foo = 1`) is synthesized directly to drive that arm.
func TestCompileDefinedCallLocal(t *testing.T) {
	prog := &ast.Program{Body: []ast.Node{
		&ast.Assign{Name: "foo", Value: &ast.IntLit{Value: 1}},
		&ast.Call{Name: "defined?", Args: []ast.Node{
			&ast.Call{Name: "foo"},
		}},
	}}
	iseq, err := Compile(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !iseqHasStringConst(iseq, "local-variable") {
		t.Fatalf("expected a \"local-variable\" tag in the constant pool, got %#v", iseq.Consts)
	}
}

func iseqHasStringConst(iseq *bytecode.ISeq, want string) bool {
	for _, c := range iseq.Consts {
		if s, ok := c.(*object.String); ok && s.Str() == want {
			return true
		}
	}
	return false
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

// TestRegexpNamedCaptures drives the static extraction of `=~` capture-group
// names directly, covering escaped characters, character classes, lookbehind
// (which is not a capture), the `(?'name'…)` spelling, de-duplication, and names
// that are not valid local identifiers (skipped). Each expectation matches how
// MRI introduces capture locals for a literal regexp on the left of `=~`.
func TestRegexpNamedCaptures(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{`(?<a>.)(?<b>.)`, []string{"a", "b"}},
		{`\((?<c>.)`, []string{"c"}},          // an escaped paren opens no group
		{`\\(?<d>.)`, []string{"d"}},          // an escaped backslash is skipped as a pair
		{`[()<>?](?<e>.)`, []string{"e"}},     // group syntax inside a class is literal
		{`(?<=x)(?<f>.)`, []string{"f"}},      // positive lookbehind is not a capture
		{`(?<!x)(?<g>.)`, []string{"g"}},      // negative lookbehind is not a capture
		{`(?'h'.)`, []string{"h"}},            // the quoted spelling
		{`(?<a>.)(?<a>.)`, []string{"a"}},     // a repeated name is recorded once
		{`(?<Bad>.)(?<ok>.)`, []string{"ok"}}, // a non-local name (uppercase) is skipped
		{`(?<>.)(?<i>.)`, []string{"i"}},      // an empty name is skipped
		{`no captures here`, nil},             // a plain pattern has none
	}
	for _, tc := range cases {
		got := regexpNamedCaptures(tc.src)
		if len(got) != len(tc.want) {
			t.Fatalf("regexpNamedCaptures(%q) = %v, want %v", tc.src, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("regexpNamedCaptures(%q) = %v, want %v", tc.src, got, tc.want)
			}
		}
	}
}

// TestIsLocalName covers the identifier-validity predicate used to decide which
// capture names become locals: empty, a bad leading character, a bad interior
// character, and the accepted shapes (leading letter/underscore, then word
// characters including uppercase and digits).
func TestIsLocalName(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"", false},
		{"a", true},
		{"_x", true},
		{"abc123", true},
		{"a_B9", true},
		{"Foo", false}, // uppercase leading char
		{"1ab", false}, // digit leading char
		{"a-b", false}, // invalid interior char
	}
	for _, tc := range cases {
		if got := isLocalName(tc.s); got != tc.want {
			t.Fatalf("isLocalName(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

// TestRewriteAnonKwSplatUnchanged drives the no-op path of the anonymous `**`
// hash rewrite: an ordinary keyword hash (no bare `**` marker) is returned as-is.
func TestRewriteAnonKwSplatUnchanged(t *testing.T) {
	c := &Compiler{}
	c.push(newBuilder("<t>", nil))
	h := &ast.HashLit{Keys: []ast.Node{&ast.SymbolLit{Name: "a"}}, Values: []ast.Node{&ast.IntLit{Value: 1}}}
	got, changed := c.rewriteAnonKwSplat(h)
	if changed || got != h {
		t.Fatalf("rewriteAnonKwSplat(explicit hash) = (%p, %v), want (%p, false)", got, changed, h)
	}
}

// TestAddConstPoolsFloatsByBits pins the reason Float literals do not share the
// value-keyed constant pool. Two floats can be equal without being the same
// number, and one can be unequal to itself, so equality is the wrong question
// to ask of a literal that has to survive to run time exactly as written.
func TestAddConstPoolsFloatsByBits(t *testing.T) {
	b := newBuilder("<t>", nil)
	negZero := object.Float(math.Copysign(0, -1))
	posZero := object.Float(0)
	nan := object.Float(math.NaN())

	// -0.0 == 0.0, so a value-keyed pool hands the second literal the first
	// one's slot and the file's later mention prints as its earlier one.
	i, j := b.addConst(posZero), b.addConst(negZero)
	if i == j {
		t.Fatalf("0.0 and -0.0 share slot %d; they are equal but not the same number", i)
	}
	if got := math.Signbit(float64(b.consts[i].(object.Float))); got {
		t.Errorf("slot %d holds a negative zero, want positive", i)
	}
	if got := math.Signbit(float64(b.consts[j].(object.Float))); !got {
		t.Errorf("slot %d holds a positive zero, want negative", j)
	}

	// The same number asked for twice keeps one slot, which is the whole point
	// of pooling — including NaN, which is equal to nothing, itself included,
	// and so took a fresh slot on every mention.
	if a, c := b.addConst(posZero), b.addConst(posZero); a != c || a != i {
		t.Errorf("0.0 pooled to %d then %d, want %d both times", a, c, i)
	}
	if a, c := b.addConst(nan), b.addConst(nan); a != c {
		t.Errorf("NaN took slots %d and %d, want one slot", a, c)
	}

	// Non-Float constants still go through the value-keyed pool.
	if a, c := b.addConst(object.Integer(7)), b.addConst(object.Integer(7)); a != c {
		t.Errorf("Integer(7) took slots %d and %d, want one slot", a, c)
	}
}
