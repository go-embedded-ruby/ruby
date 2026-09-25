package compiler

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-ruby-parser/parser"
	"github.com/go-ruby-parser/parser/ast"
)

// lastSendFlags returns the Flags of the deepest-last send-shaped instruction in
// the ISeq tree: children first (a `super` or a `yield` lives in the method body
// the source wraps it in), then this scope, scanning backwards. Every construct
// under test lowers to exactly one send of interest, and it is the last one
// written.
func lastSendFlags(iseq *bytecode.ISeq) (int, bool) {
	for i := len(iseq.Children) - 1; i >= 0; i-- {
		if f, ok := lastSendFlags(iseq.Children[i]); ok {
			return f, true
		}
	}
	for i := len(iseq.Insns) - 1; i >= 0; i-- {
		switch iseq.Insns[i].Op {
		case bytecode.OpSend, bytecode.OpSendArray, bytecode.OpSendBlockArg,
			bytecode.OpSendArrayBlockArg, bytecode.OpInvokeBlock,
			bytecode.OpInvokeBlockArray, bytecode.OpInvokeSuper, bytecode.OpInvokeSuperArray:
			return iseq.Insns[i].Flags, true
		}
	}
	return 0, false
}

func sendFlagsOf(t *testing.T, src string) int {
	t.Helper()
	iseq, err := compileString(src)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	f, ok := lastSendFlags(iseq)
	if !ok {
		t.Fatalf("no send instruction compiled for %q", src)
	}
	return f
}

// TestSendNoKWFlagAtTheCallSite pins the compile-time half of MRI's
// keyword/positional decision: FlagSendNoKW is set exactly when the last written
// argument is something other than a hash literal, which is the only case
// go-ruby-parser v0.3.0 lets the compiler decide (setup_parameters_complex reads
// VM_CALL_KWARG / VM_CALL_KW_SPLAT, vm_args.c v3_4_0:591).
func TestSendNoKWFlagAtTheCallSite(t *testing.T) {
	for _, tc := range []struct {
		src   string
		noKW  bool
		kwspl bool
	}{
		{src: "f(1, 2, h)", noKW: true},                       // a local/method value: positional
		{src: "f(1, 2, k: 42)", noKW: false},                  // literal keywords
		{src: "f(1, 2, {k: 42})", noKW: false},                // SAME AST as the line above (parser gap)
		{src: "f(1, 2, **h)", noKW: false, kwspl: true},       // a keyword splat
		{src: "f(*a)", noKW: true},                            // a splat is not keywords
		{src: "f(*a, h)", noKW: true},                         // splat + positional tail
		{src: "f(*a, **h)", noKW: false, kwspl: true},         // splat + keyword splat
		{src: "f()", noKW: false},                             // no last argument to protect
		{src: "f(1, &blk)", noKW: true},                       // a block-pass is not an argument
		{src: "f(k: 1, &blk)", noKW: false},                   // ... even parked before the keywords
		{src: "o.m(x)", noKW: true},                           // explicit receiver keeps both flags
		{src: "f(1) { }", noKW: true},                         // literal block, static argc
		{src: "f(*a) { }", noKW: true},                        // literal block, array argc
		{src: "yield x", noKW: true},                          // yield is an ordinary call site
		{src: "yield(k: 1)", noKW: false},                     //
		{src: "yield(*a, **h)", noKW: false, kwspl: true},     //
		{src: "def m; super(x); end", noKW: true},             // so is an explicit super
		{src: "def m; super(k: 1); end", noKW: false},         //
		{src: "def m; super(*a, **h); end", kwspl: true},      //
		{src: "def m(...); g(...); end", kwspl: true},         // `...` forwards a kwsplat
		{src: "def m(...); super(...); end", kwspl: true},     //
		{src: "def m; super; end", noKW: false},               // bare super forwards keywords AS keywords
		{src: "a[i] = v", noKW: true},                         // a setter is a call site too
		{src: "a[*i] = v", noKW: true},                        //
		{src: "o.x = h", noKW: true},                          //
		{src: "f(1, 2, h, &blk)", noKW: true},                 // block-pass after the positional tail
		{src: "f(1, 2, **h, &blk)", noKW: false, kwspl: true}, //
	} {
		src := tc.src
		if strings.HasPrefix(src, "def m; super") || strings.HasPrefix(src, "def m(...)") {
			// A super/forward only compiles inside a def; the helper already wraps it.
			src = "class K\n" + src + "\nend"
		}
		flags := sendFlagsOf(t, src)
		if got := flags&bytecode.FlagSendNoKW != 0; got != tc.noKW {
			t.Errorf("%q: FlagSendNoKW = %v, want %v", tc.src, got, tc.noKW)
		}
		if got := flags&bytecode.FlagSendKWSplat != 0; got != tc.kwspl {
			t.Errorf("%q: FlagSendKWSplat = %v, want %v", tc.src, got, tc.kwspl)
		}
	}
}

// TestLastArgIsPositional drives the helper directly over the shapes the call
// compilers cannot reach: an empty list, and a list that is nothing but a
// block-pass (both have no last ARGUMENT, so neither is protected).
func TestLastArgIsPositional(t *testing.T) {
	if lastArgIsPositional(nil) {
		t.Error("no arguments: want false")
	}
	if lastArgIsPositional([]ast.Node{&ast.BlockPass{Value: &ast.VarRef{Name: "b"}}}) {
		t.Error("only a block-pass: want false")
	}
	if !lastArgIsPositional([]ast.Node{&ast.IntLit{Value: 1}}) {
		t.Error("an integer literal: want true")
	}
	if lastArgIsPositional([]ast.Node{&ast.HashLit{}}) {
		t.Error("a hash literal: want false (the parser cannot say whether it was braced)")
	}
}

// TestEmptyKwSplatIsAppendedForACall: the compiler no longer decides about an
// empty `**kw` — it always concatenates the hash and lets the VM drop it
// (ignore_keyword_hash_p, vm_args.c v3_4_0:506). The old `empty?` test is what
// disappeared, so its absence is what this checks; the non-call uses of
// compileSplatItems keep it.
func TestEmptyKwSplatIsAppendedForACall(t *testing.T) {
	hasEmptyQ := func(src string) bool {
		iseq, err := compileString(src)
		if err != nil {
			t.Fatalf("compile %q: %v", src, err)
		}
		for _, in := range iseq.Insns {
			if in.Op == bytecode.OpSend && iseq.Names[in.A] == "empty?" {
				return true
			}
		}
		return false
	}
	if hasEmptyQ("f(*a, **h)") {
		t.Error("a call argument list still tests the kwsplat for emptiness in the compiler")
	}
	// A multiple-assignment index TARGET and an index op-assign are not call
	// argument lists compiled by compileCall; they keep the older lowering.
	if !hasEmptyQ("a[*i, **h] += 1") {
		t.Error("an index op-assign should keep the compiler-side empty-kwsplat drop")
	}
	if !hasEmptyQ("a[*i, **h], b = 1, 2") {
		t.Error("a masgn index target should keep the compiler-side empty-kwsplat drop")
	}
}

// TestDuplicatedArgumentName pins shadowing_lvar_0 (parse.y v3_4_0:13589),
// including the is_private_local_id exemption (:13578) and the anonymous forms,
// which declare no name at all. Every expectation was run against MRI 4.0.5.
func TestDuplicatedArgumentName(t *testing.T) {
	for _, tc := range []struct {
		src    string
		refuse bool
	}{
		{"def m(a, a); end", true},
		{"def m(a, *a); end", true},
		{"def m(a, *b, a); end", true},
		{"def m(a, a:); end", true},
		{"def m(a, **a); end", true},
		{"def m(a, b: 1, &a); end", true},
		{"proc { |x, x| }", true},
		{"proc { |*y, y| }", true},
		{"proc { |x; x| }", true},
		{"proc { |; q, q| }", true},
		{"proc { |a, &a| }", true},
		{"def m(_, _); end", false},
		{"def m(_a, _a); end", false},
		{"proc { |_x; _x| }", false},
		{"def m(*, **, &); end", false},
		{"def m(...); end", false},
		{"def m(a, b); end", false},
		{"z = 1; proc { |; z| }", false},
		{"def m(a, (b, c)); end", false},
		{"proc { |(a, b), (c, d)| }", false},
	} {
		_, err := compileString(tc.src)
		refused := err != nil && strings.Contains(err.Error(), "duplicated argument name")
		if refused != tc.refuse {
			t.Errorf("%q: refused = %v (err %v), want %v", tc.src, refused, err, tc.refuse)
		}
	}
}

// TestParamCheckName drives the sigil stripping directly, including the entries
// only a block's folded parameter list produces.
func TestParamCheckName(t *testing.T) {
	for in, want := range map[string]string{
		"a":      "a",
		"**rest": "rest",
		"*":      "",
		"**":     "",
		"&":      "",
		"&blk":   "blk",
		"kw:":    "kw",
		"_":      "",
		"_x":     "",
		"(0)":    "",
		"":       "",
	} {
		if got := paramCheckName(in); got != want {
			t.Errorf("paramCheckName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPostCountNoSplat pins the count against the parameter shapes MRI allows.
// An optional AFTER a post parameter is a SyntaxError, so the optionals are one
// contiguous run and the post count is the distance from the last of them to the
// end (args_setup_post_parameters, vm_args.c v3_4_0:887).
func TestPostCountNoSplat(t *testing.T) {
	d := &ast.IntLit{Value: 1}
	for _, tc := range []struct {
		name     string
		nparams  int
		defaults []ast.Node
		splat    int
		want     int
	}{
		{"no optional at all", 2, []ast.Node{nil, nil}, -1, 0},
		{"m(a=1, b)", 2, []ast.Node{d, nil}, -1, 1},
		{"m(a, b=2, c)", 3, []ast.Node{nil, d, nil}, -1, 1},
		{"m(a, b=1, c=2, d)", 4, []ast.Node{nil, d, d, nil}, -1, 1},
		{"m(a=5, b, c, d)", 4, []ast.Node{d, nil, nil, nil}, -1, 3},
		{"m(a, b=1) — trailing optional", 2, []ast.Node{nil, d}, -1, 0},
		{"a splat owns its own post run", 4, []ast.Node{nil, d, nil, nil}, 2, 0},
	} {
		if got := postCountNoSplat(tc.nparams, tc.defaults, tc.splat); got != tc.want {
			t.Errorf("%s: postCountNoSplat = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestPostCountIsCompiled checks the field actually lands on the ISeq for both a
// method and a block, and stays 0 for the shapes it does not describe.
func TestPostCountIsCompiled(t *testing.T) {
	methodPost := func(src string) (int, int) {
		iseq, err := compileString(src)
		if err != nil {
			t.Fatalf("compile %q: %v", src, err)
		}
		child := iseq.Children[0]
		return child.PostCount, child.NumRequired
	}
	if got, req := methodPost("def m(a=1, b); end"); got != 1 || req != 0 {
		t.Errorf("def m(a=1, b): PostCount=%d NumRequired=%d, want 1/0", got, req)
	}
	if got, req := methodPost("def m(a, b=2, c); end"); got != 1 || req != 1 {
		t.Errorf("def m(a, b=2, c): PostCount=%d NumRequired=%d, want 1/1", got, req)
	}
	if got, _ := methodPost("def m(a, b); end"); got != 0 {
		t.Errorf("def m(a, b): PostCount=%d, want 0", got)
	}
	if got, _ := methodPost("def m(a, b=9, *c, d); end"); got != 0 {
		t.Errorf("a splat shape records its post run through SplatIndex, not PostCount: got %d", got)
	}
	blk, err := compileString("f { |a=5, b, c, d| }")
	if err != nil {
		t.Fatalf("compile block: %v", err)
	}
	if got := blk.Children[0].PostCount; got != 3 {
		t.Errorf("{ |a=5, b, c, d| }: PostCount=%d, want 3", got)
	}
}

// TestNumberedParameterNameIsReserved pins the reserved set exactly: `_1`..`_9`
// cannot be BOUND as a local anywhere, while `_0`, `_10` and `@_1` are ordinary
// identifiers. Every expectation was run against MRI 4.0.5.
func TestNumberedParameterNameIsReserved(t *testing.T) {
	for _, tc := range []struct {
		src    string
		refuse bool
	}{
		{"_1 = 0", true},
		{"_9 = 0", true},
		{"proc { _1 = 0 }", true},
		{"x = 1; _1 = 2", true},
		{"_1, x = 1, 2", true},
		{"for _1 in [1]; end", true},
		{"begin; rescue => _1; end", true},
		{"_0 = 0", false},
		{"_10 = 0", false},
		{"@_1 = 1", false},
		{"_ = 1", false},
		{"_1a = 1", false},
		{"proc { _1 }", false}, // the parameter itself, synthesised by the parser
	} {
		_, err := compileString(tc.src)
		refused := err != nil && strings.Contains(err.Error(), "reserved for numbered parameters")
		if refused != tc.refuse {
			t.Errorf("%q: refused = %v (err %v), want %v", tc.src, refused, err, tc.refuse)
		}
	}
	if !isNumberedParamName("_1") || !isNumberedParamName("_9") {
		t.Error("_1/_9 must be reserved")
	}
	for _, n := range []string{"_0", "_10", "_", "_a", "", "a1", "_1_"} {
		if isNumberedParamName(n) {
			t.Errorf("%q must not be reserved", n)
		}
	}
}

// TestImplicitBlockParamsAreHidden pins both halves: the discriminator that
// tells a SYNTHESISED parameter list from a written one, and what is blanked.
//
// The discriminator is an artefact of go-ruby-parser v0.3.0 — a written list
// pads Defaults to one entry per parameter, a synthesised one leaves it empty —
// so it is pinned here on purpose: if the parser ever pads a synthesised list,
// this fails rather than letting `it` quietly become a visible local again.
func TestImplicitBlockParamsAreHidden(t *testing.T) {
	blockOf := func(t *testing.T, src string) *bytecode.ISeq {
		t.Helper()
		iseq, err := compileString(src)
		if err != nil {
			t.Fatalf("compile %q: %v", src, err)
		}
		return iseq.Children[0]
	}

	// The discriminator, over every parameter shape the parser writes out.
	implicit := func(src string) bool {
		prog := mustBlock(t, src)
		return blockParamsAreImplicit(prog)
	}
	for src, want := range map[string]bool{
		"f { }":         false,
		"f { |a| }":     false,
		"f { |a, b| }":  false,
		"f { |a=1| }":   false,
		"f { |*a| }":    false,
		"f { |it| it }": false,
		"f { |x| it }":  false,
		"f { it }":      true,
		"f { _1 }":      true,
		"f { _1 + _3 }": true,
	} {
		if got := implicit(src); got != want {
			t.Errorf("blockParamsAreImplicit(%q) = %v, want %v", src, got, want)
		}
	}

	// `it` loses its name in BOTH tables: #parameters reads Params, a Binding
	// reads Locals.
	itBlk := blockOf(t, "f { it }")
	if len(itBlk.Params) != 1 || itBlk.Params[0] != "" {
		t.Errorf("f { it }: Params = %q, want one empty name", itBlk.Params)
	}
	if len(itBlk.Locals) != 1 || itBlk.Locals[0] != "" {
		t.Errorf("f { it }: Locals = %q, want one empty name", itBlk.Locals)
	}
	// A WRITTEN |it| keeps its name.
	writtenIt := blockOf(t, "f { |it| it }")
	if len(writtenIt.Params) != 1 || writtenIt.Params[0] != "it" {
		t.Errorf("f { |it| it }: Params = %q, want [\"it\"]", writtenIt.Params)
	}
	// The numbered parameters keep theirs, in both tables.
	np := blockOf(t, "f { _1 + _2 }")
	if len(np.Params) != 2 || np.Params[0] != "_1" || np.Params[1] != "_2" {
		t.Errorf("f { _1 + _2 }: Params = %q, want [\"_1\" \"_2\"]", np.Params)
	}
	if len(np.Locals) < 2 || np.Locals[0] != "_1" || np.Locals[1] != "_2" {
		t.Errorf("f { _1 + _2 }: Locals = %q, want the numbered names kept", np.Locals)
	}
}

// mustBlock parses src (one call carrying one block) and returns that block.
func mustBlock(t *testing.T, src string) *ast.Block {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	call, ok := prog.Body[0].(*ast.Call)
	if !ok || call.Block == nil {
		t.Fatalf("%q did not parse as a call carrying a block", src)
	}
	return call.Block
}
