// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestGsubLastMatch covers $~ (the last-match global) around String#gsub/#sub and
// their Hash form: after the call $~ is the last match's MatchData, or nil when
// there was none (even if a block reassigned $~); and inside a block $~ carries
// the whole receiver as #string with absolute #offset/#begin (via byteOff).
// Verified against ruby 4.0.6.
func TestGsubLastMatch(t *testing.T) {
	cases := []struct{ src, want string }{
		// After the call: last match, or nil when there is none.
		{`'hello.'.gsub('hello', 'x'); p $~[0]`, `"hello"`},
		{`'hello.'.gsub('not', 'x'); p $~`, "nil"},
		{`'hello.'.gsub(/.(.)/, 'x'); p $~[0]`, `"o."`},
		{`'hello.'.gsub(/not/, 'x'); p $~`, "nil"},
		// Same for the block and Hash forms.
		{`'hello.'.gsub('l') { 'x' }; p [$~.begin(0), $~[0]]`, `[3, "l"]`},
		{`'hello.'.gsub('not') { 'x' }; p $~`, "nil"},
		{`'hello.'.gsub('l', 'l' => 'L'); p [$~.begin(0), $~[0]]`, `[3, "l"]`},
		{`'hello.'.gsub('not', 'ot' => 'to'); p $~`, "nil"},
		// Inside the block $~ carries the whole receiver and absolute offsets.
		{`off = []; "hello".gsub(/([aeiou])/) { off << $~.offset(0); "x" }; p off`, `[[1, 2], [4, 5]]`},
		{`p "hello".gsub(/([aeiou])/) { $~.string == "hello" ? "y" : "n" }`, `"hylly"`},
		{`p "hello".gsub(/([aeiou])/) { "<#{$1}>" }`, `"h<e>ll<o>"`},
		{`p "hello".gsub("l") { "<#{$~[0]}>" }`, `"he<l><l>o"`},
		// A block that reassigns $~ still leaves the gsub's last match afterwards.
		{`"hello".gsub(/./) { "ok".match(/./); "x" }; p [$~[0], $~.string]`, `["o", "hello"]`},
		// #sub (single) sets $~ to its one match, or nil when none.
		{`"hello".sub("l", "L"); p [$~.begin(0), $~[0]]`, `[2, "l"]`},
		{`"hello".sub("z", "Z"); p $~`, "nil"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
