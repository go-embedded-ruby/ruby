// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringDeleteAffix covers String#delete_prefix/#delete_suffix and their
// bang forms: removing a found affix, leaving the string unchanged (a fresh
// copy) when absent or empty, refusing to strip a broken (invalid-encoding)
// affix, #to_str coercion of the argument (with a non-String result and a
// non-coercible argument both raising TypeError), preserving the receiver's
// encoding, the bang forms returning nil on no change and raising FrozenError on
// a frozen receiver. Verified against ruby 4.0.6.
func TestStringDeleteAffix(t *testing.T) {
	cases := []struct{ src, want string }{
		// delete_prefix / delete_suffix
		{`p "hello".delete_prefix("hell")`, `"o"`},
		{`p "hello".delete_prefix("hello")`, `""`},
		{`p "hello".delete_prefix("xyz")`, `"hello"`},
		{`p "hello".delete_prefix("")`, `"hello"`},
		{`p "hello".delete_suffix("llo")`, `"he"`},
		{`p "hello".delete_suffix("hello")`, `""`},
		{`p "hello".delete_suffix("xyz")`, `"hello"`},
		{`p "hello".delete_suffix("")`, `"hello"`},
		// An affix longer than the receiver strips nothing.
		{`p "hi".delete_prefix("hello")`, `"hi"`},
		{`p "hi".delete_suffix("hello")`, `"hi"`},
		// A copy is returned even when nothing is removed (not the same object).
		{`s = "hello"; p s.delete_prefix("xyz").equal?(s)`, `false`},
		// A broken (invalid-UTF-8) affix strips nothing.
		{`p "\xe3\x81\x82".delete_prefix("\xe3") == "\xe3\x81\x82"`, `true`},
		{`p "\xe3\x81\x82".delete_suffix("\x82") == "\xe3\x81\x82"`, `true`},
		// #to_str coercion of the argument.
		{`class Pre; def to_str; "he"; end; end; p "hello".delete_prefix(Pre.new)`, `"llo"`},
		// A non-coercible argument, and a #to_str returning a non-String, raise TypeError.
		{`begin; "hello".delete_prefix(1); rescue TypeError => e; p e.class; end`, `TypeError`},
		{`class BadPre; def to_str; 1; end; end
begin; "hello".delete_prefix(BadPre.new); rescue TypeError => e; p e.class; end`, `TypeError`},
		// The result keeps the receiver's encoding.
		{`p "hello".encode("US-ASCII").delete_prefix("hell").encoding.name`, `"US-ASCII"`},
		// A subclass receiver yields a plain String.
		{`class MyStr < String; end; p MyStr.new("hello").delete_prefix("hell").class`, `String`},
		// Bang forms: mutate and return self, or nil on no change.
		{`s = "hello"; r = s.delete_prefix!("he"); p [r.equal?(s), s]`, `[true, "llo"]`},
		{`p "hello".delete_prefix!("xx")`, `nil`},
		{`p "hello".delete_prefix!("")`, `nil`},
		{`s = "hello"; r = s.delete_suffix!("lo"); p [r.equal?(s), s]`, `[true, "hel"]`},
		{`p "hello".delete_suffix!("xx")`, `nil`},
		// Frozen receiver raises FrozenError (even for a no-op empty affix).
		{`begin; "hello".freeze.delete_prefix!("hell"); rescue FrozenError => e; p e.class; end`, `FrozenError`},
		{`begin; "hello".freeze.delete_suffix!(""); rescue FrozenError => e; p e.class; end`, `FrozenError`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
