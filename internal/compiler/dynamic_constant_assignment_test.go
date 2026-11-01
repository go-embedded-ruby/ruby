// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package compiler

import (
	"strings"
	"testing"
)

// Assigning a constant inside a method body is MRI's "dynamic constant
// assignment", a SyntaxError raised before the program runs. rbgo refused none
// of it: `def d; FOO = 1; end` compiled and ran.
//
// Every row below was run through /usr/bin/ruby on the same source. The rule is
// ancient — it predates the 2.6 on this machine by many versions — so unlike the
// numbered-parameter check next door, that interpreter IS a valid oracle here,
// and all eighteen shapes agree with it.
//
// The boundary is PURELY SYNTACTIC: an enclosing `def` keyword, reached through
// any number of blocks. Two measured rows make that concrete and they point in
// opposite directions:
//
//	def d; Class.new { FOO = 1 }; end          refused
//	class C; define_method(:m) { FOO = 1 }; end  allowed
//
// Both blocks end up running as a method body, so reasoning from "will this run
// as a method?" gets both of them backwards. Only the lexical question answers.
func TestDynamicConstantAssignment(t *testing.T) {
	const want = "dynamic constant assignment"

	refused := []string{
		// The plain shape, and the three scopes that are still the method.
		"def d; AA = 1; end",
		"def d; [1].each { AA = 1 }; end",
		"def d; [1].each { [2].each { AA = 1 } }; end",
		"def d; -> { AA = 1 }; end",
		// ⛔ A block is not an escape even when it is building a class: the
		// `Class.new` receiver is an expression, so nothing here opens a
		// class BODY, and the walk still reaches the `def`.
		"def d; Class.new { AA = 1 }; end",
		"def d; Object.new.instance_eval { AA = 1 }; end",
		// ⛔ A scoped assignment is refused inside a `def` too. Naming the
		// namespace explicitly looks like it should settle where the constant
		// goes, and it does — upstream refuses it anyway.
		"class C; def m; C::AA = 1; end; end",
		"def d; ::AA = 1; end",
		// Every way of writing the assignment, not just the plain one.
		"def d; AA ||= 1; end",
		"def d; AA += 1; end",
		"def d; AA, x = 1, 2; end",
		"def d; x, AA = 1, 2; end",
		"def d; AA, (x, BB) = 1, [2, 3]; end",
		// Singleton and nested definitions are method bodies like any other.
		"class S; def self.m; AA = 1; end; end",
		"class S; class << self; def m; AA = 1; end; end; end",
		"o = Object.new; def o.m; AA = 1; end",
		"def d; def inner; AA = 1; end; end",
		// Including the parts of a method that are not its statement list.
		"def d; raise; rescue; AA = 1; end",
		"def d; ensure; AA = 1; end",
		"def d(x = (AA = 1)); end",
	}
	for _, src := range refused {
		_, err := compileString(src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want one containing %q", src, err, want)
		}
	}

	// The controls. Every one of these is a place a constant assignment is
	// ORDINARY Ruby, and a check that reached any of them would break real code.
	allowed := []string{
		"AA = 1",                                     // the top level
		"class C; AA = 1; end",                       // a class body
		"module M; AA = 1; end",                      // a module body
		"class C; class D; AA = 1; end; end",         // and a nested one
		"[1].each { AA = 1 }",                        // a block at the top level
		"class C; [1].each { AA = 1 }; end",          // a block in a class body
		"class C; class << self; AA = 1; end; end",   // a singleton class body
		"class C; define_method(:m) { AA = 1 }; end", // ⛔ the define_method row
		"class C; end; C.module_eval { AA = 1 }",     // a module_eval block
		"def d; x = 1; end",                          // a local in a def is fine
		"def d; @ivar = 1; end",                      // so is an ivar
		"def d; @@cvar = 1; end",                     // and a class variable
		"def d; $gvar = 1; end",                      // and a global
		"def d; AA; end",                             // and READING a constant
		"def d; C::AA; end",                          // or reading a scoped one
	}
	for _, src := range allowed {
		if _, err := compileString(src); err != nil && strings.Contains(err.Error(), want) {
			t.Errorf("%q: refused with %q, must be allowed", src, err)
		}
	}
}

// The control for the control. Every row of the `allowed` list above passes if
// the refusal is deleted, so the suite needs one planted positive it cannot pass
// without the check — and one assertion that the check is looking at the SCOPE
// and not at the name, which a lookup table of constant names would also
// satisfy.
func TestDynamicConstantAssignmentIsObservable(t *testing.T) {
	if _, err := compileString("def d; AA = 1; end"); err == nil {
		t.Fatal("the planted positive compiled: the silence of the allowed rows proves nothing")
	}
	// Same name, same assignment, two scopes: one refused, one not. This is what
	// says the predicate reads the scope chain.
	if _, err := compileString("class C; SAME = 1; end"); err != nil {
		t.Errorf("SAME = 1 in a class body: %v, want no error", err)
	}
	if _, err := compileString("class C; def m; SAME = 1; end; end"); err == nil {
		t.Error("SAME = 1 in a method body compiled, want a refusal")
	}
}

// inDefBody is the predicate both this refusal and (later) the
// class-definition-in-method-body one need, so it is worth pinning directly
// rather than only through the sources above: the walk must pass through blocks
// and stop at a class or module body.
func TestInDefBodyWalksThroughBlocksAndStopsAtAClassBody(t *testing.T) {
	// Expressed through sources whose outcome depends only on the walk, since
	// inDefBody has no state a test can set up by hand.
	for _, tc := range []struct {
		src     string
		refused bool
	}{
		{"def d; AA = 1; end", true},                                        // one hop, no blocks
		{"def d; [1].each { AA = 1 }; end", true},                           // through one block
		{"def d; [1].each { [2].each { [3].each { AA = 1 } } }; end", true}, // three
		{"def d; for x in [1]; AA = 1; end; end", true},                     // a `for` is a block scope
		{"class C; [1].each { AA = 1 }; end", false},                        // stops at the class body
		{"module M; [1].each { AA = 1 }; end", false},                       // and at a module body
		{"[1].each { AA = 1 }", false},                                      // and at the top level
		{"class C; def m; end; AA = 1; end", false},                         // a sibling def does not count
	} {
		_, err := compileString(tc.src)
		got := err != nil && strings.Contains(err.Error(), "dynamic constant assignment")
		if got != tc.refused {
			t.Errorf("%q: refused = %v (err %v), want %v", tc.src, got, err, tc.refused)
		}
	}
}
