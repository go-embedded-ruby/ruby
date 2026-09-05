// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestArrayAliasesAndArity covers the wave-12 residual fixes that align Array
// method identity and argument checking with MRI 4.0.6: #length is the same
// method record as #size, #clear/#map reject stray arguments, and Array.allocate
// yields a fully-formed empty Array while rejecting arguments.
func TestArrayAliasesAndArity(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		// #length is a true alias of #size (identical UnboundMethod record).
		{`p(Array.instance_method(:length) == Array.instance_method(:size))`, "true\n"},
		{`p([1,2,3].length)`, "3\n"},
		// Array.allocate is a real, mutable, empty Array.
		{`a = Array.allocate; p a.instance_of?(Array); p a.size; a << 1; p a`,
			"true\n0\n[1]\n"},
		// A subclass allocates an (empty) instance of the subclass.
		{`class SubA < Array; end; a = SubA.allocate; p a.class; p a.size`,
			"SubA\n0\n"},
	})
}

// TestArrayArityErrors covers the ArgumentError paths added in wave 12: #clear
// and #map take no positional arguments, and Array.allocate takes none either.
func TestArrayArityErrors(t *testing.T) {
	for _, src := range []string{
		`[1].clear(true)`,
		`[1,2,3].map(:foo)`,
		`Array.allocate(1)`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
			t.Errorf("src=%q expected ArgumentError, got %v", src, err)
		}
	}
}

// TestArrayArefArithmeticSequence covers Array#[] indexed by an
// Enumerator::ArithmeticSequence: positive and negative steps, endless and
// beginless ranges, exclusive endpoints, negative indices, an empty result, and
// the begin-past-end nil result. Every expectation matches MRI 4.0.6.
func TestArrayArefArithmeticSequence(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p([0,1,2,3,4,5][(0..).step(2)])`, "[0, 2, 4]\n"},
		{`p([0,1,2,3,4,5][(2..).step(-1)])`, "[2, 1, 0]\n"},
		{`p([0,1,2,3,4,5][(...0).step(-1)])`, "[5, 4, 3, 2, 1]\n"},
		{`p([0,1,2,3,4,5][(..0).step(-1)])`, "[5, 4, 3, 2, 1, 0]\n"},
		{`p([0,1,2,3,4,5][(1...3).step(2)])`, "[1]\n"},
		{`p([0,1,2,3,4,5][(-4..4).step(2)])`, "[2, 4]\n"},
		{`p([0,1,2,3,4,5][(1..3).step(-1)])`, "[]\n"},  // positive range, negative step
		{`p([0,1,2,3,4,5][(6..).step(1)])`, "[]\n"},    // begin == length
		{`p([0,1,2,3,4,5][(7..).step(1)])`, "nil\n"},   // begin past the end (step > 0)
		{`p([0,1,2,3,4,5][(0..8).step(-1)])`, "nil\n"}, // begin past the end (step < 0)
		{`p([0,1,2,3,4,5][(9..).step(-1)])`, "[5, 4, 3, 2, 1, 0]\n"},
		{`p([0,1,2,3,4,5][(-3..).step(-2)])`, "[3, 1]\n"},
		// A begin still out of the array after negative-index resolution is nil.
		{`p([0,1,2,3,4,5][(-9..).step(1)])`, "nil\n"},
		{`p([0,1,2,3,4,5][(..-9).step(-1)])`, "nil\n"},
	})
}

// TestArrayArefArithSeqErrors covers the RangeError guard for a strided slice
// whose declared span reaches past the array (|step| > 1) and the ArgumentError
// for a step that truncates to zero (a fractional step). Verified against MRI.
func TestArrayArefArithSeqErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`[0,1,2,3,4,5][(0..6).step(2)]`, "RangeError"},
		{`[0,1,2,3,4,5][(2..8).step(2)]`, "RangeError"},
		{`[0,1,2,3,4,5][(8..2).step(-2)]`, "RangeError"},
		{`[0,1,2,3,4,5][(7..).step(2)]`, "RangeError"},
		{`[0,1,2,3,4,5][(-9..).step(2)]`, "RangeError"}, // begin below 0 after resolution
		{`[0,1,2,3,4,5][(0..5).step(0.5)]`, "slice step cannot be zero"},
	}
	for _, c := range cases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q expected %q, got %v", c.src, c.want, err)
		}
	}
}

// TestArrayArefRangeAndIndexEdges covers the remaining Array#[] residuals: a user
// Range subclass slices like a plain Range, a Range with a length argument is a
// TypeError, and an index or length beyond the Fixnum range raises RangeError
// (while an ordinary Float index truncates). Verified against MRI 4.0.6.
func TestArrayArefRangeAndIndexEdges(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`class MyR < Range; end; p([1,2,3,4][MyR.new(1,2)])`, "[2, 3]\n"},
		{`class MyR2 < Range; end; p([1,2,3,4][MyR2.new(-3,-1,true)])`, "[2, 3]\n"},
		{`p([10,20,30][1.9])`, "20\n"}, // ordinary Float index truncates
	})
	errCases := []struct{ src, want string }{
		{`[1,2,3][1..2, 1]`, "TypeError"},
		{`[1,2,3,4,5,6][2.0**63]`, "RangeError"},
		{`[1,2,3,4,5,6][1, 8e19]`, "RangeError"},
		{`[1,2,3,4,5,6][1, -8e19]`, "RangeError"},
	}
	for _, c := range errCases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q expected %q, got %v", c.src, c.want, err)
		}
	}
}

// TestArraySumKahan covers Array#sum's exact-then-Kahan folding: an exact integer
// sum, the Kahan-compensated float sum that reduce(:+) cannot match, a Float init
// seeding the Kahan accumulator, a non-numeric run through #+, and the NaN/Infinity
// rules of kahanAdd. Verified against MRI 4.0.6.
func TestArraySumKahan(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p([1,2,3].sum)`, "6\n"},
		{`p([1,2,3].sum(10))`, "16\n"},
		{`p([1,2,3].sum { |i| i * 10 })`, "60\n"},
		{`p([].sum(0.0))`, "0.0\n"},
		// Kahan compensation: reduce(:+) drifts to 50.00000000000001, sum is exact.
		{`floats = [2.7800000000000002, 5.0, 2.5, 4.44, 3.89, 3.89, 4.44, 7.78, 5.0, 2.7800000000000002, 5.0, 2.5]; p(floats.sum)`,
			"50.0\n"},
		{`p([1, 2.5, 3].sum)`, "6.5\n"},           // exact -> float switch mid-array
		{`p(["a","b","c"].sum(""))`, "\"abc\"\n"}, // non-numeric fold through #+
		// NaN / Infinity handling (kahanAdd branches).
		{`p([1.0, Float::INFINITY].sum)`, "Infinity\n"},
		{`p([Float::INFINITY, 1.0].sum)`, "Infinity\n"},
		{`p([1.0, -Float::INFINITY].sum)`, "-Infinity\n"},
		{`p([1.0, Float::NAN].sum.nan?)`, "true\n"},
		{`p([Float::NAN, 1.0].sum.nan?)`, "true\n"},
		{`p([Float::INFINITY, -Float::INFINITY].sum.nan?)`, "true\n"},
		{`p([Float::INFINITY, Float::INFINITY].sum)`, "Infinity\n"},
		// Live-index loop folds a block-appended tail (integers stay exact).
		{`a=[1,2]; i=0; r=a.sum{|e| a<<3 if i<1; i+=1; e}; p r`, "6\n"},
	})
	// A Float appearing after a non-numeric accumulator, and a non-numeric element
	// after a float run, both raise TypeError (and exercise the fall-through paths).
	for _, src := range []string{`[1.0].sum("")`, `[1.0, "x"].sum`} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "TypeError") {
			t.Errorf("src=%q expected TypeError, got %v", src, err)
		}
	}
}

// TestArrayIndexAndFindIndex covers Array#index becoming a full #find_index (value,
// block and no-arg Enumerator forms) and a true alias of #find_index (shared method
// record), matching MRI 4.0.6.
func TestArrayIndexAndFindIndex(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p([1,2,3,2].index(2))`, "1\n"},
		{`p([1,2,3,2].index { |x| x > 1 })`, "1\n"},
		{`p([1,2,3].index(9))`, "nil\n"},
		{`p([1,2,3].index.class)`, "Enumerator\n"},
		{`p([1,2,3].find_index { |x| x == 3 })`, "2\n"},
		{`p(Array.instance_method(:index) == Array.instance_method(:find_index))`, "true\n"},
	})
	if err := runErr(t, `[1,2,3].index(1, 2)`); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
		t.Errorf("index with 2 args: expected ArgumentError, got %v", err)
	}
}

// TestArrayPermutationLazyEnumerator covers Array#permutation returning a lazy
// Enumerator that re-reads self when iterated (so a later mutation is seen) and
// reports the descending-factorial #size. Verified against MRI 4.0.6.
func TestArrayPermutationLazyEnumerator(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`n=[1,2]; e=n.permutation; n<<3; p e.to_a.sort`,
			"[[1, 2, 3], [1, 3, 2], [2, 1, 3], [2, 3, 1], [3, 1, 2], [3, 2, 1]]\n"},
		{`p([1,2,3].permutation.size)`, "6\n"},
		{`p([1,2,3].permutation(2).size)`, "6\n"},
		{`p([1,2,3].permutation(0).size)`, "1\n"},
		{`p([1,2,3].permutation(5).size)`, "0\n"}, // k > n
		{`p([].permutation.size)`, "1\n"},
		{`p([1,2,3].permutation(2).to_a.length)`, "6\n"},
		{`p([1,2,3].permutation(5).to_a)`, "[]\n"}, // k > n yields nothing
	})
}

// TestPackUnicode covers Array#pack('U') across every codepoint width of MRI's
// extended UTF-8 (1..6 bytes, including values above U+10FFFF that Go's own
// encoder would clamp), the count/'*' modifiers, and #to_int coercion of the
// argument. Every expectation was checked against MRI 4.0.6.
func TestPackUnicode(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p([0x41].pack("U").bytes)`, "[65]\n"},
		{`p([0x85].pack("U").bytes)`, "[194, 133]\n"},
		{`p([0x3042].pack("U").bytes)`, "[227, 129, 130]\n"},
		{`p([0x110000].pack("U").bytes)`, "[244, 144, 128, 128]\n"},
		{`p([0x200000].pack("U").bytes)`, "[248, 136, 128, 128, 128]\n"},
		{`p([0x7FFFFFFF].pack("U").bytes)`, "[253, 191, 191, 191, 191, 191]\n"},
		{`p([65,66].pack("U*").bytes)`, "[65, 66]\n"},
		{`p([65,66,67].pack("U2").bytes)`, "[65, 66]\n"},
		{`p([65].pack("U").encoding.to_s)`, "\"UTF-8\"\n"},
		// #to_int coerces the argument.
		{`o=Object.new; def o.to_int; 66; end; p([o].pack("U").bytes)`, "[66]\n"},
	})
	// Out-of-range and bad-coercion errors.
	errCases := []struct{ src, want string }{
		{`[-1].pack("U")`, "RangeError"},
		{`[2**32].pack("U")`, "RangeError"}, // fits int64, exceeds 0x7FFFFFFF
		{`[2**64].pack("U")`, "RangeError"}, // Bignum
		{`o=Object.new; def o.to_int; "5"; end; [o].pack("U")`, "TypeError"},
		{`[Object.new].pack("U")`, "no implicit conversion of Object into Integer"},
	}
	for _, c := range errCases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q expected %q, got %v", c.src, c.want, err)
		}
	}
}

// TestPackStringNil covers a/A/Z packing a nil argument as an empty string
// (padded with spaces for A, NULs for a/Z), matching MRI 4.0.6.
func TestPackStringNil(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p([nil].pack("A3"))`, "\"   \"\n"},
		{`p([nil].pack("a3"))`, "\"\\x00\\x00\\x00\"\n"},
		{`p([nil].pack("Z3"))`, "\"\\x00\\x00\\x00\"\n"},
		{`p([nil].pack("a*"))`, "\"\"\n"},
	})
}

// TestPackBuffer covers Array#pack's :buffer option: appending to the buffer's
// content, absolute repositioning with @, the buffer being the returned object,
// encoding preservation, and the TypeError / FrozenError guards. Verified against
// MRI 4.0.6.
func TestPackBuffer(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`b=+"123"; r=[65,66,67].pack("ccc", buffer: b); p r; p r.equal?(b)`, "\"123ABC\"\ntrue\n"},
		{`p([65,66,67].pack("@3ccc", buffer: +"123456"))`, "\"123ABC\"\n"},
		{`p([65,66,67].pack("@6ccc", buffer: +"123"))`, "\"123\\u0000\\u0000\\u0000ABC\"\n"},
		{`p([65,66,67].pack("@3ccc", buffer: +"1234567890"))`, "\"123ABC\"\n"},
		{`buf=''.encode(Encoding::ISO_8859_1); [65].pack("c", buffer: buf); p buf.encoding.to_s`,
			"\"ISO-8859-1\"\n"},
		// A trailing keyword hash without :buffer is ignored (no buffer redirection).
		{`p([65].pack("C", other: 1))`, "\"A\"\n"},
	})
	errCases := []struct{ src, want string }{
		{`[65].pack("ccc", buffer: [])`, "buffer must be String, not Array"},
		{`[65].pack("c", buffer: "x".freeze)`, "FrozenError"},
	}
	for _, c := range errCases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q expected %q, got %v", c.src, c.want, err)
		}
	}
}

// TestPackPointer covers the P/p (pointer) directives: pack emits a native-width
// word and unpack recovers the registered string (P by leading count, p whole);
// nil packs a null pointer that unpacks to nil, and reads it as integer 0.
// Verified against MRI 4.0.6.
func TestPackPointer(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p(["hello"].pack("P").size == [0].pack("J").size)`, "true\n"},
		{`p(["hello"].pack("P").unpack("P5"))`, "[\"hello\"]\n"},
		{`p(["hello"].pack("p").unpack("p"))`, "[\"hello\"]\n"},
		{`p(["hello"].pack("P").unpack("P3"))`, "[\"hel\"]\n"}, // P count truncation
		{`p([nil].pack("P").unpack("J"))`, "[0]\n"},
		{`p([nil].pack("P").unpack("P5"))`, "[nil]\n"},      // null pointer unpacks to nil
		{`p("".unpack("P"))`, "[]\n"},                       // too few bytes -> no element
		{`p(["hi"].pack("P").unpack("P10"))`, "[\"hi\"]\n"}, // count clamped to length
	})
}

// TestArrayEnumeratorUnknownSize covers the wave-12 fix that gives the block-less
// #bsearch / #bsearch_index / #rindex Enumerators an unknown (nil) size, as MRI
// does, while the ordinary #map Enumerator keeps its known size.
func TestArrayEnumeratorUnknownSize(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p([1,2,3].bsearch.size)`, "nil\n"},
		{`p([1,2,3].bsearch_index.size)`, "nil\n"},
		{`p([1,2,3].rindex.size)`, "nil\n"},
		{`p([1,2,3].map.size)`, "3\n"},
		// The unsized Enumerators still drive their search when iterated.
		{`p([1,2,3,4].bsearch.each { |x| x >= 3 })`, "3\n"},
		{`p([1,2,3,4].bsearch_index.each { |x| x >= 3 })`, "2\n"},
	})
}
