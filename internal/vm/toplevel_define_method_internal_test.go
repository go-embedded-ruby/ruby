// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestTopLevelDefineMethod covers the top-level define_method: at the top level
// it operates on Object (the default definee), so define_method(:m){…} defines
// Object#m and returns the method name. Verified against ruby 4.0.6.
func TestTopLevelDefineMethod(t *testing.T) {
	cases := []struct{ src, want string }{
		{`define_method(:dm_a) { 42 }; p dm_a`, `42`},
		{`define_method(:dm_b) { |x| x * 2 }; p dm_b(3)`, `6`},
		{`p(define_method(:dm_c) { 1 })`, `:dm_c`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
