// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestDidYouMean covers DidYouMean::SpellChecker backed by
// github.com/go-ruby-did-you-mean/did-you-mean (the MRI-4.0.5 faithful port of
// the spell-suggestion matcher): the ranked #correct output for a String
// dictionary, the original-type round-trip for Symbol and Integer dictionaries,
// the empty no-match result, and the require/defined? surface — every value
// asserted against MRI 4.0.5's stdlib did_you_mean.
func TestDidYouMean(t *testing.T) {
	cases := []struct{ src, want string }{
		// String dictionary: the ranked matches come back as Strings.
		{`require "did_you_mean"; p DidYouMean::SpellChecker.new(dictionary: ["foo","food","bar"]).correct("doo")`, "[\"foo\"]\n"},
		// Symbol dictionary: the matches come back as Symbols (original type).
		{`require "did_you_mean"; p DidYouMean::SpellChecker.new(dictionary: [:foo,:food,:bar]).correct(:doo)`, "[:foo]\n"},
		// A Symbol dictionary with a String input still yields Symbols.
		{`require "did_you_mean"; p DidYouMean::SpellChecker.new(dictionary: [:foo]).correct("doo")`, "[:foo]\n"},
		// Integer dictionary: the matches come back as Integers (the dictName/byName
		// default path — entries that are neither String nor Symbol).
		{`require "did_you_mean"; p DidYouMean::SpellChecker.new(dictionary: [1, 12, 2]).correct("1")`, "[12]\n"},
		// No close-enough entry yields the empty Array.
		{`require "did_you_mean"; p DidYouMean::SpellChecker.new(dictionary: ["foo"]).correct("zzzzz")`, "[]\n"},
		// An empty dictionary always returns [].
		{`require "did_you_mean"; p DidYouMean::SpellChecker.new(dictionary: []).correct("x")`, "[]\n"},
		// defined? after require; the module and the nested class both resolve.
		{`require "did_you_mean"; p defined?(DidYouMean)`, "\"constant\"\n"},
		{`require "did_you_mean"; p defined?(DidYouMean::SpellChecker)`, "\"constant\"\n"},
		// require returns true the first time and false thereafter (a normal load).
		{`p require("did_you_mean"); p require("did_you_mean")`, "true\nfalse\n"},
		// The wrapper's inspect/to_s rendering, and its truthiness (always truthy, like
		// any non-nil/non-false object).
		{`require "did_you_mean"; p DidYouMean::SpellChecker.new(dictionary: []).to_s`, "\"#<DidYouMean::SpellChecker>\"\n"},
		{`require "did_you_mean"; p DidYouMean::SpellChecker.new(dictionary: [])`, "#<DidYouMean::SpellChecker>\n"},
		{`require "did_you_mean"; p(DidYouMean::SpellChecker.new(dictionary: []) ? :yes : :no)`, ":yes\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestDidYouMeanErrors covers the constructor's error branches: a missing
// :dictionary keyword and a bare call both raise ArgumentError with MRI's exact
// messages; a positional argument is the arity error; a non-Array dictionary is
// a TypeError — each asserted against MRI 4.0.5.
func TestDidYouMeanErrors(t *testing.T) {
	cases := []struct {
		src, class, msg string
	}{
		{`require "did_you_mean"; DidYouMean::SpellChecker.new`, "ArgumentError", "missing keyword: :dictionary"},
		{`require "did_you_mean"; DidYouMean::SpellChecker.new(other: 1)`, "ArgumentError", "missing keyword: :dictionary"},
		{`require "did_you_mean"; DidYouMean::SpellChecker.new("x")`, "ArgumentError", "wrong number of arguments (given 1, expected 0; required keyword: dictionary)"},
		{`require "did_you_mean"; DidYouMean::SpellChecker.new(dictionary: 5)`, "TypeError", "no implicit conversion of 5 into Array"},
	}
	for _, tc := range cases {
		class, msg := evalErr(t, tc.src)
		if class != tc.class || msg != tc.msg {
			t.Errorf("src=%q got=%s:%q want=%s:%q", tc.src, class, msg, tc.class, tc.msg)
		}
	}
}
