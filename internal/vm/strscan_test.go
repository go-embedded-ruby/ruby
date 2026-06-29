// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestStrscanLibrarySurface drives the native StringScanner binding
// (internal/vm/strscan.go, backed by github.com/go-ruby-strscan/strscan) across
// its whole Ruby surface, asserting each method matches MRI 4.0.5. Every case
// was checked against `ruby -rstrscan`. Together with TestStrscanErrors and
// TestStrscanInspect below it covers every method binding and the
// nil / IndexError / RangeError / Error branches.
func TestStrscanLibrarySurface(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// scan / scan_until / check / check_until: text or nil; advancing vs not.
		{"scan", `require "strscan"; s=StringScanner.new("hi there"); p s.scan(/\w+/); p s.pos; p s.scan(/\w+/)`, "\"hi\"\n2\nnil\n"},
		{"scan_until", `require "strscan"; s=StringScanner.new("xx99"); p s.scan_until(/\d+/); p s.matched; p s.pos`, "\"xx99\"\n\"99\"\n4\n"},
		{"scan_until_miss", `require "strscan"; s=StringScanner.new("abc"); p s.scan_until(/z/); p s.pos`, "nil\n0\n"},
		{"check", `require "strscan"; s=StringScanner.new("abc"); p s.check(/ab/); p s.pos; p s.check(/zz/)`, "\"ab\"\n0\nnil\n"},
		{"check_until", `require "strscan"; s=StringScanner.new("a-b-c"); p s.check_until(/b/); p s.pos; p s.check_until(/z/)`, "\"a-b\"\n0\nnil\n"},
		// skip / skip_until / match? / matched_size: lengths or nil.
		{"skip", `require "strscan"; s=StringScanner.new("  ab"); p s.skip(/\s+/); p s.pos; p s.skip(/\s+/)`, "2\n2\nnil\n"},
		{"skip_until", `require "strscan"; s=StringScanner.new("ab99"); p s.skip_until(/\d+/); p s.pos; p s.skip_until(/z/)`, "4\n4\nnil\n"},
		{"match?", `require "strscan"; s=StringScanner.new("abcd"); p s.match?(/ab/); p s.pos; p s.match?(/zz/)`, "2\n0\nnil\n"},
		{"matched_size", `require "strscan"; s=StringScanner.new("abc"); s.scan(/ab/); p s.matched_size; s.scan(/z/); p s.matched_size`, "2\nnil\n"},
		// matched / matched? / pre_match / post_match.
		{"matched_q", `require "strscan"; s=StringScanner.new("ab"); p s.matched?; s.scan(/a/); p s.matched?; p s.matched`, "false\ntrue\n\"a\"\n"},
		{"pre_post_match", `require "strscan"; s=StringScanner.new("xxYzz"); s.scan_until(/Y/); p s.pre_match; p s.post_match`, "\"xx\"\n\"zz\"\n"},
		{"post_match_none", `require "strscan"; s=StringScanner.new("ab"); p s.pre_match; p s.post_match`, "nil\nnil\n"},
		// getch / peek.
		{"getch", `require "strscan"; s=StringScanner.new("ab"); p s.getch; p s.getch; p s.getch`, "\"a\"\n\"b\"\nnil\n"},
		{"peek", `require "strscan"; s=StringScanner.new("abcd"); p s.peek(2); p s.pos; p s.peek(9)`, "\"ab\"\n0\n\"abcd\"\n"},
		// [] with index, named group (Symbol and String), getch[0], out of range.
		{"index_group", `require "strscan"; s=StringScanner.new("foo bar"); s.scan(/(\w+)(\s*)/); p s[0]; p s[1]; p s[2]; p s[5]`, "\"foo \"\n\"foo\"\n\" \"\nnil\n"},
		{"named_group", `require "strscan"; s=StringScanner.new("Dec"); s.scan(/(?<m>\w+)/); p s[:m]; p s["m"]`, "\"Dec\"\n\"Dec\"\n"},
		{"getch_index", `require "strscan"; s=StringScanner.new("xy"); s.getch; p s[0]; p s[1]`, "\"x\"\nnil\n"},
		{"index_no_match", `require "strscan"; s=StringScanner.new("zz"); p s.scan(/q/); p s[1]`, "nil\nnil\n"},
		// pos / pos= (including negative) / charpos / pointer alias.
		{"pos_set", `require "strscan"; s=StringScanner.new("abcd"); s.pos=2; p s.rest; s.pos=-1; p s.rest`, "\"cd\"\n\"d\"\n"},
		{"charpos", `require "strscan"; s=StringScanner.new("héllo"); s.scan(/hé/); p s.pos; p s.charpos`, "3\n2\n"},
		{"pointer", `require "strscan"; s=StringScanner.new("abcd"); s.pointer=2; p s.pointer`, "2\n"},
		// rest / rest_size / eos? / empty? / beginning_of_line? / bol?.
		{"rest", `require "strscan"; s=StringScanner.new("hi"); s.scan(/h/); p s.rest; p s.rest_size; p s.string; p s.eos?`, "\"i\"\n1\n\"hi\"\nfalse\n"},
		{"eos_empty", `require "strscan"; p StringScanner.new("").eos?; p StringScanner.new("").empty?`, "true\ntrue\n"},
		{"bol", `require "strscan"; s=StringScanner.new("a\nb"); p s.beginning_of_line?; s.scan(/a\n/); p s.bol?`, "true\ntrue\n"},
		// string= / << / concat / terminate / reset / clear.
		{"string_set", `require "strscan"; s=StringScanner.new("ab"); s.string="XY"; p s.scan(/X/); p s.pos`, "\"X\"\n1\n"},
		{"concat", `require "strscan"; s=StringScanner.new("ab"); s<<"cd"; s.concat("ef"); p s.string`, "\"abcdef\"\n"},
		{"terminate_reset", `require "strscan"; s=StringScanner.new("abc"); s.terminate; p s.eos?; s.reset; p s.pos`, "true\n0\n"},
		{"clear", `require "strscan"; s=StringScanner.new("abc"); s.scan(/a/); s.clear; p s.pos`, "0\n"},
		// unscan undoes the last advancing match.
		{"unscan", `require "strscan"; s=StringScanner.new("abc"); s.scan(/a/); s.unscan; p s.pos`, "0\n"},
		// require returns false on the second load.
		{"require_again", `require "strscan"; p require("strscan")`, "false\n"},
		// pattern given as a String matches literally (regex metacharacters escaped).
		{"string_pattern", `require "strscan"; s=StringScanner.new("a.b"); p s.scan("a."); p s.pos`, "\"a.\"\n2\n"},
		{"string_pattern_skip", `require "strscan"; s=StringScanner.new("a+b"); p s.skip("a+")`, "2\n"},
		// a pattern object answering #to_str is coerced like a String.
		{"to_str_pattern", `require "strscan"; o=Object.new; def o.to_str; "ab"; end; p StringScanner.new("abc").scan(o)`, "\"ab\"\n"},
		// Regexp flags carry through (case-insensitive scan).
		{"regexp_flags", `require "strscan"; s=StringScanner.new("ABC"); p s.scan(/abc/i)`, "\"ABC\"\n"},
		// new accepts the optional trailing (fixed_anchor) argument; the empty
		// string is a valid (immediately-eos) scanner.
		{"new_two_args", `require "strscan"; p StringScanner.new("ab", true).string`, "\"ab\"\n"},
		{"new_empty", `require "strscan"; p StringScanner.new("").string`, "\"\"\n"},
		// truthiness: a scanner is always truthy.
		{"truthy", `require "strscan"; p(StringScanner.new("x") ? :yes : :no)`, ":yes\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("got=%q want=%q", got, c.want)
			}
		})
	}
}

// TestStrscanErrors covers the exception branches of the binding: #pos= out of
// range (RangeError), #unscan with nothing to undo (StringScanner::Error,
// which is a StandardError), #[] with an undefined group name (IndexError) vs an
// out-of-range integer index (nil), and a non-string-like pattern (TypeError).
func TestStrscanErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"pos_range", `require "strscan"; s=StringScanner.new("ab"); begin; s.pos=99; rescue => e; p e.class; p e.message; end`, "RangeError\n\"index out of range\"\n"},
		{"pointer_range", `require "strscan"; s=StringScanner.new("ab"); begin; s.pointer=-99; rescue RangeError; p :range; end`, ":range\n"},
		{"unscan_fail", `require "strscan"; s=StringScanner.new("ab"); begin; s.unscan; rescue StringScanner::Error => e; p e.class; p(e.is_a?(StandardError)); end`, "StringScanner::Error\ntrue\n"},
		{"unscan_after_miss", `require "strscan"; s=StringScanner.new("ab"); s.scan(/z/); begin; s.unscan; rescue StringScanner::Error; p :err; end`, ":err\n"},
		{"named_index_error", `require "strscan"; s=StringScanner.new("ab"); s.scan(/a/); begin; p s[:nope]; rescue IndexError => e; p e.class; end`, "IndexError\n"},
		{"string_index_error", `require "strscan"; s=StringScanner.new("ab"); s.scan(/(?<g>a)/); p s["g"]; begin; s["bad"]; rescue IndexError; p :idx; end`, "\"a\"\n:idx\n"},
		{"int_index_nil", `require "strscan"; s=StringScanner.new("ab"); s.scan(/a/); p s[7]`, "nil\n"},
		{"bad_index_type", `require "strscan"; s=StringScanner.new("ab"); s.scan(/a/); begin; s[1.5]; rescue TypeError; p :type; end`, ":type\n"},
		{"bad_pattern", `require "strscan"; begin; StringScanner.new("x").scan(123); rescue TypeError => e; p e.class; end`, "TypeError\n"},
		{"new_no_args", `require "strscan"; begin; StringScanner.new; rescue ArgumentError => e; p e.message; end`, "\"wrong number of arguments (given 0, expected 1..2)\"\n"},
		{"new_too_many", `require "strscan"; begin; StringScanner.new("a", 1, 2); rescue ArgumentError; p :arg; end`, ":arg\n"},
		{"new_nil", `require "strscan"; begin; StringScanner.new(nil); rescue TypeError; p :type; end`, ":type\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("got=%q want=%q", got, c.want)
			}
		})
	}
}

// TestStrscanInspect covers StringScanner#inspect / #to_s (ToS) across every
// window arm: the start (no pre window), an interior position with short and
// elided pre/post windows, and the end-of-string "fin" form — each verified
// against MRI 4.0.5's exact byte output.
func TestStrscanInspect(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"start", `require "strscan"; p StringScanner.new("abcdefgh").inspect`, "\"#<StringScanner 0/8 @ \\\"abcde...\\\">\"\n"},
		{"post_exact5", `require "strscan"; p StringScanner.new("ABCDE").inspect`, "\"#<StringScanner 0/5 @ \\\"ABCDE\\\">\"\n"},
		{"interior", `require "strscan"; s=StringScanner.new("0123456789X"); s.pos=5; p s.inspect`, "\"#<StringScanner 5/11 \\\"01234\\\" @ \\\"56789...\\\">\"\n"},
		{"pre_elided", `require "strscan"; s=StringScanner.new("0123456789X"); s.pos=6; p s.inspect`, "\"#<StringScanner 6/11 \\\"...12345\\\" @ \\\"6789X\\\">\"\n"},
		{"to_s", `require "strscan"; s=StringScanner.new("0123456789X"); s.pos=5; p s.to_s`, "\"#<StringScanner 5/11 \\\"01234\\\" @ \\\"56789...\\\">\"\n"},
		{"fin", `require "strscan"; s=StringScanner.new("ab"); s.terminate; p s.inspect`, "\"#<StringScanner fin>\"\n"},
		{"fin_empty", `require "strscan"; p StringScanner.new("").inspect`, "\"#<StringScanner fin>\"\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("got=%q want=%q", got, c.want)
			}
		})
	}
}
