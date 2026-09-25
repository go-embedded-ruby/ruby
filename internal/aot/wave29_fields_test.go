package aot

import (
	"regexp"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
)

// TestFreezeCarriesPostCount is the guard for the failure mode a frozen prelude
// cannot show you: the emitter silently omits a new ISeq field, the regenerated
// file comes back byte-identical, everything looks fine, and every AOT-loaded
// frame has lost the information.
//
// It is not enough that the field appears — it has to come back with the value
// it went in with, including inside a nested child ISeq, where a per-scope
// emitter is most likely to skip it.
func TestFreezeCarriesPostCount(t *testing.T) {
	child := &bytecode.ISeq{
		Name: "m", Params: []string{"a", "b"}, SplatIndex: -1, PostCount: 1,
		KwRestSlot: -1, BlockSlot: -1, NumLocals: 2,
		Insns: []bytecode.Instr{{Op: bytecode.OpPushNil}, {Op: bytecode.OpReturn}},
	}
	top := &bytecode.ISeq{
		Name: "<main>", SplatIndex: -1, PostCount: 0, KwRestSlot: -1, BlockSlot: -1,
		Insns:    []bytecode.Instr{{Op: bytecode.OpPushNil}, {Op: bytecode.OpReturn}},
		Children: []*bytecode.ISeq{child},
	}
	src := FreezeISeq(top, "p", "fn", "")
	if n := strings.Count(src, "PostCount:"); n != 2 {
		t.Fatalf("PostCount emitted %d times for a 2-ISeq tree, want 2:\n%s", n, src)
	}
	// go/format aligns the struct keys, so match past the padding.
	if !regexp.MustCompile(`PostCount:\s+1,`).MatchString(src) {
		t.Errorf("the child's PostCount: 1 did not survive the freeze:\n%s", src)
	}
	if !regexp.MustCompile(`PostCount:\s+0,`).MatchString(src) {
		t.Errorf("the top-level PostCount: 0 was not written explicitly:\n%s", src)
	}
}

// TestFreezeCarriesSendFlags: Instr.Flags already carried FlagSendExplicit, and
// the keyword/positional verdict rides the same field. A send flagged only
// FlagSendNoKW must survive, which the older "omit zero" emitter would still do
// — the risk is a future emitter that special-cases the visibility bit.
func TestFreezeCarriesSendFlags(t *testing.T) {
	iseq := &bytecode.ISeq{
		Name: "<main>", SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1,
		Names: []string{"f"},
		Insns: []bytecode.Instr{
			{Op: bytecode.OpPushSelf},
			{Op: bytecode.OpSend, A: 0, B: 0, Flags: bytecode.FlagSendNoKW},
			{Op: bytecode.OpReturn},
		},
	}
	src := FreezeISeq(iseq, "p", "fn", "")
	if !regexp.MustCompile(`Flags:\s*2`).MatchString(src) {
		t.Errorf("FlagSendNoKW (2) did not survive the freeze:\n%s", src)
	}
}

// TestLoweredSendStatesTheKeywordVerdict: AOT-lowered Go has to say what the
// interpreter's OpSend handler says, or an AOT-built binary would bind
// `f(1, 2, h)` as keywords where the interpreter binds it positionally.
func TestLoweredSendStatesTheKeywordVerdict(t *testing.T) {
	positional := mainSrc(t, "h = {k: 1}\nputs(h)")
	if !strings.Contains(positional, "vm.setSendNoKW(true)") {
		t.Errorf("a positional last argument did not state its verdict:\n%s", positional)
	}
	keywords := mainSrc(t, "puts(k: 1)")
	if !strings.Contains(keywords, "vm.setSendNoKW(false)") {
		t.Errorf("a keyword last argument did not state its verdict:\n%s", keywords)
	}
	// A send carrying a literal block takes the other arm of emitSend.
	withBlock := mainSrc(t, "h = {k: 1}\n[1].each { |x| puts(h) }")
	if !strings.Contains(withBlock, "vm.setSendNoKW(true)") {
		t.Errorf("the literal-block arm did not state its verdict:\n%s", withBlock)
	}
}
