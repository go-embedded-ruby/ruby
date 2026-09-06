// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestStrComparableDirect exercises strComparable's branches directly (the
// symmetric ASCII-compatible case is unreachable from #==/#eql?, whose byte
// comparison short-circuits first). Mirrors rb_str_comparable. Verified against
// MRI 4.0.5.
func TestStrComparableDirect(t *testing.T) {
	vm := New(io.Discard)
	mk := func(b []byte, enc string) *object.String { return object.NewStringBytesEnc(b, enc) }
	cases := []struct {
		a, b *object.String
		want bool
	}{
		// Same encoding always compares.
		{mk([]byte{0xff}, "UTF-8"), mk([]byte{0xff}, "UTF-8"), true},
		// Different encodings, both all-ASCII (7-bit) -> comparable.
		{mk([]byte("hi"), "UTF-8"), mk([]byte("hi"), "ISO-8859-1"), true},
		// self 7-bit, other non-ASCII in an ASCII-compatible encoding.
		{mk([]byte("a"), "UTF-8"), mk([]byte{0xe9}, "ISO-8859-1"), true},
		// self non-ASCII in an ASCII-compatible encoding, other 7-bit (symmetric).
		{mk([]byte{0xe9}, "ISO-8859-1"), mk([]byte("a"), "UTF-8"), true},
		// Neither side is 7-bit -> not comparable.
		{mk([]byte{0xff}, "UTF-8"), mk([]byte{0xff}, "ISO-8859-1"), false},
		// self 7-bit but other's encoding is not ASCII-compatible.
		{mk([]byte("ab"), "UTF-8"), mk([]byte("ab"), "UTF-16LE"), false},
	}
	for i, c := range cases {
		if got := vm.strComparable(c.a, c.b); got != c.want {
			t.Errorf("case %d: strComparable(%q/%s, %q/%s) = %v, want %v",
				i, c.a.Str(), c.a.EncName(), c.b.Str(), c.b.EncName(), got, c.want)
		}
	}
}

// TestCharBoundary covers charBoundary, the rb_enc_left_char_head gate used by
// String#start_with?/#end_with? to reject a match that would split a character.
// Each encoding branch is exercised directly. Verified against ruby 4.0.5.
func TestCharBoundary(t *testing.T) {
	cases := []struct {
		b    []byte
		pos  int
		enc  string
		want bool
	}{
		// Ends of the string are always boundaries, whatever the encoding.
		{[]byte{0xC3, 0xA9}, 0, "UTF-8", true},
		{[]byte{0xC3, 0xA9}, 2, "UTF-8", true},
		// UTF-8: a continuation byte (0x80..0xBF) is mid-character; a lead/ASCII
		// byte is a head. An empty encoding is the UTF-8 default.
		{[]byte{0xC3, 0xA9}, 1, "UTF-8", false},
		{[]byte{0xC3, 0xA9}, 1, "", false},
		{[]byte{0x41, 0x42}, 1, "UTF-8", true},
		// UTF-16BE: an odd offset splits a code unit; a low surrogate preceded by a
		// high surrogate is the tail of a 4-byte pair; an ordinary unit is a head.
		{[]byte{0xD8, 0x00, 0xDC, 0x00}, 1, "UTF-16BE", false},
		{[]byte{0xD8, 0x00, 0xDC, 0x00}, 2, "UTF-16BE", false},
		{[]byte{0x00, 0x41, 0x00, 0x42}, 2, "UTF-16BE", true},
		// UTF-16LE: mirror of the above with byte order swapped.
		{[]byte{0x00, 0xD8, 0x00, 0xDC}, 1, "UTF-16LE", false},
		{[]byte{0x00, 0xD8, 0x00, 0xDC}, 2, "UTF-16LE", false},
		{[]byte{0x41, 0x00, 0x42, 0x00}, 2, "UTF-16LE", true},
		// UTF-32: a boundary is any 4-byte multiple.
		{[]byte{0, 0, 0, 0x41, 0, 0, 0, 0x42}, 4, "UTF-32BE", true},
		{[]byte{0, 0, 0, 0x41, 0, 0, 0, 0x42}, 3, "UTF-32BE", false},
		{[]byte{0x41, 0, 0, 0, 0x42, 0, 0, 0}, 4, "UTF-32LE", true},
		{[]byte{0x41, 0, 0, 0, 0x42, 0, 0, 0}, 5, "UTF-32LE", false},
		// A single-byte / unknown encoding treats every byte as a character head.
		{[]byte{0x80, 0x80}, 1, "ISO-8859-1", true},
	}
	for _, c := range cases {
		if got := charBoundary(c.b, c.pos, c.enc); got != c.want {
			t.Errorf("charBoundary(%v, %d, %q) = %v, want %v", c.b, c.pos, c.enc, got, c.want)
		}
	}
}

// TestStringEqlComparable covers the rb_str_comparable encoding gate added to
// String#== and String#eql?: equal bytes compare only when the encodings are the
// same or one side is all-ASCII in an ASCII-compatible encoding, so an
// invalid/non-ASCII byte tagged differently is unequal. Verified against MRI 4.0.5.
func TestStringEqlComparable(t *testing.T) {
	cases := []struct{ src, want string }{
		// Same encoding, equal / unequal content.
		{`p "hello" == "hello"`, "true"},
		{`p "hello".eql?("hello")`, "true"},
		{`p "more".eql?("MORE")`, "false"},
		// Different but compatible encodings, all-ASCII content -> equal.
		{`p "hello".dup.force_encoding("utf-8") == "hello".dup.force_encoding("iso-8859-1")`, "true"},
		{`p "hello".dup.force_encoding("utf-8").eql?("hello".dup.force_encoding("iso-8859-1"))`, "true"},
		// Incompatible: non-ASCII bytes tagged with different encodings -> unequal.
		{`p "\xff".dup.force_encoding("utf-8").eql?("\xff".dup.force_encoding("iso-8859-1"))`, "false"},
		// A non-ASCII-compatible encoding (UTF-32LE) never compares by ASCII bytes.
		{`p "abcd".dup.force_encoding("utf-8").eql?("abcd".dup.force_encoding("utf-32le"))`, "false"},
		// Two empty strings compare even when one encoding is not ASCII-compatible.
		{`p "".eql?("".dup.force_encoding("iso-2022-jp"))`, "true"},
		// Comparable by the symmetric ASCII-compatible branch (self is non-ASCII in
		// an ASCII-compatible encoding, other is 7-bit) but with differing content.
		{`p "\xe9".dup.force_encoding("iso-8859-1").eql?("a")`, "false"},
		// eql? does not use #to_str; == does defer to the other operand.
		{`p "hello".eql?(42)`, "false"},
		{`class S; def to_str; "hello"; end; def ==(o); true; end; end; p "hello" == S.new`, "true"},
		{`p "hello" == 42`, "false"},
		// A String subclass unwraps for both.
		{`class MyS < String; end; p "hello".eql?(MyS.new("hello"))`, "true"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestStringStartEndWithBoundary covers String#start_with?/#end_with? honouring
// character boundaries, scanning every argument, converting via #to_str, and
// negotiating encodings. Verified against MRI 4.0.5.
func TestStringStartEndWithBoundary(t *testing.T) {
	cases := []struct{ src, want string }{
		// A prefix/suffix that splits a multibyte character does not match.
		{`p "\xC3\xA9".start_with?("\xC3")`, "false"},
		{`p "\xe3\x81\x82".start_with?("\xe3")`, "false"},
		{`p "\xC3\xA9".end_with?("\xA9")`, "false"},
		{`p "\xe3\x81\x82".end_with?("\x82")`, "false"},
		// A whole-string match at a boundary still matches.
		{`p "\xA9".start_with?("\xA9")`, "true"},
		{`p "hello".end_with?("llo")`, "true"},
		// A UTF-16BE surrogate tail is not a boundary.
		{`p "\xd8\x00\xdc\x00".dup.force_encoding("UTF-16BE").end_with?("\xdc\x00".dup.force_encoding("UTF-16BE"))`, "false"},
		// Multiple arguments: true if any matches; zero arguments -> false.
		{`p "hello".start_with?("x", "y", "he", "z")`, "true"},
		{`p "hello".end_with?("x", "y", "llo", "z")`, "true"},
		{`p "hello".end_with?()`, "false"},
		// #to_str conversion of a non-String argument.
		{`o = Object.new; def o.to_str; "he"; end; p "hello".start_with?(o)`, "true"},
		{`o = Object.new; def o.to_str; "lo"; end; p "hello".end_with?(o)`, "true"},
		// Multibyte suffix.
		{`p "céréale".end_with?("réale")`, "true"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// A non-convertible argument raises TypeError; incompatible encodings raise.
	for _, tc := range []struct{ src, cls string }{
		{`"hello".end_with?(1)`, "TypeError"},
		{`"hello".end_with?(["o"])`, "TypeError"},
		{`"あれ".end_with?("ア".encode(Encoding::EUC_JP))`, "Encoding::CompatibilityError"},
	} {
		if cls, _ := evalErr(t, tc.src); cls != tc.cls {
			t.Errorf("src=%q class=%q, want %q", tc.src, cls, tc.cls)
		}
	}
}

// TestStringToStrArguments covers String#include?/#prepend/#replace converting a
// non-String argument through #to_str and unwrapping String subclasses, plus the
// encoding adopted by #replace. Verified against MRI 4.0.5.
func TestStringToStrArguments(t *testing.T) {
	cases := []struct{ src, want string }{
		// include? via #to_str and via a String subclass.
		{`o = Object.new; def o.to_str; "lo"; end; p "hello".include?(o)`, "true"},
		{`class MyS < String; end; p "hello".include?(MyS.new("lo"))`, "true"},
		// prepend via #to_str and a subclass; multiple arguments concatenate.
		{`o = Object.new; def o.to_str; "ab"; end; p "cd".dup.prepend(o)`, `"abcd"`},
		{`class MyS < String; end; p "cd".dup.prepend(MyS.new("ab"))`, `"abcd"`},
		{`p "z".dup.prepend("a", "b")`, `"abz"`},
		// replace via #to_str and its encoding / invalidity carry-over.
		{`o = Object.new; def o.to_str; "new"; end; p "old".dup.replace(o)`, `"new"`},
		{`a = "".encode("UTF-16LE"); b = "".encode("UTF-8"); a.replace(b); p a.encoding.name`, `"UTF-8"`},
		{`p "".dup.tap { |s| s.replace("\u{8765}".dup.force_encoding("ascii")) }.valid_encoding?`, "false"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	for _, tc := range []struct{ src, cls string }{
		{`"hello".include?(1)`, "TypeError"},
		{`"cd".dup.prepend(1)`, "TypeError"},
		{`"old".dup.replace(123)`, "TypeError"},
		{`"hello".freeze.replace("x")`, "FrozenError"},
	} {
		if cls, _ := evalErr(t, tc.src); cls != tc.cls {
			t.Errorf("src=%q class=%q, want %q", tc.src, cls, tc.cls)
		}
	}
}

// TestStringBytesliceSetbyteCoerce covers String#byteslice/#setbyte coercing
// index and length through #to_int (Float truncates, #to_int object converts),
// raising RangeError past a machine long, and byteslice accepting a Range
// subclass and #to_int range bounds. Verified against MRI 4.0.5.
func TestStringBytesliceSetbyteCoerce(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "hello".byteslice(0.5)`, `"h"`},
		{`o = Object.new; def o.to_int; 1; end; p "hello".byteslice(o)`, `"e"`},
		{`p "hello".byteslice(1.9, 2.9)`, `"el"`},
		// A too-large length clamps to the end; out-of-range / negative return nil.
		{`p "hello".byteslice(2, 10)`, `"llo"`},
		{`p "hello".byteslice(10)`, `nil`},
		{`p "hello".byteslice(2, -1)`, `nil`},
		{`p "hello".byteslice(10, 2)`, `nil`},
		{`class R < Range; end; p "hello".byteslice(R.new(1, 3))`, `"ell"`},
		{`o = Object.new; def o.to_int; 1; end; p "hello".byteslice(o..3)`, `"ell"`},
		{`p "hello".byteslice(9..)`, `nil`},
		{`p "abc".dup.tap { |s| s.setbyte(0, 100) }`, `"dbc"`},
		{`o = Object.new; def o.to_int; 1; end; p "abc".dup.tap { |s| s.setbyte(o, 100) }`, `"adc"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	for _, src := range []string{
		`"hello".byteslice(18446744073709551616)`,
		`"hello".byteslice(0, 18446744073709551616)`,
		`"hello".byteslice(-18446744073709551616, 1)`,
	} {
		if cls, _ := evalErr(t, src); cls != "RangeError" {
			t.Errorf("src=%q class=%q, want RangeError", src, cls)
		}
	}
}

// TestStringInitialize covers String#initialize / String.new: the encoding:
// keyword (a name or an Encoding), a no-argument call that leaves self unchanged
// (and never raises on a frozen receiver), #initialize being private, the binary
// default of String.new, and the FrozenError raised when a positional argument
// modifies a frozen receiver. Verified against MRI 4.0.5.
func TestStringInitialize(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p String.new.encoding.name`, `"ASCII-8BIT"`},
		{`p String.new("t").encoding.name`, `"UTF-8"`},
		{`p String.new("x", encoding: "euc-jp").encoding.name`, `"EUC-JP"`},
		{`p String.new("x", encoding: Encoding::EUC_JP).encoding.name`, `"EUC-JP"`},
		{`p String.new("abc", capacity: 100000)`, `"abc"`},
		{`s = "some string"; s.send(:initialize); p s`, `"some string"`},
		{`a = "hello".freeze; p a.send(:initialize).equal?(a)`, "true"},
		{`p String.private_instance_methods(false).include?(:initialize)`, "true"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	for _, tc := range []struct{ src, cls string }{
		{`"hello".freeze.send(:initialize, "world")`, "FrozenError"},
		{`"hello".freeze.send(:initialize, "hello".freeze)`, "FrozenError"},
		{`String.new(123)`, "TypeError"},
		{`String.new(nil)`, "TypeError"},
	} {
		if cls, _ := evalErr(t, tc.src); cls != tc.cls {
			t.Errorf("src=%q class=%q, want %q", tc.src, cls, tc.cls)
		}
	}
}

// TestStringJustifyInsertEncoding covers String#ljust/#rjust/#center/#insert
// negotiating the pad/insert encoding (rb_str_justify / rb_str_update call
// rb_enc_check): the result carries the combined encoding, an incompatible pad
// raises Encoding::CompatibilityError, and each padding side and edge is
// exercised. Verified against MRI 4.0.5.
func TestStringJustifyInsertEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "abc".ljust(7, "12")`, `"abc1212"`},
		{`p "abc".rjust(6, "xy")`, `"xyxabc"`},
		{`p "abc".center(7, "-")`, `"--abc--"`},
		{`p "abcdef".center(3)`, `"abcdef"`}, // already wide enough
		{`p "abc".ljust(2)`, `"abc"`},        // default pad, no widening
		// The result adopts the combined encoding of receiver and pad.
		{`p "abc".dup.force_encoding("IBM437").center(6, "あ").encoding.name`, `"UTF-8"`},
		{`p "abc".dup.force_encoding("IBM437").ljust(6).encoding.name`, `"IBM437"`},
		// #insert: basic, negative index, #to_str pad, and combined encoding.
		{`p "abc".dup.insert(1, "X")`, `"aXbc"`},
		{`p "abc".dup.insert(-1, "X")`, `"abcX"`},
		{`o = Object.new; def o.to_str; "X"; end; p "abc".dup.insert(1, o)`, `"aXbc"`},
		{`s = "".dup.force_encoding("US-ASCII"); s.insert(0, "ありがとう"); p s.encoding.name`, `"UTF-8"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	for _, tc := range []struct{ src, cls string }{
		{`"abc".ljust(6, "")`, "ArgumentError"},
		{`"あれ".center(5, "ア".encode(Encoding::EUC_JP))`, "Encoding::CompatibilityError"},
		{`"abc".insert(10, "X")`, "IndexError"},
		{`"あれ".insert(0, "ア".encode(Encoding::EUC_JP))`, "Encoding::CompatibilityError"},
		{`"abc".insert(1, 42)`, "TypeError"},
	} {
		if cls, _ := evalErr(t, tc.src); cls != tc.cls {
			t.Errorf("src=%q class=%q, want %q", tc.src, cls, tc.cls)
		}
	}
}
