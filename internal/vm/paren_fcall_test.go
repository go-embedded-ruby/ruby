// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// Parentheses settle whether a receiver-less name is a method call or a local
// variable read. `foobar()` is `fcall paren_args` (parse.y-ruby_4_0:3572-3577,
// 5242-5248), which builds NEW_FCALL and never consults the local table, so a
// local named foobar does NOT shadow a method foobar at a parenthesised call.
//
// Every expectation below was measured on
// `ruby 4.0.7 (2026-09-15 revision 229531a6cf) +PRISM [arm64-darwin25]`
// through scripts/conformance/rubyspec/cmd/oracle.
//
// ⛔ The compiler already read ast.Call.Paren for the vcall flag, citing these
// same productions, while the local-variable shortcut thirty lines above it did
// not. One bit, needed in two places, read in one.
func TestParenthesesMakeACallAnFCallAndNotALocalRead(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// ⛔ The decisive row: language/send_spec.rb "with same names as existing
		// variables is ok". Before the fix the second line printed 100.
		{"a local does not shadow a method at a parenthesised call",
			`def shadowed; 200; end
shadowed = 100
p shadowed
p shadowed()`, "100\n200"},

		{"the parenthesised call reaches the method, not the local's value",
			`def m; :method; end
m = :local
p m()`, ":method"},

		// A local with NO method of that name is not returned: the call misses.
		{"a parenthesised call with only a local raises NoMethodError",
			`only_local = 7
begin
  only_local()
rescue NoMethodError => e
  p e.class
end`, "NoMethodError"},

		// Even when the local holds something callable, `f()` is not `f.call`.
		{"a local holding a lambda is not called by parentheses",
			`callable = -> { :from_proc }
begin
  callable()
rescue NoMethodError => e
  p e.class
end`, "NoMethodError"},

		{"the bare name is still the local",
			`def bare; 200; end
bare = 100
p bare`, "100"},

		{"it holds inside a method body, not only at the top level",
			`class C
  def v; 9; end
  def go
    v = 1
    [v, v(), self.v]
  end
end
p C.new.go`, "[1, 9, 9]"},

		// ⛔ A control for a shape that was never the local anyway: a regression
		// here would be in this change's blast radius rather than in its rule.
		{"a parenthesised call with arguments was always a call",
			`def witharg(x); x * 2; end
witharg = 5
p witharg(21)`, "42"},

		// ⛔ The vcall/fcall distinction this change must NOT disturb: a miss
		// without parentheses is NameError, with parentheses NoMethodError.
		{"a miss without parentheses is still NameError",
			`begin
  nope
rescue NameError => e
  p e.class
end`, "NameError"},

		{"a miss with parentheses is still NoMethodError",
			`begin
  nope()
rescue NoMethodError => e
  p e.class
end`, "NoMethodError"},

		// ⛔ The behaviour the local-variable shortcut exists for, and the reason
		// !v.Paren is a conjunct rather than a removal. The parser emits a Call,
		// not a VarRef, for an identifier whose assignment it never saw — which is
		// how an eval'd string reaches the binding's locals seeded into the scope.
		// This row fails if the shortcut is narrowed too far.
		{"a local seeded into an eval'd binding is still readable",
			`seeded = 42
p eval("seeded")`, "42"},

		{"a local is still readable from a block that closes over it",
			`x = 3
p [1].map { x }`, "[3]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}
