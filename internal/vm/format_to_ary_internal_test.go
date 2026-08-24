// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestFormatToAry covers how String#% turns its right-hand side into the
// positional argument list: an Array is spread, a Hash is kept whole for
// %{name}/%<name> references, a single scalar is one argument, and any other
// object is converted with #to_ary (an Array spreads, a nil result falls back to
// a single argument, a non-Array result raises TypeError). Verified against
// ruby 4.0.6.
func TestFormatToAry(t *testing.T) {
	cases := []struct{ src, want string }{
		// Array spreads; a plain scalar is a single argument.
		{`p "%d %d" % [1, 2]`, `"1 2"`},
		{`p "%d" % 5`, `"5"`},
		// A Hash stays whole (named references / %s of the hash).
		{`p "%<x>d" % {x: 7}`, `"7"`},
		{`p "%s" % {a: 1}`, `"{a: 1}"`},
		// #to_ary returning an Array is spread.
		{`class FA; def to_ary; [1, 2]; end; end; p "%d %d" % FA.new`, `"1 2"`},
		// #to_ary returning nil falls back to a single argument.
		{`class FB; def to_ary; nil; end; def to_s; "b"; end; end; p "%s" % FB.new`, `"b"`},
		// #to_ary returning a non-Array raises TypeError.
		{`class FC; def to_ary; 5; end; end
begin; "%s" % FC.new; rescue TypeError => e; p e.message; end`, `"can't convert FC to Array (FC#to_ary gives Integer)"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
