// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestIntegerChr covers Integer#chr: the no-argument form (US-ASCII for 0..127,
// ASCII-8BIT for 128..255) and the encoding-argument form, which encodes the
// receiver as a codepoint (UTF-8 multibyte, or a single byte for a single-byte
// encoding). Verified against ruby 4.0.6.
func TestIntegerChr(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 65.chr`, `"A"`},
		{`p 0.chr.encoding.to_s`, `"US-ASCII"`},
		{`p 127.chr.encoding.to_s`, `"US-ASCII"`},
		{`p 128.chr.encoding.to_s`, `"ASCII-8BIT"`},
		{`p 255.chr.encoding.to_s`, `"ASCII-8BIT"`},
		{`p 0.chr.bytes`, `[0]`},
		{`p 200.chr.bytes`, `[200]`},
		// Encoding argument: UTF-8 encodes the codepoint (multibyte allowed).
		{`p 65.chr("UTF-8")`, `"A"`},
		{`p 65.chr("UTF-8").encoding.to_s`, `"UTF-8"`},
		{`p 0x100.chr("UTF-8").bytes`, `[196, 128]`},
		{`p 0x3042.chr("UTF-8").bytes`, `[227, 129, 130]`},
		// US-ASCII and a single-byte encoding hold one byte.
		{`p 100.chr("US-ASCII")`, `"d"`},
		{`p 200.chr("ASCII-8BIT").bytes`, `[200]`},
		{`p 200.chr(Encoding::ISO_8859_1).bytes`, `[200]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// RangeError cases across every branch.
	rangeErr := []string{
		`256.chr`,               // no-arg above 255
		`(-1).chr`,              // no-arg below 0
		`(2 ** 64).chr`,         // a bignum receiver
		`0x110000.chr("UTF-8")`, // above the Unicode range
		`0xD800.chr("UTF-8")`,   // a surrogate
		`128.chr("US-ASCII")`,   // above US-ASCII's range
		`256.chr("ASCII-8BIT")`, // above a single byte
	}
	for _, src := range rangeErr {
		if got := eval(t, `p ((`+src+`; :no) rescue $!.class)`); got != "RangeError\n" {
			t.Errorf("%s: got=%q want RangeError", src, got)
		}
	}
}
