// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestEndBlockRunsInsteadOfCrashing (#805).
//
// `END { }` used to end the process with an uncontained Go panic --
// `slice bounds out of range [:1] with capacity 0`, exit 2 -- when the handler
// ran at exit. For a program that is a crash instead of an answer; for an
// embedder it is worse, because ruby.Run does not return an error, it takes the
// host process down.
//
// The cause was in the parser and is fixed by v0.13.1: `END { }` desugars to
// `at_exit { }`, and the Block it built left SplatIndex at Go's zero value --
// which is a valid index, where the field's "no splat" value is -1. So the
// block claimed a *splat parameter at position 0 with no parameters to hold it,
// and this compiler's rest-binding read past an empty environment.
//
// The test lives HERE as well as upstream because the defect was invisible in
// the AST: the parser's own suite was green throughout, and the whole cost
// landed on the consumer that lowers a Block. A downgrade of the dependency
// brings the crash back, and this is what notices.
func TestEndBlockRunsInsteadOfCrashing(t *testing.T) {
	checkCases(t, []runCase{
		{`END { puts "E" }`, "E\n"},
		// MRI runs END handlers after the program body, in reverse order of
		// registration -- the ordering is the part a "did it crash" test misses.
		{`puts "main"
END { puts "E" }`, "main\nE\n"},
		{`END { puts "first registered" }
END { puts "second registered" }`, "second registered\nfirst registered\n"},
		// BEGIN runs before the body and was never broken; it is here so a
		// regression in either one is attributable to the right half.
		{`BEGIN { puts "B" }
puts "main"
END { puts "E" }`, "B\nmain\nE\n"},
		// at_exit is the shape END desugars to, and it also never broke: if this
		// one ever fails with the others, the fault is in at_exit, not in END.
		{`at_exit { puts "A" }
puts "main"`, "main\nA\n"},
	})
}
