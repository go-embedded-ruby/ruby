package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestFormatBacktraceEntryOmitsZeroLine pins location_format's easily-missed
// rule (vm_backtrace.c v3_4_0:446): the ":%d" is appended only when the line is
// non-zero, so a frame that cannot be placed says nothing about its line rather
// than claiming line zero.
func TestFormatBacktraceEntryOmitsZeroLine(t *testing.T) {
	if got := formatBacktraceEntry("/a.rb", 0, "foo"); got != "/a.rb:in 'foo'" {
		t.Errorf("zero line: got %q", got)
	}
	if got := formatBacktraceEntry("/a.rb", 12, "foo"); got != "/a.rb:12:in 'foo'" {
		t.Errorf("real line: got %q", got)
	}
}

// TestFrameLineFallbacks drives frameLine's four answers directly: out of
// range, a frame with no ISeq, the mapped pc, and the first_lineno fallback
// rb_vm_get_sourceline takes when the map cannot place the pc
// (vm_backtrace.c v3_4_0:105).
func TestFrameLineFallbacks(t *testing.T) {
	vm := New(nil)
	vm.frameISeqs = []*bytecode.ISeq{
		nil,
		{Lines: []bytecode.LineEntry{{PC: 0, Line: 31}}},
		{FirstLine: 8}, // no map at all: falls back
		{FirstLine: 8, Lines: []bytecode.LineEntry{{PC: 4, Line: 40}}},
	}
	vm.framePCs = []int{0, 0, 0, 0}

	if got := vm.frameLine(-1); got != 0 {
		t.Errorf("negative index: got %d, want 0", got)
	}
	if got := vm.frameLine(99); got != 0 {
		t.Errorf("past the end: got %d, want 0", got)
	}
	if got := vm.frameLine(0); got != 0 {
		t.Errorf("nil ISeq: got %d, want 0", got)
	}
	if got := vm.frameLine(1); got != 31 {
		t.Errorf("mapped pc: got %d, want 31", got)
	}
	if got := vm.frameLine(2); got != 8 {
		t.Errorf("no map: got %d, want the FirstLine 8", got)
	}
	// pc 0 is BEFORE the only entry (at pc 4), so the map cannot place it and
	// the first_lineno fallback answers instead.
	if got := vm.frameLine(3); got != 8 {
		t.Errorf("unplaceable pc: got %d, want the FirstLine 8", got)
	}
	vm.framePCs[3] = 4
	if got := vm.frameLine(3); got != 40 {
		t.Errorf("placeable pc: got %d, want 40", got)
	}
}

// TestSetFrameCodeSizesToDepth pins the invariant that keeps the code stacks
// aligned with frameNames without any unwind bookkeeping of their own: a push
// sizes them to exactly depth+1, so an abandoned deeper entry is overwritten by
// the next frame to occupy that depth instead of shifting every index after it.
func TestSetFrameCodeSizesToDepth(t *testing.T) {
	vm := New(nil)
	a := &bytecode.ISeq{Name: "a"}
	b := &bytecode.ISeq{Name: "b"}

	vm.setFrameCode(3, a)
	if len(vm.frameISeqs) != 4 || len(vm.framePCs) != 4 {
		t.Fatalf("grew to %d/%d, want 4/4", len(vm.frameISeqs), len(vm.framePCs))
	}
	if vm.frameISeqs[3] != a {
		t.Error("slot 3 does not hold the pushed ISeq")
	}
	vm.framePCs[3] = 17

	// A shallower push must SHRINK them, discarding the abandoned entry.
	vm.setFrameCode(1, b)
	if len(vm.frameISeqs) != 2 || len(vm.framePCs) != 2 {
		t.Fatalf("shrank to %d/%d, want 2/2", len(vm.frameISeqs), len(vm.framePCs))
	}
	if vm.frameISeqs[1] != b {
		t.Error("slot 1 does not hold the pushed ISeq")
	}
	if vm.framePCs[1] != 0 {
		t.Errorf("pc not reset on push: %d", vm.framePCs[1])
	}
}

// TestFrameLabelFromISeq covers the non-method frame labels, which all rendered
// "<main>" before the ISeq could be consulted.
func TestFrameLabelFromISeq(t *testing.T) {
	vm := New(nil)
	vm.frameNames = []string{"foo", "", "", "", "", ""}
	vm.frameISeqs = []*bytecode.ISeq{
		{Name: "foo"},
		{Name: "<class:K>"},
		{Name: "<singleton class>"},
		{Name: "block in foo"},
		{Name: ""},
		nil,
	}
	vm.framePCs = make([]int, 6)
	want := []string{"foo", "<class:K>", "singleton class", "block in foo", "<main>", "<main>"}
	for i, w := range want {
		if got := vm.frameLabel(i); got != w {
			t.Errorf("frameLabel(%d) = %q, want %q", i, got, w)
		}
	}
	// A frame whose index outruns the ISeq stack falls back too.
	vm.frameISeqs = nil
	if got := vm.frameLabel(1); got != "<main>" {
		t.Errorf("no ISeq stack: got %q", got)
	}
}

// TestStripBlockQualifier covers the three shapes base_label has to undo. MRI
// keeps label and base_label in separate fields (vm_backtrace.c v3_4_0:331);
// the difference between them is exactly this decoration.
func TestStripBlockQualifier(t *testing.T) {
	cases := map[string]string{
		"block in foo":             "foo",
		"block (2 levels) in foo":  "foo",
		"block (17 levels) in C#m": "C#m",
		"foo":                      "foo",
		"<class:K>":                "<class:K>",
		// "block (" without the levels marker is not a qualifier we made.
		"block (weird": "block (weird",
	}
	for in, want := range cases {
		if got := stripBlockQualifier(in); got != want {
			t.Errorf("stripBlockQualifier(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBacktraceLocationBaseLabelStrips is the end-to-end of the above: the
// Location built from a block frame's captured string reports the undecorated
// label from #base_label and the decorated one from #label.
func TestBacktraceLocationBaseLabelStrips(t *testing.T) {
	vm := New(nil)
	loc := vm.backtraceLocation("/a.rb:4:in 'block (2 levels) in foo'")
	if got := vm.send(loc, "label", nil, nil).ToS(); got != "block (2 levels) in foo" {
		t.Errorf("label: got %q", got)
	}
	if got := vm.send(loc, "base_label", nil, nil).ToS(); got != "foo" {
		t.Errorf("base_label: got %q", got)
	}
	if got := vm.send(loc, "lineno", nil, nil); got != object.IntValue(4) {
		t.Errorf("lineno: got %v", got)
	}
}
