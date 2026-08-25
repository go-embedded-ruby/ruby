// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringAppendAsBytes covers String#append_as_bytes: it appends each String
// argument's raw bytes and each Integer argument's least-significant byte
// (wrapping negatives and Bignums) to the receiver in place, never changing its
// encoding, accepting only String and Integer (no #to_str/#to_int coercion), and
// raising FrozenError on a frozen receiver. Verified against ruby 4.0.6.
func TestStringAppendAsBytes(t *testing.T) {
	cases := []struct{ src, want string }{
		// Strings append raw bytes; the return value is the receiver.
		{`s = +"hello"; s.append_as_bytes("!"); p s`, `"hello!"`},
		{`s = +"a"; p s.append_as_bytes("b").equal?(s)`, `true`},
		// Appending raw bytes can invalidate the encoding, which is left unchanged.
		{`s = +"hello"; s.append_as_bytes("\xE2\x82"); p s.valid_encoding?`, `false`},
		{`s = +"hello"; s.append_as_bytes("\xE2\x82"); s.append_as_bytes("\xAC"); p s.valid_encoding?`, `true`},
		{`s = "".b; s.append_as_bytes("€"); p s.encoding.name`, `"ASCII-8BIT"`},
		// Integers append their least-significant byte, truncating and wrapping.
		{`s = "".b; s.append_as_bytes(0x131, 0x232, 0x333); p s.bytes`, `[49, 50, 51]`},
		{`s = "".b; s.append_as_bytes(-1, -256, -257); p s.bytes`, `[255, 0, 255]`},
		// Bignum arguments truncate to their least-significant byte too.
		{`s = "".b; s.append_as_bytes(2**64, 2**64 + 1, -(2**64 + 1)); p s.bytes`, `[0, 1, 255]`},
		// Mixed String and Integer arguments in one call.
		{`s = "hello".b; s.append_as_bytes("\xE2\x82", 12, 43, "\xAC"); p s.bytes`, `[104, 101, 108, 108, 111, 226, 130, 12, 43, 172]`},
		// No arguments is a no-op returning the receiver.
		{`s = +"x"; p s.append_as_bytes`, `"x"`},
		// A frozen receiver raises FrozenError.
		{`begin; "f".freeze.append_as_bytes("x"); rescue FrozenError => e; p e.class; end`, `FrozenError`},
		// Only String and Integer are accepted; the class name is reported.
		{`begin; (+"x").append_as_bytes(Object.new); rescue TypeError => e; p e.message; end`, `"wrong argument type Object (expected String or Integer)"`},
		{`class Foo; end; begin; (+"x").append_as_bytes(Foo.new); rescue TypeError => e; p e.message; end`, `"wrong argument type Foo (expected String or Integer)"`},
		{`begin; (+"x").append_as_bytes(1.5); rescue TypeError => e; p e.message; end`, `"wrong argument type Float (expected String or Integer)"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
