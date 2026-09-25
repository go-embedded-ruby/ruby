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
	vm.frameCode = []frameCode{
		{},
		{iseq: &bytecode.ISeq{Lines: []bytecode.LineEntry{{PC: 0, Line: 31}}}},
		{iseq: &bytecode.ISeq{FirstLine: 8}}, // no map at all: falls back
		{iseq: &bytecode.ISeq{FirstLine: 8, Lines: []bytecode.LineEntry{{PC: 4, Line: 40}}}},
	}

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
	vm.frameCode[3].pc = 4
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
	if len(vm.frameCode) != 4 {
		t.Fatalf("grew to %d, want 4", len(vm.frameCode))
	}
	if vm.frameCode[3].iseq != a {
		t.Error("slot 3 does not hold the pushed ISeq")
	}
	vm.frameCode[3].pc = 17

	// A shallower push must SHRINK it, discarding the abandoned entry.
	vm.setFrameCode(1, b)
	if len(vm.frameCode) != 2 {
		t.Fatalf("shrank to %d, want 2", len(vm.frameCode))
	}
	if vm.frameCode[1].iseq != b {
		t.Error("slot 1 does not hold the pushed ISeq")
	}
	if vm.frameCode[1].pc != 0 {
		t.Errorf("pc not reset on push: %d", vm.frameCode[1].pc)
	}
}

// TestFrameLabelFromISeq covers the non-method frame labels, which all rendered
// "<main>" before the ISeq could be consulted.
func TestFrameLabelFromISeq(t *testing.T) {
	vm := New(nil)
	vm.frameNames = []string{"foo", "", "", "", "", ""}
	vm.frameCode = []frameCode{
		{iseq: &bytecode.ISeq{Name: "foo"}},
		{iseq: &bytecode.ISeq{Name: "<class:K>"}},
		{iseq: &bytecode.ISeq{Name: "<singleton class>"}},
		{iseq: &bytecode.ISeq{Name: "block in foo"}},
		{iseq: &bytecode.ISeq{Name: ""}},
		{},
	}
	want := []string{"foo", "<class:K>", "singleton class", "block in foo", "<main>", "<main>"}
	for i, w := range want {
		if got := vm.frameLabel(i); got != w {
			t.Errorf("frameLabel(%d) = %q, want %q", i, got, w)
		}
	}
	// A frame whose index outruns the ISeq stack falls back too.
	vm.frameCode = nil
	if got := vm.frameLabel(1); got != "<main>" {
		t.Errorf("no ISeq stack: got %q", got)
	}
}

// TestSplitBlockQualifier covers the three shapes the block decoration takes.
// MRI keeps label and base_label in separate fields (vm_backtrace.c
// v3_4_0:331); this decoration is one half of the difference between them.
func TestSplitBlockQualifier(t *testing.T) {
	cases := map[string][2]string{
		"block in foo":             {"block in ", "foo"},
		"block (2 levels) in foo":  {"block (2 levels) in ", "foo"},
		"block (17 levels) in C#m": {"block (17 levels) in ", "C#m"},
		"foo":                      {"", "foo"},
		"<class:K>":                {"", "<class:K>"},
		// "block (" without the levels marker is not a qualifier we made.
		"block (weird": {"", "block (weird"},
	}
	for in, want := range cases {
		qual, base := splitBlockQualifier(in)
		if qual != want[0] || base != want[1] {
			t.Errorf("splitBlockQualifier(%q) = %q,%q, want %q,%q", in, qual, base, want[0], want[1])
		}
	}
}

// TestStripOwnerPrefix covers the OTHER half of the difference: the "Owner#" /
// "Owner." that rb_gen_method_name puts in front of a method label and that
// base_label does not carry. The prefix comes off only when what precedes the
// separator is a constant path, the one shape rb_mod_name0 can produce.
func TestStripOwnerPrefix(t *testing.T) {
	cases := map[string]string{
		"C#m":                    "m",
		"C.m":                    "m",
		"Outer::Inner#deep":      "deep",
		"ThreadBacktraceSpecs.x": "x",
		"C#<=>":                  "<=>",
		// No owner: unchanged.
		"foo":       "foo",
		"<main>":    "<main>",
		"<class:K>": "<class:K>",
		// A separator whose left side is not a constant path is not an owner —
		// which is how `main.label_sdef_method_of_main` keeps its whole label.
		"main.label_sdef_method": "main.label_sdef_method",
		"a::B#m":                 "a::B#m",
		// Degenerate placements: nothing before the separator, nothing after.
		"#m": "#m",
		"C#": "C#",
	}
	for in, want := range cases {
		if got := stripOwnerPrefix(in); got != want {
			t.Errorf("stripOwnerPrefix(%q) = %q, want %q", in, got, want)
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
