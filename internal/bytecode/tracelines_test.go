package bytecode

import "testing"

// TestIsLineStart covers the exact-hit search: a pc that begins a line run says
// yes, a pc inside one says no, and an ISeq with no map — or a pc before its
// first entry — says no rather than reaching for Lines[0].
func TestIsLineStart(t *testing.T) {
	is := &ISeq{Lines: []LineEntry{{PC: 2, Line: 10}, {PC: 5, Line: 11}, {PC: 9, Line: 12}}}
	for pc, want := range map[int]bool{0: false, 1: false, 2: true, 3: false, 4: false, 5: true, 8: false, 9: true, 40: false} {
		if got := is.IsLineStart(pc); got != want {
			t.Errorf("IsLineStart(%d) = %v, want %v", pc, got, want)
		}
	}
	// An ISeq compiled with no position information at all (a prelude frozen
	// before the line map existed) begins no line anywhere.
	empty := &ISeq{}
	if empty.IsLineStart(0) {
		t.Error("IsLineStart on an ISeq with no line map must be false")
	}
}

// TestHasLine covers the target_line: membership question, whose false answer
// is MRI's "can not enable any hooks".
func TestHasLine(t *testing.T) {
	is := &ISeq{Lines: []LineEntry{{PC: 0, Line: 10}, {PC: 4, Line: 12}}}
	for line, want := range map[int]bool{10: true, 11: false, 12: true, 0: false, -2: false} {
		if got := is.HasLine(line); got != want {
			t.Errorf("HasLine(%d) = %v, want %v", line, got, want)
		}
	}
}
