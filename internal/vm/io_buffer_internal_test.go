// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestIOBuffer covers the in-memory IO::Buffer: construction (.new/.for/.string),
// the state predicates, byte- and value-level access with every data type, the
// bitwise operators and bit_count, freeing, and the error paths (out-of-bounds,
// read-only, unknown/wrong type). Verified against ruby 4.0.6.
func TestIOBuffer(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction and default state.
		{`p IO::Buffer.new.size`, `65536`},
		{`p IO::Buffer.new(8).size`, `8`},
		{`p IO::Buffer.new(4).get_string`, `"\x00\x00\x00\x00"`},
		{`b = IO::Buffer.new(4); p [b.internal?, b.external?, b.mapped?, b.readonly?, b.null?, b.empty?, b.valid?, b.shared?, b.private?, b.locked?]`, `[true, false, false, false, false, false, true, false, false, false]`},
		{`p IO::Buffer::DEFAULT_SIZE`, `65536`},
		{`p [IO::Buffer::PAGE_SIZE, IO::Buffer::READONLY, IO::Buffer::MAPPED]`, `[16384, 128, 4]`},
		{`p IO::Buffer::AccessError.ancestors.include?(RuntimeError)`, `true`},
		// .for copies a String into a read-only external buffer.
		{`b = IO::Buffer.for("abc"); p [b.get_string, b.external?, b.internal?, b.readonly?]`, `["abc", true, false, true]`},
		{`p IO::Buffer.for("xy") { |b| b.get_string }`, `"xy"`},
		// .string yields a writable external buffer and returns its bytes.
		{`p IO::Buffer.string(4) { |b| b.set_string("hi") }`, `"hi\x00\x00"`},
		// get_string with offset / length / encoding.
		{`b = IO::Buffer.new(8); b.set_string("hello", 1); p b.get_string(1, 5)`, `"hello"`},
		{`b = IO::Buffer.for("café"); p b.get_string(0, 3, "UTF-8").encoding.name`, `"UTF-8"`},
		// Every integer/float type round-trips (big-endian upper-case, little lower).
		{`b = IO::Buffer.new(8); b.set_value(:U8, 0, 200); p b.get_value(:U8, 0)`, `200`},
		{`b = IO::Buffer.new(8); b.set_value(:S8, 0, -5); p b.get_value(:S8, 0)`, `-5`},
		{`b = IO::Buffer.new(8); b.set_value(:U16, 0, 0x1234); p [b.get_value(:U16, 0), b.get_string(0, 2).bytes]`, `[4660, [18, 52]]`},
		{`b = IO::Buffer.new(8); b.set_value(:u16, 0, 0x1234); p [b.get_value(:u16, 0), b.get_string(0, 2).bytes]`, `[4660, [52, 18]]`},
		{`b = IO::Buffer.new(8); b.set_value(:S16, 0, -2); p b.get_value(:S16, 0)`, `-2`},
		{`b = IO::Buffer.new(8); b.set_value(:s16, 0, -2); p b.get_value(:s16, 0)`, `-2`},
		{`b = IO::Buffer.new(8); b.set_value(:U32, 0, 0xdeadbeef); p b.get_value(:U32, 0)`, `3735928559`},
		{`b = IO::Buffer.new(8); b.set_value(:u32, 0, 0xdeadbeef); p b.get_value(:u32, 0)`, `3735928559`},
		{`b = IO::Buffer.new(8); b.set_value(:S32, 0, -3); p b.get_value(:S32, 0)`, `-3`},
		{`b = IO::Buffer.new(8); b.set_value(:s32, 0, -3); p b.get_value(:s32, 0)`, `-3`},
		{`b = IO::Buffer.new(8); b.set_value(:U64, 0, 2**63 + 1); p b.get_value(:U64, 0)`, `9223372036854775809`},
		{`b = IO::Buffer.new(8); b.set_value(:u64, 0, 2**63 + 1); p b.get_value(:u64, 0)`, `9223372036854775809`},
		{`b = IO::Buffer.new(8); b.set_value(:S64, 0, -9); p b.get_value(:S64, 0)`, `-9`},
		{`b = IO::Buffer.new(8); b.set_value(:s64, 0, -9); p b.get_value(:s64, 0)`, `-9`},
		{`b = IO::Buffer.new(8); b.set_value(:F32, 0, 1.5); p b.get_value(:F32, 0)`, `1.5`},
		{`b = IO::Buffer.new(8); b.set_value(:f32, 0, 1.5); p b.get_value(:f32, 0)`, `1.5`},
		{`b = IO::Buffer.new(8); b.set_value(:F64, 0, 2.5); p b.get_value(:F64, 0)`, `2.5`},
		{`b = IO::Buffer.new(8); b.set_value(:f64, 0, 2.5); p b.get_value(:f64, 0)`, `2.5`},
		// A float type accepts an Integer, an int type a Bignum operand.
		{`b = IO::Buffer.new(8); b.set_value(:F64, 0, 3); p b.get_value(:F64, 0)`, `3.0`},
		{`b = IO::Buffer.new(8); b.set_value(:U8, 0, (2**64 + 7)); p b.get_value(:U8, 0)`, `7`},
		// clear fills a range with a byte.
		{`b = IO::Buffer.new(4); b.set_string("abcd"); b.clear(0, 1, 2); p b.get_string.bytes`, `[97, 0, 0, 100]`},
		{`b = IO::Buffer.new(3); b.set_string("abc"); b.clear; p b.get_string.bytes`, `[0, 0, 0]`},
		// Bitwise operators combine two buffers; ~ complements.
		{`a = IO::Buffer.for("\x0f\xf0"); c = IO::Buffer.for("\xff\x0f"); p (a & c).get_string.bytes`, `[15, 0]`},
		{`a = IO::Buffer.for("\x0f\xf0"); c = IO::Buffer.for("\xff\x0f"); p (a | c).get_string.bytes`, `[255, 255]`},
		{`a = IO::Buffer.for("\x0f\xf0"); c = IO::Buffer.for("\xff\x0f"); p (a ^ c).get_string.bytes`, `[240, 255]`},
		{`p (~IO::Buffer.for("\x0f")).get_string.bytes`, `[240]`},
		{`p IO::Buffer.for("\xff\x01\x03").bit_count`, `11`},
		// == compares contents.
		{`p IO::Buffer.for("ab") == IO::Buffer.for("ab")`, `true`},
		{`p IO::Buffer.for("ab") == IO::Buffer.for("ac")`, `false`},
		{`p IO::Buffer.for("ab") == "ab"`, `false`},
		{`p IO::Buffer.new(2).to_s`, `"#<IO::Buffer>"`},
		{`p IO::Buffer.new(2).inspect`, `"#<IO::Buffer>"`},
		{`p(IO::Buffer.new(2) ? "truthy" : "falsy")`, `"truthy"`},
		// set_string coerces its argument via #to_str.
		{`class T; def to_str; "hi"; end; end; b = IO::Buffer.new(4); b.set_string(T.new); p b.get_string(0, 2)`, `"hi"`},
		{`p IO::Buffer.new(4, IO::Buffer::READONLY).readonly?`, `true`},
		{`b = IO::Buffer.new(6); b.set_string("abcdef", 0, 3); p b.get_string(0, 3)`, `"abc"`},
		{`b = IO::Buffer.new(6); b.set_string("abcdef", 0, 2, 2); p b.get_string(0, 2)`, `"cd"`},
		{`begin; IO::Buffer.new(2).set_value(:U32, 0, 1); rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		// free nullifies the buffer.
		{`b = IO::Buffer.new(4); b.free; p [b.null?, b.size, b.internal?, b.to_s]`, `[true, 0, false, "#<IO::Buffer 0x0000000000000000+0 NULL>"]`},
		// Error paths.
		{`begin; IO::Buffer.for("x").set_string("y"); rescue IO::Buffer::AccessError => e; p e.message; end`, `"Buffer is not writable!"`},
		{`begin; IO::Buffer.for("x").set_value(:U8, 0, 1); rescue IO::Buffer::AccessError => e; p e.class; end`, `IO::Buffer::AccessError`},
		{`begin; b = IO::Buffer.new(2); b.free; b.get_string; rescue IO::Buffer::AccessError => e; p e.message; end`, `"The buffer is not allocated!"`},
		{`begin; IO::Buffer.new(2).get_value(:U32, 0); rescue ArgumentError => e; p e.message; end`, `"Offset/length out of bounds!"`},
		{`begin; IO::Buffer.new(2).get_string(0, 5); rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; IO::Buffer.new(4).set_string("abc", 3); rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; IO::Buffer.new(4).get_value(:Q8, 0); rescue ArgumentError => e; p e.message; end`, `"Unknown type: Q8"`},
		{`begin; IO::Buffer.new(4).get_value("U8", 0); rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; IO::Buffer.new(4).set_value(:U8, 0, "x"); rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; IO::Buffer.new(4).set_value(:F32, 0, "x"); rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; IO::Buffer.new(4) & "x"; rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; IO::Buffer.new(4) & IO::Buffer.new(0); rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; IO::Buffer.for(:sym); rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; IO::Buffer.string(2); rescue LocalJumpError => e; p e.class; end`, `LocalJumpError`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
