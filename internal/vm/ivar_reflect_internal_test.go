// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestValidIvarName covers validIvarName directly, including the defensive
// invalid-UTF-8 branch that a Ruby Symbol literal (always valid UTF-8) cannot
// reach, and the trailing-character rejection path.
func TestValidIvarName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"@a", true},
		{"@_x", true},
		{"@a9", true},
		{"@💙", true},   // non-ASCII first character
		{"@a💙9", true}, // non-ASCII trailing character
		{"", false},
		{"a", false},     // missing '@'
		{"@", false},     // just '@'
		{"@0", false},    // digit first
		{"@@x", false},   // '@' first (class var)
		{"@a!", false},   // invalid trailing character
		{"@\xff", false}, // invalid UTF-8 first byte (RuneError, size 1)
	}
	for _, c := range cases {
		if got := validIvarName(c.name); got != c.want {
			t.Errorf("validIvarName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
