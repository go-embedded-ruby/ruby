// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringSearchEncoding covers the encoding-compatibility check shared by the
// String search methods (#index, #rindex, #include?, #start_with?, #end_with?,
// #byteindex, #byterindex) and String#[]= value assignment: an ASCII-compatible
// pattern/replacement works, while an incompatible one raises
// Encoding::CompatibilityError. Verified against ruby 4.0.6.
func TestStringSearchEncoding(t *testing.T) {
	// Ordinary (compatible) uses keep working.
	okCases := []struct{ src, want string }{
		{`p "hello".index("ll")`, `2`},
		{`p "hello".rindex("l")`, `3`},
		{`p "hello".include?("ell")`, `true`},
		{`p "hello".start_with?("he")`, `true`},
		{`p "hello".end_with?("lo")`, `true`},
		{`p "hello".byteindex("ll")`, `2`},
		{`p :foo.start_with?("f")`, `true`},         // Symbol#start_with? is unaffected
		{`s = "hello"; s[0] = "X"; p s`, `"Xello"`}, // plain []= still works
	}
	for _, c := range okCases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// An incompatible pattern/replacement encoding raises Encoding::CompatibilityError.
	incompat := []string{
		`"あれ".index("れ".encode(Encoding::EUC_JP))`,
		`"あれ".rindex("れ".encode(Encoding::EUC_JP))`,
		`"あれ".include?("れ".encode(Encoding::EUC_JP))`,
		`"あれ".start_with?("れ".encode(Encoding::EUC_JP))`,
		`"あれ".end_with?("れ".encode(Encoding::EUC_JP))`,
		`"あれ".byteindex("れ".encode(Encoding::EUC_JP))`,
		`"あれ".byterindex("れ".encode(Encoding::EUC_JP))`,
		`s = "あれ".dup; s[0] = "が".encode(Encoding::EUC_JP)`,
		`s = "あれ".dup; s["れ"] = "が".encode(Encoding::EUC_JP)`,
	}
	for _, src := range incompat {
		if got := eval(t, `p ((`+src+`; :no) rescue $!.class)`); got != "Encoding::CompatibilityError\n" {
			t.Errorf("%s: got=%q want Encoding::CompatibilityError", src, got)
		}
	}
	// A non-String search argument still raises TypeError (strPatternCompat's
	// coercion fallback).
	if got := eval(t, `p (("hello".include?(5); :no) rescue $!.class)`); got != "TypeError\n" {
		t.Errorf("include? non-String: got=%q want TypeError", got)
	}
}
