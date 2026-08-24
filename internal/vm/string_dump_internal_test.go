// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringDump covers String#dump: the receiver wrapped in double quotes with
// non-printable bytes, quotes, backslashes, and interpolation sigils escaped so
// the result is a Ruby literal reproducing the receiver. It exercises the named
// escapes, the \# sigil guard, printable ASCII passthrough, single-byte \xHH
// controls (in both binary and UTF-8 strings), UTF-8 multibyte \uXXXX / \u{…}
// escaping, and the result's encoding. Verified against ruby 4.0.6.
func TestStringDump(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "hello".dump`, `"\"hello\""`},
		{`p "".dump`, `"\"\""`},
		// Named escapes for the control characters that have them.
		{`p "\a\b\t\n\v\f\r\e".dump`, `"\"\\a\\b\\t\\n\\v\\f\\r\\e\""`},
		// Quote and backslash double up.
		{`p "\"\\".dump`, `"\"\\\"\\\\\""`},
		// `#` is escaped only before an interpolation sigil ($, @, {). Single-quoted
		// Ruby literals keep the sigils intact for dump to escape.
		{`p '#{a}'.dump`, `"\"\\\#{a}\""`},
		{`p '#$x'.dump`, `"\"\\\#$x\""`},
		{`p '#@y'.dump`, `"\"\\\#@y\""`},
		{`p "#1 # #a".dump`, `"\"#1 # #a\""`},
		// Single-byte controls escape as \xHH (upper-case), in any encoding.
		{`p 0.chr.dump`, `"\"\\x00\""`},
		{`p 0x1c.chr.dump`, `"\"\\x1C\""`},
		{`p 0x7f.chr.dump`, `"\"\\x7F\""`},
		// A high byte of a binary string escapes as \xHH.
		{`p 0x80.chr.dump`, `"\"\\x80\""`},
		{`p 0xff.chr.dump`, `"\"\\xFF\""`},
		// A UTF-8 single-byte control still uses \xHH (unlike #inspect's \uXXXX).
		{`p 0.chr('utf-8').dump`, `"\"\\x00\""`},
		{`p 0x7f.chr('utf-8').dump`, `"\"\\x7F\""`},
		// UTF-8 multibyte: \uXXXX in the BMP, \u{…} above it.
		{`p 0x80.chr('utf-8').dump`, `"\"\\u0080\""`},
		{`p 0xFFFF.chr('utf-8').dump`, `"\"\\uFFFF\""`},
		{`p 0x10000.chr('utf-8').dump`, `"\"\\u{10000}\""`},
		{`p 0x10FFFF.chr('utf-8').dump`, `"\"\\u{10FFFF}\""`},
		{`p "é".dump`, `"\"\\u00E9\""`},
		// dump ignores frozen and returns a plain String instance.
		{`p "foo".freeze.dump.frozen?`, `false`},
		{`p "foo".dump.instance_of?(String)`, `true`},
		// The result carries the receiver's (ASCII-compatible) encoding.
		{`p "foo".encode("ISO-8859-1").dump.encoding.name`, `"ISO-8859-1"`},
		{`p 1.chr.dump.encoding.name`, `"US-ASCII"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
