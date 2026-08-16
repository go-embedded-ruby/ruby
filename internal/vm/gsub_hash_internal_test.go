// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestGsubHash covers String#gsub/#sub with a Hash replacement: each match is
// looked up with Hash#[], so a missing key runs the hash's default value or
// default_proc, and any non-nil value is coerced with #to_s. A nil result (no
// key and no default) contributes nothing. Verified against ruby 4.0.6.
func TestGsubHash(t *testing.T) {
	cases := []struct{ src, want string }{
		// Present key.
		{`p "a0b".gsub(/0/, {"0" => "X"})`, `"aXb"`},
		// Missing key uses the hash's default value.
		{`h = Hash.new("?"); p "00".gsub(/0/, h)`, `"??"`},
		{`h = {"0" => "x"}; h.default = "?"; p "a0b".gsub(/[a-z0-9]/, h)`, `"?x?"`},
		// Missing key uses the default_proc.
		{`h = Hash.new { |hh, k| k * 2 }; p "ab".gsub(/./, h)`, `"aabb"`},
		// Values are coerced with #to_s.
		{`h = Hash.new([]); p "0".gsub(/0/, h)`, `"[]"`},
		{`p "0".gsub(/0/, {"0" => 42})`, `"42"`},
		// Missing key with no default contributes nothing.
		{`p "abc".gsub(/x/, {})`, `"abc"`},
		{`p "a1b2".gsub(/\d/, {"1" => "-"})`, `"a-b"`},
		// An explicit nil value contributes nothing.
		{`p "a".gsub(/a/, {"a" => nil})`, `""`},
		// #sub (non-global) shares the same lookup for its single replacement.
		{`h = Hash.new("?"); p "0x0".sub(/0/, h)`, `"?x0"`},
		{`p "0".sub(/0/, Hash.new([]))`, `"[]"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
