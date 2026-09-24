package bytecode

import "testing"

// TestLineAtEmpty: an ISeq with no source map places nothing. MRI's
// rb_iseq_line_no (iseq.c v3_4_0:2314) likewise answers 0 when get_insn_info
// finds no entry, which is the state an AOT-frozen ISeq built before the map
// existed comes back in.
func TestLineAtEmpty(t *testing.T) {
	var s ISeq
	for _, pc := range []int{0, 1, 99} {
		if got := s.LineAt(pc); got != 0 {
			t.Errorf("LineAt(%d) = %d, want 0", pc, got)
		}
	}
}

// TestLineAtBeforeFirstEntry: a pc ahead of the first entry has no line. MRI's
// binary search starts at index 1 and can only return an entry it has passed.
func TestLineAtBeforeFirstEntry(t *testing.T) {
	s := ISeq{Lines: []LineEntry{{PC: 3, Line: 7}}}
	if got := s.LineAt(0); got != 0 {
		t.Errorf("LineAt(0) = %d, want 0", got)
	}
	if got := s.LineAt(2); got != 0 {
		t.Errorf("LineAt(2) = %d, want 0", got)
	}
	if got := s.LineAt(3); got != 7 {
		t.Errorf("LineAt(3) = %d, want 7", got)
	}
}

// TestLineAtSingleEntry: one entry covers every pc at or after it.
func TestLineAtSingleEntry(t *testing.T) {
	s := ISeq{Lines: []LineEntry{{PC: 0, Line: 12}}}
	for _, pc := range []int{0, 1, 500} {
		if got := s.LineAt(pc); got != 12 {
			t.Errorf("LineAt(%d) = %d, want 12", pc, got)
		}
	}
}

// TestLineAtRuns walks every pc across a multi-entry table, checking each lands
// in the run that owns it — the property get_insn_info_binary_search has and a
// single spot-check would not prove.
func TestLineAtRuns(t *testing.T) {
	s := ISeq{Lines: []LineEntry{
		{PC: 0, Line: 1},
		{PC: 2, Line: 4},
		{PC: 5, Line: 9},
		{PC: 9, Line: 2}, // lines need not increase: a loop jumps backwards
	}}
	want := []int{1, 1, 4, 4, 4, 9, 9, 9, 9, 2, 2, 2}
	for pc, w := range want {
		if got := s.LineAt(pc); got != w {
			t.Errorf("LineAt(%d) = %d, want %d", pc, got, w)
		}
	}
}
