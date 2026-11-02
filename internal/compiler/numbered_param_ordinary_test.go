// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package compiler

import (
	"strings"
	"testing"
)

// A block cannot have both a WRITTEN parameter list and a numbered parameter:
// MRI's lexer reserves `_1`..`_9` for the numbered-parameter mechanism, so
// `proc { |x| _1 }` is rejected at parse time with "ordinary parameter is
// defined". Writing `||` at all is what declares the list, which is why the
// empty forms are rejected too.
//
// ⛔ Unlike its neighbour TestNumberedParameterNameIsReserved, NOT ONE row here
// was run against a reference interpreter: the Ruby on this machine is 2.6.10
// and numbered parameters arrived in 2.7, so the local oracle cannot answer any
// of these. The authority is language/numbered_parameters_spec.rb:49, which pins
// all eight shapes below verbatim, and MRI's own rule that the nine names are
// reserved by the grammar. Said plainly so nobody reads a measurement here that
// was never taken.
//
// Before this check the names resolved to no local and were compiled as
// receiverless sends, so the shapes that were CALLED failed at runtime with
// NameError and the ones that were only defined — `proc { |x| _1 }` on its own —
// raised nothing at all.
func TestNumberedParameterWithOrdinaryParameters(t *testing.T) {
	const want = "ordinary parameter is defined"
	refused := []string{
		// The eight shapes the spec lists, in its order: a written list that is
		// empty and one that is not, across ->, proc, lambda and a method block.
		"-> () { _1 }",
		"-> (x) { _1 }",
		"proc { || _1 }",
		"proc { |x| _1 }",
		"lambda { || _1 }",
		"lambda { |x| _1 }",
		"['a'].map { || _1 }",
		"['a'].map { |x| _1 }",
		// The same rule reaches the other nine names and the other kinds of
		// written parameter — a block parameter and a keyword are still a written
		// list, even though neither is positional.
		"proc { |x| _9 }",
		"proc { |&blk| _1 }",
		"proc { |k: 1| _1 }",
	}
	for _, src := range refused {
		_, err := compileString(src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want one containing %q", src, err, want)
		}
	}

	// The controls. Each one would be broken by a check that was too eager, and
	// the first two are the ones that matter most: the numbered parameter's whole
	// reason for existing is the block that writes NO list.
	allowed := []string{
		"proc { _1 }",              // synthesised by the parser — the normal case
		"proc { _1 + _2 }",         // and more than one of them
		"proc { |x| x }",           // an ordinary written parameter, no _N in sight
		"proc { |x| _0 }",          // _0 is an ordinary identifier
		"proc { |x| _10 }",         // so is _10
		"proc { |x| _1a }",         // and so is anything longer
		"proc { |x| @_1 }",         // an ivar is not a numbered parameter
		"proc { |x| self._1 }",     // an explicit receiver is an ordinary call
		"proc { |x| _1(2) }",       // so is a call with arguments
		"proc { |x| proc { _1 } }", // the INNER block wrote no list; _1 is its own
		"def m; _1; end",           // not a block at all: left as a runtime NameError
		"_1",                       // nor at the top level
		"for x in [1]; _1; end",    // `for` opens no Ruby scope, so the rule misses it
	}
	for _, src := range allowed {
		if _, err := compileString(src); err != nil && strings.Contains(err.Error(), want) {
			t.Errorf("%q: refused with %q, must be allowed", src, err)
		}
	}
}

// The control for the control: the two refusals must stay DISTINGUISHABLE.
// Binding `_1` is "reserved for numbered parameters" (storeLocal) and reading it
// beside an ordinary parameter is "ordinary parameter is defined" (compileCall).
// They are different sentences in MRI and the specs match on them separately —
// `/_1 is reserved for numbered parameter/` and `/ordinary parameter is defined/`
// — so a fix that collapsed them into one message would satisfy neither spec.
func TestNumberedParameterRefusalsStayDistinct(t *testing.T) {
	for _, tc := range []struct{ src, want, notWant string }{
		{"proc { _1 = 0 }", "reserved for numbered parameters", "ordinary parameter is defined"},
		{"proc { |x| _1 }", "ordinary parameter is defined", "reserved for numbered parameters"},
	} {
		_, err := compileString(tc.src)
		if err == nil {
			t.Errorf("%q: compiled, want a refusal", tc.src)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want one containing %q", tc.src, err, tc.want)
		}
		if strings.Contains(err.Error(), tc.notWant) {
			t.Errorf("%q: err = %v, must NOT contain %q", tc.src, err, tc.notWant)
		}
	}
}
