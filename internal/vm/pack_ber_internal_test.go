// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestPackBER covers Array#pack's 'w' directive (BER-compressed integers): the
// base-128 big-endian encoding with the high bit set on all but the last byte,
// zero as a single NUL, arbitrary-size (Bignum) values, the count/'*' modifier,
// a Float truncated toward zero, #to_int coercion, the ArgumentError for a
// negative value, and the BINARY result encoding. Verified against ruby 4.0.6.
func TestPackBER(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [0].pack("w").bytes`, `[0]`},
		{`p [1].pack("w").bytes`, `[1]`},
		{`p [127].pack("w").bytes`, `[127]`},
		{`p [128].pack("w").bytes`, `[129, 0]`},
		{`p [9999].pack("w").bytes`, `[206, 15]`},
		{`p [16384].pack("w").bytes`, `[129, 128, 0]`},
		{`p [2**65].pack("w").bytes`, `[132, 128, 128, 128, 128, 128, 128, 128, 128, 0]`},
		// count / '*' modifiers.
		{`p [1, 2, 300].pack("w*").bytes`, `[1, 2, 130, 44]`},
		{`p [1, 2, 3].pack("w2").bytes`, `[1, 2]`},
		// A Float is truncated toward zero; #to_int coerces an object.
		{`p [1.9].pack("w").bytes`, `[1]`},
		{`class I; def to_int; 130; end; end; p [I.new].pack("w").bytes`, `[129, 2]`},
		// The result is BINARY.
		{`p [1].pack("w").encoding.name`, `"ASCII-8BIT"`},
		// A negative value raises ArgumentError; a non-integer raises TypeError.
		{`begin; [-1].pack("w"); rescue ArgumentError => e; p e.message; end`, `"can't compress negative numbers"`},
		{`begin; [Object.new].pack("w"); rescue TypeError => e; p e.class; end`, `TypeError`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestUnpackBER covers String#unpack's 'w' directive: decoding one BER integer,
// a numeric count, the '*' modifier decoding every integer, two directives each
// taking one, Bignum results, and a pack/unpack round trip. Verified against ruby
// 4.0.6.
func TestUnpackBER(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "\x00".unpack("w")`, `[0]`},
		{`p "\xce\x0f".unpack("w")`, `[9999]`},
		{`p "\x01\x02\x03\x04".unpack("w*")`, `[1, 2, 3, 4]`},
		{`p "\x01\x02\x03".unpack("w w")`, `[1, 2]`},
		{`p "\x01\x02\x03".unpack("w2")`, `[1, 2]`},
		{`p "\x84\x80\x80\x80\x80\x80\x80\x80\x80\x00".unpack("w")`, `[36893488147419103232]`},
		// A round trip over a mix of small and Bignum values is the identity.
		{`a = [0, 127, 128, 9999, 2**65, 1]; p a.pack("w*").unpack("w*") == a`, `true`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
