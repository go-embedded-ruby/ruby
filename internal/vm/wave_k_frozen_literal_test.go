package vm

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestFrozenStringLiteralPragma covers `# frozen_string_literal: true` end to
// end: the literal comes out FROZEN and INTERNED, so `s.equal?("abc")` holds,
// which is the pair MRI 4.0.5 answers `[true, true]` and rbgo answered
// `[false, false]` before this. Every want was measured against ruby 4.0.5.
//
// The strings below are deliberately odd: object.InternFString's table is
// process-wide (as MRI's is), so a literal shared with another test in this
// package would make one test's result depend on the other's having run.
func TestFrozenStringLiteralPragma(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"frozen and interned",
			"# frozen_string_literal: true\ns = \"wkfsl_a\"\np [s.frozen?, s.equal?(\"wkfsl_a\")]", "[true, true]"},
		{"false is the default behaviour",
			"# frozen_string_literal: false\ns = \"wkfsl_b\"\np [s.frozen?, s.equal?(\"wkfsl_b\")]", "[false, false]"},
		{"absent is the default behaviour",
			"s = \"wkfsl_c\"\np [s.frozen?, s.equal?(\"wkfsl_c\")]", "[false, false]"},

		// An INTERPOLATED literal is never frozen in any Ruby: it is built at
		// run time by concatenating onto a fresh mutable "", so it never reaches
		// newStrLit. The plain literal beside it shows the pragma is in force.
		{"interpolation is not frozen",
			"# frozen_string_literal: true\np [\"wk#{1}fsl\".frozen?, \"wkfsl_d\".frozen?]", "[false, true]"},
		{"empty interpolation is not frozen",
			"# frozen_string_literal: true\np \"#{}\".frozen?", "false"},

		// The pragma is a property of the FILE, so it reaches every literal
		// compiled from it, however deeply nested.
		{"reaches a method body",
			"# frozen_string_literal: true\ndef m; \"wkfsl_e\"; end\np m.frozen?", "true"},
		{"reaches a block",
			"# frozen_string_literal: true\np [1].map { \"wkfsl_f\" }.first.frozen?", "true"},

		// A frozen literal cannot be mutated -- which is the point of the pragma,
		// and the proof that vm's OpPushConst is sharing rather than cloning it.
		{"mutating one raises FrozenError",
			"# frozen_string_literal: true\nbegin\n  \"wkfsl_g\" << \"h\"\nrescue => e\n  p e.class\nend", "FrozenError"},

		// The empty literal is interned like any other.
		{"the empty literal", "# frozen_string_literal: true\np [\"\".frozen?, \"\".equal?(\"\")]", "[true, true]"},

		// String#-@ consults the SAME table, so a literal and its interned form
		// are one object. Two tables would have passed every case above and
		// failed only here.
		{"shares the table with String#-@",
			"# frozen_string_literal: true\np \"wkfsl_i\".equal?(-\"wkfsl_i\")", "true"},
		{"shares the table with String#freeze",
			"# frozen_string_literal: true\np \"wkfsl_j\".equal?(\"wkfsl_j\".freeze)", "true"},

		// A literal in a file with a non-default source encoding is interned
		// under that encoding, so it is a DIFFERENT object from the same bytes in
		// a UTF-8 file -- ruby/spec "produce different objects for literals with
		// the same content in different files if they have different encodings".
		{"keeps the source encoding",
			"# encoding: binary\n# frozen_string_literal: true\np [\"wkfsl_k\".frozen?, \"wkfsl_k\".encoding.to_s]", `[true, "ASCII-8BIT"]`},

		// Where the pragma counts. These duplicate the unit tests on the scanner
		// deliberately: they prove the scan is actually wired into the path a
		// file takes, not merely correct in isolation.
		{"honoured after a shebang",
			"#!/usr/bin/env ruby\n# frozen_string_literal: true\np \"wkfsl_l\".frozen?", "true"},
		{"honoured after other comments",
			"# encoding: utf-8\n# unrelated\n# frozen_string_literal: true\np \"wkfsl_m\".frozen?", "true"},
		{"ignored after a token",
			"x = 1\n# frozen_string_literal: true\np \"wkfsl_n\".frozen?", "false"},
	}
	for _, c := range cases {
		if got := runSrcEnc(t, c.src); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// TestFrozenStringLiteralIsPerFile covers the property the pragma's whole
// plumbing exists for: it is declared PER FILE, not per program. A required
// file keeps its own setting in both directions, and two files that both
// declare it share their literals -- ruby/spec language/string_spec.rb "produce
// the same object for literals with the same content in different files",
// which no per-compilation cache could satisfy.
//
// This is also the test that would fail if the pragma were read once at the
// entry point and handed down.
func TestFrozenStringLiteralIsPerFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return strconv.Quote(p)
	}
	frozen := write("wk_frozen.rb", "# frozen_string_literal: true\n$WK_FROZEN = \"wkfsl_shared\"\n")
	plain := write("wk_plain.rb", "$WK_PLAIN = \"wkfsl_shared\"\n")

	cases := []struct{ name, src, want string }{
		// A required file WITHOUT the pragma keeps mutable literals even though
		// the requiring file has it, and its "wkfsl_shared" is a different
		// object from the caller's interned one.
		{"required file without the pragma",
			"# frozen_string_literal: true\nrequire " + plain + "\np [$WK_PLAIN.frozen?, \"wkfsl_shared\".frozen?, $WK_PLAIN.equal?(\"wkfsl_shared\")]",
			"[false, true, false]"},
		// And the mirror: the pragma in a required file does not leak outward.
		{"requiring file without the pragma",
			"require " + frozen + "\np [$WK_FROZEN.frozen?, \"wkfsl_shared\".frozen?, $WK_FROZEN.equal?(\"wkfsl_shared\")]",
			"[true, false, false]"},
		// Two files that BOTH declare it share one object.
		{"two files that both declare it",
			"# frozen_string_literal: true\nrequire " + frozen + "\np \"wkfsl_shared\".equal?($WK_FROZEN)",
			"true"},
	}
	for _, c := range cases {
		if got := runSrcEnc(t, c.src); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// TestFrozenStringLiteralInEval covers eval, which reaches the compiler through
// a different seam (CompileEvalWithLocals) that carried no magic comments at
// all before this -- not even the encoding one.
//
// An eval does NOT inherit its caller's setting: MRI parses the eval string as
// its own unit, so only a pragma INSIDE the string counts. Measured against
// ruby 4.0.5.
func TestFrozenStringLiteralInEval(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"eval does not inherit the caller's pragma",
			"# frozen_string_literal: true\np eval('\"wkfsl_eval_a\"').frozen?", "false"},
		{"eval honours its own pragma",
			"p eval(%Q{# frozen_string_literal: true\\n\"wkfsl_eval_b\"}).frozen?", "true"},
		{"eval honours it against a binding",
			"b = binding\np eval(%Q{# frozen_string_literal: true\\n\"wkfsl_eval_c\"}, b).frozen?", "true"},
		{"eval ignores it after a token",
			"p eval(%Q{x = 1\\n# frozen_string_literal: true\\n\"wkfsl_eval_d\"}).frozen?", "false"},
		// The same seam now carries the encoding pragma, which it silently
		// dropped before: an eval'd `# encoding:` comment had no effect at all.
		{"eval honours its own encoding pragma",
			"p eval(%Q{# encoding: binary\\n\"wkfsl_eval_e\"}).encoding.to_s", `"ASCII-8BIT"`},
	}
	for _, c := range cases {
		if got := runSrcEnc(t, c.src); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}
