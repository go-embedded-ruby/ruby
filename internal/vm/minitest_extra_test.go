// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestMinitestExtra covers assertion/mock paths not exercised by the main
// minitest suite: refute_same, assert_raises with a trailing custom-message
// String, and the mock expect form that rejects positional args given alongside
// a validation block (the gem's "args ignored when block given").
func TestMinitestExtra(t *testing.T) {
	harness := `require "minitest"
class T; include Minitest::Assertions; end
$t = T.new
`
	cases := []struct{ src, want string }{
		// refute_same passes for two distinct objects and returns true.
		{harness + `p $t.refute_same("a", "b")`, "true\n"},
		// assert_raises with a trailing String custom message still returns the
		// caught exception (the String is consumed as the message, not a class).
		{harness + `e = $t.assert_raises(ArgumentError, "custom") { raise ArgumentError, "boom" }
puts e.message`, "boom\n"},
		// The block-validated expect rejects positional args given with the block.
		{`require "minitest"
m = Minitest::Mock.new
begin
  m.expect(:foo, 1, [2]) { |x| true }
rescue ArgumentError => e
  puts e.message
end`, "args ignored when block given\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
