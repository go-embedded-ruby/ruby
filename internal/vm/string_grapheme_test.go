// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestStringGraphemeClusters covers String#grapheme_clusters across every UAX#29
// break rule (GB3–GB13/GB999): CRLF, control breaks, Hangul L/V/T conjoining,
// combining marks (GB9), SpacingMark (GB9a), Prepend (GB9b), the ZWJ emoji rule
// (GB11) and regional-indicator flag pairing (GB12/13). The per-cluster byte
// sizes are asserted (inspect escaping of control bytes is out of scope) against
// MRI 4.0.5.
func TestStringGraphemeClusters(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "hello".grapheme_clusters`, "[\"h\", \"e\", \"l\", \"l\", \"o\"]\n"},                                   // GB999 + chars-like
		{`p "".grapheme_clusters`, "[]\n"},                                                                         // empty
		{`p "\r\n".grapheme_clusters.map(&:bytesize)`, "[2]\n"},                                                    // GB3
		{`p "a\tb".grapheme_clusters.map(&:bytesize)`, "[1, 1, 1]\n"},                                              // GB4/GB5 control
		{`p "\u{AC01}".grapheme_clusters.map(&:bytesize)`, "[3]\n"},                                                // precomposed 각
		{`p "\u{1100}\u{1161}\u{11A8}".grapheme_clusters.map(&:bytesize)`, "[9]\n"},                                // GB6/7/8 L V T
		{`p "\u{AC00}\u{1161}".grapheme_clusters.map(&:bytesize)`, "[6]\n"},                                        // GB7 LV+V
		{`p "\u{1100}\u{1100}".grapheme_clusters.map(&:bytesize)`, "[6]\n"},                                        // GB6 L×L
		{`p "\u{AC01}\u{11A8}".grapheme_clusters.map(&:bytesize)`, "[6]\n"},                                        // GB8 LVT×T
		{`p "é".grapheme_clusters.map(&:bytesize)`, "[3]\n"},                                                      // GB9 combining
		{`p "\u{0915}\u{0903}".grapheme_clusters.map(&:bytesize)`, "[6]\n"},                                        // GB9a SpacingMark
		{`p "\u{0600}\u{0915}".grapheme_clusters.map(&:bytesize)`, "[5]\n"},                                        // GB9b Prepend
		{`p "\u{1F1E6}\u{1F1E7}\u{1F1E8}".grapheme_clusters.map(&:bytesize)`, "[8, 4]\n"},                          // GB12/13 flags
		{`p "\u{1F1E6}".grapheme_clusters.map(&:bytesize)`, "[4]\n"},                                               // lone RI
		{`p "ab\u{1f3f3}\u{fe0f}\u{200d}\u{1f308}\u{1F43E}".grapheme_clusters.map(&:bytesize)`, "[1, 1, 14, 4]\n"}, // GB11 ZWJ emoji
		{`p "a\u{1F468}\u{200D}\u{1F469}\u{200D}\u{1F467}b".grapheme_clusters.map(&:bytesize)`, "[1, 18, 1]\n"},    // GB11 family ZWJ
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestStringGraphemeClustersContent asserts the exact substrings (not just sizes)
// for the ASCII and rainbow-flag cases, and that encoding is preserved.
func TestStringGraphemeClustersContent(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "ab\u{1f3f3}\u{fe0f}\u{200d}\u{1f308}\u{1F43E}".grapheme_clusters`, "[\"a\", \"b\", \"🏳️‍🌈\", \"🐾\"]\n"},
		{`p "abc".grapheme_clusters.map(&:encoding).map(&:name)`, "[\"UTF-8\", \"UTF-8\", \"UTF-8\"]\n"},
		{`p "abc".b.grapheme_clusters.map(&:encoding).map(&:name)`, "[\"ASCII-8BIT\", \"ASCII-8BIT\", \"ASCII-8BIT\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestStringEachGraphemeCluster covers the block form (yields each cluster,
// returns self) and the no-block form (an Enumerator whose #size and #to_a match).
func TestStringEachGraphemeCluster(t *testing.T) {
	cases := []struct{ src, want string }{
		{`a=[]; r="ab\u{1f3f3}\u{fe0f}\u{200d}\u{1f308}".each_grapheme_cluster{|c| a<<c}; p a; p r.equal?("ab\u{1f3f3}\u{fe0f}\u{200d}\u{1f308}") == false`,
			"[\"a\", \"b\", \"🏳️‍🌈\"]\ntrue\n"},
		{`p "abc".each_grapheme_cluster{|c|}.equal?("abc") == false`, "true\n"}, // returns self (a distinct literal)
		{`p "hello".each_grapheme_cluster.class`, "Enumerator\n"},
		{`p "hello".each_grapheme_cluster.to_a`, "[\"h\", \"e\", \"l\", \"l\", \"o\"]\n"},
		{`p "hello".each_grapheme_cluster.size`, "5\n"},
		{`p "a\u{1F1E6}\u{1F1E7}b".each_grapheme_cluster.size`, "3\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestStringEachGraphemeClusterReturnsSelf verifies the block form returns the
// very same receiver object (equal? identity).
func TestStringEachGraphemeClusterReturnsSelf(t *testing.T) {
	if got := eval(t, `s="hi"; p s.each_grapheme_cluster{|c|}.equal?(s)`); got != "true\n" {
		t.Errorf("got %q, want true", got)
	}
}

// TestStringByteindex covers String#byteindex with String and Regexp needles,
// default/positive/negative byte offsets, empty needle, not-found (nil), the
// $~ side effect for Regexp, and the multibyte character-boundary IndexError.
func TestStringByteindex(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "hello".byteindex("l")`, "2\n"},
		{`p "hello".byteindex("l", 3)`, "3\n"},
		{`p "hello".byteindex("x")`, "nil\n"},
		{`p "hello".byteindex("l", -2)`, "3\n"},
		{`p "abc".byteindex("a", 10)`, "nil\n"},  // offset past end
		{`p "abc".byteindex("a", -10)`, "nil\n"}, // negative past start
		{`p "abc".byteindex("")`, "0\n"},         // empty needle
		{`p "abc".byteindex("", 3)`, "3\n"},      // empty at end
		{`p "abc".byteindex("", 4)`, "nil\n"},    // empty past end
		{`p "abc".byteindex("c", 3)`, "nil\n"},   // needle past offset
		{`p "こんにちは".byteindex("に")`, "6\n"},
		{`p "こんにちは".byteindex("に", 3)`, "6\n"},
		{`p "hello".byteindex(/l+/)`, "2\n"},
		{`p "こんにちは".byteindex(/に/, 3)`, "6\n"},
		{`p "abc".byteindex(/z/)`, "nil\n"},            // regexp no match
		{`"hello".byteindex(/l/); p $~[0]`, "\"l\"\n"}, // sets $~
		{`"abc".byteindex(/z/); p $~`, "nil\n"},        // $~ cleared on no match
		{`p "abc".byteindex(//, 3)`, "3\n"},            // empty regexp at end
		{`p "abc".byteindex(//, 4)`, "nil\n"},          // empty regexp past end
		{`p "abc".b.byteindex("c", 1)`, "2\n"},         // binary: every offset is a boundary
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	assertRaise(t, `"こんにちは".byteindex("に", 1)`, "IndexError", "does not land on character boundary")
	assertRaise(t, `"hello".byteindex(:sym)`, "TypeError", "no implicit conversion of Symbol into String")
	assertRaise(t, `"こ".byteindex(/x/, 1)`, "IndexError", "does not land on character boundary")
}

// TestStringByterindex covers String#byterindex: last match at or before the
// offset (default bytesize), String and Regexp (including overlapping) needles,
// negative/oversized offsets, empty needle, and the boundary IndexError.
func TestStringByterindex(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "hello".byterindex("l")`, "3\n"},
		{`p "hello".byterindex("l", 2)`, "2\n"},
		{`p "hello".byterindex("l", 1)`, "nil\n"},
		{`p "foofoo".byterindex("foo")`, "3\n"},
		{`p "foofoo".byterindex("foo", 2)`, "0\n"},
		{`p "hello".byterindex("x")`, "nil\n"},
		{`p "hello".byterindex("l", -1)`, "3\n"},
		{`p "hello".byterindex("l", 100)`, "3\n"},  // offset clamped to end
		{`p "hello".byterindex("l", -6)`, "nil\n"}, // negative past start
		{`p "hello".byterindex("")`, "5\n"},        // empty, default offset
		{`p "hello".byterindex("", 2)`, "2\n"},     // empty at offset
		{`p "hello".byterindex("lo", 3)`, "3\n"},   // match starting exactly at offset
		{`p "hello".byterindex("lo", 2)`, "nil\n"}, // start beyond offset
		{`p "hello".byterindex(/l/)`, "3\n"},
		{`p "hello".byterindex(/l/, 2)`, "2\n"},
		{`p "hello".byterindex(/l/, 1)`, "nil\n"},
		{`p "aaa".byterindex(/aa/)`, "1\n"}, // overlapping regexp
		{`p "aXaXa".byterindex(/aX/, 1)`, "0\n"},
		{`p "abc".byterindex(/z/)`, "nil\n"}, // regexp no match
		{`"hello".byterindex(/l/); p $~[0]`, "\"l\"\n"},
		{`"abc".byterindex(/z/); p $~`, "nil\n"},
		{`p "abc".byterindex(//)`, "3\n"},       // empty regexp default offset
		{`p "abc".b.byterindex("b", 1)`, "1\n"}, // binary: every offset is a boundary
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	assertRaise(t, `"こんにちは".byterindex("に", 4)`, "IndexError", "does not land on character boundary")
	assertRaise(t, `"こ".byterindex(/x/, 1)`, "IndexError", "does not land on character boundary")
	assertRaise(t, `"hello".byterindex(:sym)`, "TypeError", "no implicit conversion of Symbol into String")
}

// TestStringBytesplice covers every String#bytesplice arity plus the in-place
// replacement result and self return.
func TestStringBytesplice(t *testing.T) {
	cases := []struct{ src, want string }{
		{`s="hello"; p s.bytesplice(0,1,"H"); p s`, "\"Hello\"\n\"Hello\"\n"},
		{`s="hello"; s.bytesplice(1,3,"XYZ"); p s`, "\"hXYZo\"\n"},
		{`s="hello"; p s.bytesplice(0,1,"H").equal?(s)`, "true\n"},             // returns self
		{`s="hello"; s.bytesplice(1,100,"X"); p s`, "\"hX\"\n"},                // length clamped
		{`s="hello"; s.bytesplice(5,0,"!"); p s`, "\"hello!\"\n"},              // append at end
		{`s="hello"; s.bytesplice(-1,1,"O"); p s`, "\"hellO\"\n"},              // negative index
		{`s="hello"; s.bytesplice(1,2,""); p s`, "\"hlo\"\n"},                  // deletion
		{`s="hello"; s.bytesplice(0,5,"abcdefg",2,3); p s`, "\"cde\"\n"},       // index/length/str/si/sl
		{`s="hello"; s.bytesplice(0,1,"abc",2,10); p s`, "\"cello\"\n"},        // str_length clamped
		{`s="hello"; s.bytesplice(0,1,"abcde",-2,2); p s`, "\"deello\"\n"},     // negative str_index
		{`s="hello"; s.bytesplice(1..3,"XY"); p s`, "\"hXYo\"\n"},              // range
		{`s="hello"; s.bytesplice(1...3,"XY"); p s`, "\"hXYlo\"\n"},            // exclusive range
		{`s="hello"; s.bytesplice(1..,"XY"); p s`, "\"hXY\"\n"},                // endless range
		{`s="hello"; s.bytesplice(..2,"XY"); p s`, "\"XYlo\"\n"},               // beginless range
		{`s="hello"; s.bytesplice(2..1,"XY"); p s`, "\"heXYllo\"\n"},           // inverted range = insert
		{`s="hello"; s.bytesplice(3..1,"X"); p s`, "\"helXlo\"\n"},             // inverted (end<begin) = insert
		{`s="hello"; s.bytesplice(-4..-2,"X"); p s`, "\"hXo\"\n"},              // negative range bounds
		{`s="hello"; s.bytesplice(0..4,"abcdefg",2..4); p s`, "\"cde\"\n"},     // range + str_range
		{`s="hello"; s.bytesplice(0..1,"abcdef",2..10); p s`, "\"cdefllo\"\n"}, // str_range clamped
		{`s="こんにちは"; s.bytesplice(3,3,"X"); p s`, "\"こXにちは\"\n"},               // multibyte boundary ok
		{`s="こんにちは"; s.bytesplice(0..2,"X"); p s`, "\"Xんにちは\"\n"},              // multibyte range ok
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestStringBytespliceErrors covers every error branch: bad index/length,
// boundary violations, source-slice violations, arity, non-Range where a Range
// is required, wrong types, and frozen receiver.
func TestStringBytespliceErrors(t *testing.T) {
	assertRaise(t, `"hello".bytesplice(10,1,"x")`, "IndexError", "index 10 out of string")
	assertRaise(t, `"hello".bytesplice(-10,1,"x")`, "IndexError", "index -10 out of string")
	assertRaise(t, `"hello".bytesplice(0,-1,"x")`, "IndexError", "negative length -1")
	assertRaise(t, `"こんにちは".bytesplice(1,3,"x")`, "IndexError", "offset 1 does not land on character boundary")
	assertRaise(t, `"こんにちは".bytesplice(3,1,"x")`, "IndexError", "offset 4 does not land on character boundary")
	assertRaise(t, `"こんにちは".bytesplice(0..1,"x")`, "IndexError", "offset 2 does not land on character boundary")
	assertRaise(t, `"hello".bytesplice(10..12,"x")`, "RangeError", "10..12 out of range")
	assertRaise(t, `"hello".bytesplice(0,1,"abc",5,1)`, "IndexError", "index 5 out of string")
	assertRaise(t, `"hello".bytesplice(0,1,"abc",-5,1)`, "IndexError", "index -5 out of string")
	assertRaise(t, `"hello".bytesplice(0,1,"abc",0,-1)`, "IndexError", "negative length -1")
	assertRaise(t, `"x".bytesplice(0,1,"こ",1,1)`, "IndexError", "offset 1 does not land on character boundary")
	assertRaise(t, `"x".bytesplice(0..0,"abc",5..6)`, "RangeError", "5..6 out of range")
	assertRaise(t, `"hello".bytesplice(0,1,5)`, "TypeError", "no implicit conversion of Integer into String")
	assertRaise(t, `"hello".bytesplice("x",1,"y")`, "TypeError", "no implicit conversion of String into Integer")
	assertRaise(t, `"hello".bytesplice(0,1)`, "TypeError", "wrong argument type Integer (expected Range)")
	assertRaise(t, `"hello".bytesplice(0..1,"a",5)`, "TypeError", "wrong argument type Integer (expected Range)")
	assertRaise(t, `"hello".bytesplice(0,1,"a",2)`, "ArgumentError", "wrong number of arguments (given 4, expected 2, 3, or 5)")
	assertRaise(t, `"hi".freeze.bytesplice(0,1,"x")`, "FrozenError", "")
}
