// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringTransformEncoding2 covers a second batch of transforms (chomp, chop,
// tr, tr_s, squeeze, delete) keeping the receiver's encoding on their result,
// rather than defaulting to UTF-8. The UTF-8 default and ASCII-8BIT are
// preserved, and the transforms' values are unchanged. Verified against
// ruby 4.0.6.
func TestStringTransformEncoding2(t *testing.T) {
	cases := []struct{ src, want string }{
		// US-ASCII receiver keeps US-ASCII.
		{`p "hi\n".force_encoding("US-ASCII").chomp.encoding`, `#<Encoding:US-ASCII>`},
		{`p "hi".force_encoding("US-ASCII").chop.encoding`, `#<Encoding:US-ASCII>`},
		{`p "Hello".force_encoding("US-ASCII").tr("a-y", "b-z").encoding`, `#<Encoding:US-ASCII>`},
		{`p "Hello".force_encoding("US-ASCII").tr_s("l", "r").encoding`, `#<Encoding:US-ASCII>`},
		{`p "aabbcc".force_encoding("US-ASCII").squeeze.encoding`, `#<Encoding:US-ASCII>`},
		{`p "Hello".force_encoding("US-ASCII").delete("l").encoding`, `#<Encoding:US-ASCII>`},
		// ASCII-8BIT (binary) is preserved.
		{`p "AB".b.tr("A", "a").encoding`, `#<Encoding:BINARY (ASCII-8BIT)>`},
		{`p "aabb".b.squeeze.encoding`, `#<Encoding:BINARY (ASCII-8BIT)>`},
		// The UTF-8 default is unchanged.
		{`p "héllo\n".chomp.encoding`, `#<Encoding:UTF-8>`},
		{`p "héllo".delete("l").encoding`, `#<Encoding:UTF-8>`},
		// Values are unchanged by the encoding threading.
		{`p "hi\n".chomp`, `"hi"`},
		{`p "hello".chop`, `"hell"`},
		{`p "hello".tr("l", "L")`, `"heLLo"`},
		{`p "mississippi".tr_s("sp", "*")`, `"mi*i*i*i"`},
		{`p "aaabbbccc".squeeze`, `"abc"`},
		{`p "hello".delete("l")`, `"heo"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
