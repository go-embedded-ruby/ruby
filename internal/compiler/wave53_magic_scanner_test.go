// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package compiler

import "testing"

// TestMagicCommentScannerEdges covers the five branches of the magic-comment
// scanner that nothing else reaches: the per-function coverage ratchet named
// magicCommentFields and magicCommentMarker after this file arrived.
//
// They are reachable inputs of a real syntax, not dead code, so they are
// covered rather than deleted — and every expectation below was read off ruby
// 4.0.5 rather than off this implementation. All seven already agreed, which
// is what says these are TEST gaps and not defects.
//
// The five blocks, and the input that reaches each:
//
//	a name running to end of string   `# frozen_string_literal`
//	a colon with nothing after it     `# frozen_string_literal:`
//	an indicator field with no colon  `# -*- frozen_string_literal: true; foo -*-`
//	a '*' at the very end             `# -*`
//	a "*-" not preceded by '-'        `# a*- frozen_string_literal: true -*-`
func TestMagicCommentScannerEdges(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      bool
	}{
		{"name with no colon", "# frozen_string_literal\n", false},
		{"colon with nothing after", "# frozen_string_literal:\n", false},
		{"emacs indicator, field without a colon", "# -*- frozen_string_literal: true; foo -*-\n", true},
		{"emacs indicator, ordinary", "# -*- frozen_string_literal: true -*-\n", true},
		{"truncated marker", "# -*\n", false},
		{"a star-dash not preceded by a dash", "# a*- frozen_string_literal: true -*-\n", false},
		{"emacs indicator with two fields", "# -*- coding: utf-8; frozen_string_literal: true -*-\n", true},
		// A colonless field that is NOT last: the `continue` that skips it only
		// runs when something follows, so the trailing-field case above reaches a
		// different arm.
		{"emacs indicator, colonless field first", "# -*- foo; frozen_string_literal: true -*-\n", true},
		// A line with a star but no marker pair: no pragma, whatever else is on
		// it. The scan's own run-off-the-end arm is exercised directly below,
		// because reaching it through a whole source would mean choosing a line
		// for a property of the scanner rather than for what it means.
		{"a star with no marker pair", "# abcdef*\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MagicComments(tc.src).FrozenStringLiteral; got != tc.want {
				t.Errorf("MagicComments(%q).FrozenStringLiteral = %v, want %v (ruby 4.0.5)", tc.src, got, tc.want)
			}
		})
	}
}

// TestMagicCommentMarkerRunsOffTheEnd is the scanner's own contract, tested
// directly because the arm is not reachable through a whole source in any way
// that would be an honest example: magicCommentFields drops anything of 7 bytes
// or fewer before the scan runs, and a longer line reaches this arm only for
// where its bytes happen to fall, which is a property of the scan and not of
// any Ruby anyone writes.
//
// The contract: a '*' that the walk lands on as the last byte cannot open a
// `-*-`, so there is no marker and the answer is (0, false).
func TestMagicCommentMarkerRunsOffTheEnd(t *testing.T) {
	for _, s := range []string{"# abcdef*", "# -aaaa*", "# --*"} {
		if got, ok := magicCommentMarker(s); ok || got != 0 {
			t.Errorf("magicCommentMarker(%q) = (%d, %v), want (0, false): a trailing "+
				"star cannot open a -*- pair", s, got, ok)
		}
	}
	// And the converse, so the assertion above is not satisfied by a scanner
	// that never finds anything: a real pair is found.
	if got, ok := magicCommentMarker("# -*- coding: utf-8 -*-"); !ok || got == 0 {
		t.Errorf("magicCommentMarker on a real -*- pair = (%d, %v), want a positive offset and true", got, ok)
	}
}
