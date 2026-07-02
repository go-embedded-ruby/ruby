// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestMsgpackConstants covers the MessagePack loadable module and its error tree
// (require "msgpack"). MessagePack and Msgpack name the same module object.
func TestMsgpackConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "msgpack"; p MessagePack.equal?(Msgpack)`, "true\n"},
		{`require "msgpack"; p MessagePack.is_a?(Module)`, "true\n"},
		// require returns true the first time, false afterwards.
		{`p require "msgpack"`, "true\n"},
		{`require "msgpack"; p require "msgpack"`, "false\n"},
		// Error tree.
		{`require "msgpack"; p MessagePack::Error < StandardError`, "true\n"},
		{`require "msgpack"; p MessagePack::PackError < MessagePack::Error`, "true\n"},
		{`require "msgpack"; p MessagePack::UnpackError < MessagePack::Error`, "true\n"},
		{`require "msgpack"; p MessagePack::MalformedFormatError < MessagePack::UnpackError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMsgpackRoundTrip covers a Ruby-level pack/unpack round-trip through rbgo
// across every value shape the binding maps: nil, booleans, integers (incl. a
// Bignum and a large unsigned), floats, strings, binary strings, symbols
// (packed as str, so unpacked as a String), arrays, hashes (order preserved),
// and Time (ext -1).
func TestMsgpackRoundTrip(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "msgpack"; p MessagePack.unpack(MessagePack.pack(nil))`, "nil\n"},
		{`require "msgpack"; p MessagePack.unpack(MessagePack.pack(true))`, "true\n"},
		{`require "msgpack"; p MessagePack.unpack(MessagePack.pack(false))`, "false\n"},
		{`require "msgpack"; p MessagePack.unpack(MessagePack.pack(42))`, "42\n"},
		{`require "msgpack"; p MessagePack.unpack(MessagePack.pack(-7))`, "-7\n"},
		{`require "msgpack"; p MessagePack.unpack(MessagePack.pack(2.5))`, "2.5\n"},
		// A large unsigned (> 2**63-1, a Bignum in Ruby but still 64-bit) round-trips
		// via the uint64 path and comes back == the original.
		{`require "msgpack"; n = 2 ** 63 + 1; p MessagePack.unpack(MessagePack.pack(n)) == n`, "true\n"},
		{`require "msgpack"; p MessagePack.unpack(MessagePack.pack("hi"))`, "\"hi\"\n"},
		{`require "msgpack"; p MessagePack.unpack(MessagePack.pack([1, "x", true]))`, "[1, \"x\", true]\n"},
		// Symbol packs as str; it unpacks as a String (gem default).
		{`require "msgpack"; p MessagePack.unpack(MessagePack.pack(:sym))`, "\"sym\"\n"},
		// Hash preserves key order.
		{`require "msgpack"; p MessagePack.unpack(MessagePack.pack({"a" => 1, "b" => 2}))`, "{\"a\" => 1, \"b\" => 2}\n"},
		// A binary (ASCII-8BIT) String round-trips as a binary String.
		{`require "msgpack"; s = "\xff\x00".b; p MessagePack.unpack(MessagePack.pack(s)) == s`, "true\n"},
		// pack returns an ASCII-8BIT String.
		{`require "msgpack"; p MessagePack.pack(1).encoding.name`, "\"ASCII-8BIT\"\n"},
		// Time round-trips (whole-second resolution) via ext -1.
		{`require "msgpack"; t = Time.at(1_700_000_000); p MessagePack.unpack(MessagePack.pack(t)).to_i`, "1700000000\n"},
		// dump / load are aliases of pack / unpack.
		{`require "msgpack"; p MessagePack.load(MessagePack.dump([9]))`, "[9]\n"},
		// Object#to_msgpack round-trips through unpack.
		{`require "msgpack"; p MessagePack.unpack([1, 2].to_msgpack)`, "[1, 2]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMsgpackErrors covers the error paths: packing an unrepresentable value and
// unpacking malformed bytes both raise, as does calling with no argument.
func TestMsgpackErrors(t *testing.T) {
	// Packing a Proc (outside the value model) raises ArgumentError.
	got := eval(t, `require "msgpack"
begin
  MessagePack.pack(proc {})
rescue ArgumentError
  puts "packerr"
end`)
	if !strings.Contains(got, "packerr") {
		t.Errorf("pack of Proc: got %q", got)
	}
	// Unpacking truncated/garbage bytes raises ArgumentError.
	got = eval(t, `require "msgpack"
begin
  MessagePack.unpack("\xc1".b)
rescue ArgumentError
  puts "unpackerr"
end`)
	if !strings.Contains(got, "unpackerr") {
		t.Errorf("unpack of garbage: got %q", got)
	}
	// No argument raises ArgumentError.
	for _, m := range []string{"pack", "unpack"} {
		src := `require "msgpack"
begin
  MessagePack.` + m + `
rescue ArgumentError
  puts "arity"
end`
		if got := eval(t, src); !strings.Contains(got, "arity") {
			t.Errorf("%s no-arg: got %q", m, got)
		}
	}
}
