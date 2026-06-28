// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestTimeout covers Timeout.timeout running the block to completion (no
// deadline enforced yet) and returning its value, the block receiving nil as
// its limit argument, and Timeout::Error resolving for `rescue`.
func TestTimeout(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "timeout"; p(Timeout.timeout(5) { 123 })`, "123"},
		// the block receives the limit slot (nil, since no deadline is enforced).
		{`require "timeout"; Timeout.timeout(5) { |s| p s }`, "nil"},
		// Timeout::Error < RuntimeError, so a targeted rescue catches it.
		{`require "timeout"
begin
  raise Timeout::Error, "boom"
rescue Timeout::Error => e
  p e.message
end`, `"boom"`},
		// a bare rescue also catches it (StandardError descendant).
		{`require "timeout"
begin
  raise Timeout::Error, "x"
rescue => e
  p e.class.name
end`, `"Timeout::Error"`},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTimeoutNoBlock covers the no-block branch, which raises LocalJumpError.
func TestTimeoutNoBlock(t *testing.T) {
	vm := New(nil)
	mod := vm.consts["Timeout"].(*RClass)
	got := catchRaise(func() { mod.smethods["timeout"].native(vm, mod, nil, nil) })
	if got != "LocalJumpError" {
		t.Fatalf("timeout no-block: got %q, want LocalJumpError", got)
	}
}
