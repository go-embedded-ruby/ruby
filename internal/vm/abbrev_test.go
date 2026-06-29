// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestAbbrev covers the Abbrev module and the Array#abbrev core extension backed
// by github.com/go-ruby-abbrev/abbrev (the MRI-4.0.5 faithful port): the plain
// abbreviation table, the String-prefix filter, the Regexp-pattern filter (the
// host-side path), duplicate and empty words, and the MRI Hash insertion order —
// every value asserted against MRI 4.0.5's stdlib Abbrev.
func TestAbbrev(t *testing.T) {
	cases := []struct{ src, want string }{
		// Abbrev.abbrev / Array#abbrev — the plain table, MRI insertion order.
		{`require "abbrev"; p Abbrev.abbrev(["ruby","rules"])`,
			`{"ruby" => "ruby", "rub" => "ruby", "rules" => "rules", "rule" => "rules", "rul" => "rules"}` + "\n"},
		{`require "abbrev"; p ["car","cone"].abbrev`,
			`{"car" => "car", "ca" => "car", "cone" => "cone", "con" => "cone", "co" => "cone"}` + "\n"},
		{`require "abbrev"; p %w{ summer winter }.abbrev`,
			`{"summer" => "summer", "summe" => "summer", "summ" => "summer", "sum" => "summer", "su" => "summer", "s" => "summer", "winter" => "winter", "winte" => "winter", "wint" => "winter", "win" => "winter", "wi" => "winter", "w" => "winter"}` + "\n"},
		// String-prefix pattern — only words starting with the prefix, abbreviations
		// at least as long as it.
		{`require "abbrev"; p Abbrev.abbrev(["ruby","rules"], "ru")`,
			`{"ruby" => "ruby", "rub" => "ruby", "rules" => "rules", "rule" => "rules", "rul" => "rules"}` + "\n"},
		{`require "abbrev"; p Abbrev.abbrev(%w{car box cone}, "ca")`,
			`{"car" => "car", "ca" => "car"}` + "\n"},
		{`require "abbrev"; p Abbrev.abbrev(["ruby","rules"], "x")`, "{}\n"},
		// Regexp pattern — the host-side path: the pattern filters abbreviations,
		// not just whole words (crab keeps only the full word under /b/).
		{`require "abbrev"; p Abbrev.abbrev(%w{car box cone crab}, /b/)`,
			`{"box" => "box", "bo" => "box", "b" => "box", "crab" => "crab"}` + "\n"},
		{`require "abbrev"; p %w{ fast boat day }.abbrev(/^.a/)`,
			`{"fast" => "fast", "fas" => "fast", "fa" => "fast", "day" => "day", "da" => "day"}` + "\n"},
		{`require "abbrev"; p %w{car box cone}.abbrev(/zzz/)`, "{}\n"},
		// Regexp path with shared prefixes: the seen-counter deletes on the second
		// sight and breaks on the third, so only the full words survive.
		{`require "abbrev"; p Abbrev.abbrev(["abx","aby","abz"], /a/)`,
			`{"abx" => "abx", "aby" => "aby", "abz" => "abz"}` + "\n"},
		// Regexp path where a kept abbreviation is also a full word ("ab"): pass 2
		// re-asserts it in place (the keep-position branch).
		{`require "abbrev"; p Abbrev.abbrev(["ab","abc"], /a/)`,
			`{"abc" => "abc", "ab" => "ab"}` + "\n"},
		{`require "abbrev"; p Abbrev.abbrev(["ab","abc"], /b/)`,
			`{"abc" => "abc", "ab" => "ab"}` + "\n"},
		// Array#abbrev with an explicit nil pattern (the default arg path).
		{`require "abbrev"; p ["ruby","rules"].abbrev(nil)`,
			`{"ruby" => "ruby", "rub" => "ruby", "rules" => "rules", "rule" => "rules", "rul" => "rules"}` + "\n"},
		// Duplicate words — MRI's seen-counter reorders ("ab","a").
		{`require "abbrev"; p Abbrev.abbrev(["a","a","ab"])`,
			`{"ab" => "ab", "a" => "a"}` + "\n"},
		{`require "abbrev"; p Abbrev.abbrev(["ruby","ruby"])`, `{"ruby" => "ruby"}` + "\n"},
		// Nested-prefix words.
		{`require "abbrev"; p Abbrev.abbrev(["a","ab","abc"])`,
			`{"abc" => "abc", "a" => "a", "ab" => "ab"}` + "\n"},
		// Empty input and an empty word.
		{`require "abbrev"; p Abbrev.abbrev([])`, "{}\n"},
		{`require "abbrev"; p [].abbrev`, "{}\n"},
		// An empty word: pass 1 skips it, but pass 2 still inserts "" => "" (MRI).
		{`require "abbrev"; p Abbrev.abbrev([""])`, `{"" => ""}` + "\n"},
		{`require "abbrev"; p Abbrev.abbrev(["", "a"])`, `{"a" => "a", "" => ""}` + "\n"},
		// An empty word on the Regexp path: pass 1 skips it (the pattern table's
		// empty-word guard), pass 2 omits "" since /a/ does not match it (MRI).
		{`require "abbrev"; p Abbrev.abbrev(["", "ab"], /a/)`, `{"ab" => "ab", "a" => "ab"}` + "\n"},
		// Multibyte words exercise the rune-aware prefix walk.
		{`require "abbrev"; p Abbrev.abbrev(["café"])`,
			`{"café" => "café", "caf" => "café", "ca" => "café", "c" => "café"}` + "\n"},
		// defined? after require; require returns true once then false.
		{`require "abbrev"; p defined?(Abbrev)`, "\"constant\"\n"},
		{`p require("abbrev"); p require("abbrev")`, "true\nfalse\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestAbbrevLazyLoad asserts the abbrev surface is absent until `require
// "abbrev"`, matching MRI's lazy lib/abbrev.rb load.
func TestAbbrevLazyLoad(t *testing.T) {
	if got := eval(t, `p defined?(Abbrev)`); got != "nil\n" {
		t.Errorf("Abbrev should be undefined before require, got %q", got)
	}
	if got := eval(t, `p [].respond_to?(:abbrev)`); got != "false\n" {
		t.Errorf("Array#abbrev should be absent before require, got %q", got)
	}
}

// TestAbbrevErrors covers the error branches: a non-Array words argument and a
// non-String word element both raise TypeError (rbgo's coercion convention; MRI
// raises NoMethodError from the underlying #each / #empty? call).
func TestAbbrevErrors(t *testing.T) {
	cases := []struct {
		src, class, msg string
	}{
		{`require "abbrev"; Abbrev.abbrev(5)`, "TypeError", "no implicit conversion of 5 into Array"},
		{`require "abbrev"; Abbrev.abbrev([1, 2])`, "TypeError", "no implicit conversion of 1 into String"},
		{`require "abbrev"; Abbrev.abbrev(["ok", :sym])`, "TypeError", "no implicit conversion of :sym into String"},
	}
	for _, tc := range cases {
		class, msg := evalErr(t, tc.src)
		if class != tc.class || msg != tc.msg {
			t.Errorf("src=%q got=%s:%q want=%s:%q", tc.src, class, msg, tc.class, tc.msg)
		}
	}
}
