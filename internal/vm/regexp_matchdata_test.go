package vm_test

import "testing"

// runCases runs a table of Ruby snippets through the eval harness, asserting the
// captured stdout. Every expectation below was checked against MRI ruby 4.0.5.
func runCases(t *testing.T, cases []struct{ src, want string }) {
	t.Helper()
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRegexpNames covers Regexp#names and Regexp#named_captures (the name→group
// indices map), including the unnamed case.
func TestRegexpNames(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p(/(?<a>x)(?<b>y)/.names)`, "[\"a\", \"b\"]\n"},
		{`p(/(x)(y)/.names)`, "[]\n"},
		{`p(/(?<a>x)(?<b>y)/.named_captures)`, "{\"a\" => [1], \"b\" => [2]}\n"},
		// Non-capturing groups do not shift the named-group indices.
		{`p(/(?:x)(?<b>y)/.named_captures)`, "{\"b\" => [1]}\n"},
		{`p(/(?<a>x)(?:m)(?<b>y)/.named_captures)`, "{\"a\" => [1], \"b\" => [2]}\n"},
		{`p(/nope/.named_captures)`, "{}\n"},
	})
}

// TestRegexpEquality covers Regexp#== / #eql? / #hash — equal source and options
// compare equal (and hash equal); a difference in flags or a non-Regexp operand
// is unequal. Both the operator and the explicit method forms are exercised.
func TestRegexpEquality(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p(/abc/ == /abc/)`, "true\n"},
		{`p(/abc/i == /abc/)`, "false\n"},
		{`p(/abc/ == /abc/i)`, "false\n"},
		{`p(/abc/ == "abc")`, "false\n"},
		{`p(/abc/.send(:==, /abc/))`, "true\n"},
		{`p(/abc/.eql?(/abc/))`, "true\n"},
		{`p(/abc/i.eql?(/abc/))`, "false\n"},
		{`p(/abc/.eql?("abc"))`, "false\n"},
		{`p(/abc/.send(:==, 5))`, "false\n"},
		{`p(/abc/.hash == /abc/.hash)`, "true\n"},
		{`p(/abc/.hash == /abc/i.hash)`, "false\n"},
		{`p(/abc/.hash.class)`, "Integer\n"},
		// The FIXEDENCODING/NOENCODING options participate in equality.
		{`p(Regexp.new("a", Regexp::FIXEDENCODING) == Regexp.new("a"))`, "false\n"},
	})
}

// TestRegexpEncoding covers Regexp#encoding and #fixed_encoding?: an ASCII source
// is US-ASCII (not fixed), a non-ASCII source is UTF-8 (fixed), and the
// FIXEDENCODING / NOENCODING options adjust both.
func TestRegexpEncoding(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p(/abc/.encoding)`, "#<Encoding:US-ASCII>\n"},
		{`p(/é/.encoding)`, "#<Encoding:UTF-8>\n"},
		{`p(/abc/.fixed_encoding?)`, "false\n"},
		{`p(/é/.fixed_encoding?)`, "true\n"},
		{`p(Regexp.new("abc", Regexp::FIXEDENCODING).encoding)`, "#<Encoding:US-ASCII>\n"},
		{`p(Regexp.new("abc", Regexp::FIXEDENCODING).fixed_encoding?)`, "true\n"},
		{`p(Regexp.new("é", Regexp::FIXEDENCODING).encoding)`, "#<Encoding:UTF-8>\n"},
		{`p(Regexp.new("abc", Regexp::NOENCODING).encoding)`, "#<Encoding:US-ASCII>\n"},
		{`p(Regexp.new("\xff".b, Regexp::NOENCODING).encoding)`, "#<Encoding:BINARY (ASCII-8BIT)>\n"},
	})
}

// TestRegexpOptionsWithEncoding covers Regexp#options including the
// FIXEDENCODING (16) and NOENCODING (32) bits, and the option constants.
func TestRegexpOptionsWithEncoding(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p Regexp::FIXEDENCODING`, "16\n"},
		{`p Regexp::NOENCODING`, "32\n"},
		{`p(Regexp.new("abc", Regexp::FIXEDENCODING).options)`, "16\n"},
		{`p(Regexp.new("abc", Regexp::NOENCODING).options)`, "32\n"},
		{`p(Regexp.new("abc", Regexp::FIXEDENCODING | Regexp::IGNORECASE).options)`, "17\n"},
		{`p(/abc/.options)`, "0\n"},
		{`p(/abc/mix.options)`, "7\n"},
	})
}

// TestRegexpTimeout covers Regexp.timeout / Regexp.timeout=, the timeout:
// keyword of Regexp.new, and Regexp#timeout (which reports only the Regexp's own
// limit, never the global default).
func TestRegexpTimeout(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p Regexp.timeout`, "nil\n"},
		{`Regexp.timeout = 5.0; p Regexp.timeout`, "5.0\n"},
		{`Regexp.timeout = 3; p Regexp.timeout`, "3.0\n"},
		{`p(/x/.timeout)`, "nil\n"},
		{`Regexp.timeout = 5.0; p(/x/.timeout)`, "nil\n"},
		{`p Regexp.new("abc", timeout: 3.5).timeout`, "3.5\n"},
		{`p Regexp.new("abc", timeout: 2).timeout`, "2.0\n"},
		{`p Regexp.new("abc", timeout: nil).timeout`, "nil\n"},
		{`p Regexp.new("abc").timeout`, "nil\n"},
		// A timeout is set but still matches within it.
		{`p(Regexp.new("a+", timeout: 5.0).match?("aaa"))`, "true\n"},
		// timeout: alongside a positional option.
		{`p Regexp.new("abc", Regexp::IGNORECASE, timeout: 1.0).timeout`, "1.0\n"},
		{`p Regexp.new("abc", Regexp::IGNORECASE, timeout: 1.0).options`, "1\n"},
		// Copying a Regexp keeps its timeout unless a new one is given.
		{`p Regexp.new(Regexp.new("a", timeout: 4.0)).timeout`, "4.0\n"},
		{`p Regexp.new(Regexp.new("a", timeout: 4.0), timeout: 9.0).timeout`, "9.0\n"},
	})
}

// TestRegexpTimeoutErrors covers the timeout: keyword's type error branch.
func TestRegexpTimeoutErrors(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`begin; Regexp.new("a", timeout: "x"); rescue => e; p e.class; end`, "TypeError\n"},
	})
}

// TestRegexpMatchPredicatePos covers Regexp#match?(str, pos), including negative
// and out-of-range positions, and the nil operand.
func TestRegexpMatchPredicatePos(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p(/x/.match?("axb", 1))`, "true\n"},
		{`p(/x/.match?("axb", 2))`, "false\n"},
		{`p(/b/.match?("axb", -1))`, "true\n"},
		{`p(/a/.match?("axb", -1))`, "false\n"},
		{`p(/x/.match?("axb", 99))`, "false\n"},
		{`p(/x/.match?(nil))`, "false\n"},
		{`p(/x/.match?("axb"))`, "true\n"},
	})
}

// TestRegexpTildeMatchesLastLine covers Regexp#~, which matches against $_ (the
// last line) and returns the character offset or nil, updating $~.
func TestRegexpTildeMatchesLastLine(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`$_ = "hello"; p(~ /ell/)`, "1\n"},
		{`$_ = "hello"; p(~ /zzz/)`, "nil\n"},
		{`$_ = "hello"; ~ /(l+)/; p $1`, "\"ll\"\n"},
		// No $_ set: nil operand, no match.
		{`p(~ /x/)`, "nil\n"},
	})
}

// TestMatchDataOffsets covers MatchData#offset / #begin / #end by index and by
// name, plus their error branches.
func TestMatchDataOffsets(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).offset(0)`, "[0, 11]\n"},
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).offset(1)`, "[0, 5]\n"},
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).offset(:b)`, "[6, 11]\n"},
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).offset("a")`, "[0, 5]\n"},
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).begin(:a)`, "0\n"},
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).end(:b)`, "11\n"},
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).begin(0)`, "0\n"},
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).end(2)`, "11\n"},
		// A non-participating optional group offsets to nil.
		{`p "a".match(/(a)(x)?/).offset(2)`, "[nil, nil]\n"},
		{`p "a".match(/(a)(x)?/).begin(2)`, "nil\n"},
	})
}

// TestMatchDataOffsetErrors covers the IndexError / TypeError branches of
// #offset / #begin / #end.
func TestMatchDataOffsetErrors(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`begin; "ab".match(/(.)/).offset(9); rescue => e; p [e.class, e.message]; end`,
			"[IndexError, \"index 9 out of matches\"]\n"},
		{`begin; "ab".match(/(?<a>.)/).offset(:z); rescue => e; p [e.class, e.message]; end`,
			"[IndexError, \"undefined group name reference: z\"]\n"},
		{`begin; "ab".match(/(?<a>.)/).begin(:z); rescue => e; p e.class; end`, "IndexError\n"},
		{`begin; "ab".match(/(.)/).begin(3.5); rescue => e; p e.class; end`, "TypeError\n"},
	})
}

// TestMatchDataNamesAndCaptures covers #names, #named_captures (with
// symbolize_names:), #regexp, #string.
func TestMatchDataNamesAndCaptures(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p "hi".match(/(?<a>.)(?<b>.)/).names`, "[\"a\", \"b\"]\n"},
		{`p "hi".match(/(.)(.)/).names`, "[]\n"},
		{`p "hi".match(/(?<a>.)(?<b>.)/).named_captures`, "{\"a\" => \"h\", \"b\" => \"i\"}\n"},
		{`p "hi".match(/(?<a>.)(?<b>.)/).named_captures(symbolize_names: true)`, "{a: \"h\", b: \"i\"}\n"},
		{`p "hi".match(/(?<a>.)(?<b>.)/).named_captures(symbolize_names: false)`, "{\"a\" => \"h\", \"b\" => \"i\"}\n"},
		// A non-participating named group captures nil.
		{`p "x".match(/(?<a>x)(?<b>y)?/).named_captures`, "{\"a\" => \"x\", \"b\" => nil}\n"},
		{`p "hi".match(/(?<a>.)./).regexp`, "/(?<a>.)./\n"},
		{`p "hi".match(/./).string`, "\"hi\"\n"},
	})
}

// TestMatchDataString covers #string (a frozen copy of the whole subject).
func TestMatchDataString(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p "hello".match(/l/).string`, "\"hello\"\n"},
		{`p "hello".match(/l/).string.frozen?`, "true\n"},
	})
}

// TestMatchDataToAAndCaptures covers #to_a, #captures, #deconstruct.
func TestMatchDataDeconstruct(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p "hi".match(/(.)(.)/).deconstruct`, "[\"h\", \"i\"]\n"},
		{`p "hi".match(/../).deconstruct`, "[]\n"},
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).deconstruct_keys(nil)`, "{a: \"hello\", b: \"world\"}\n"},
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).deconstruct_keys([:a])`, "{a: \"hello\"}\n"},
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).deconstruct_keys([:b, :a])`, "{b: \"world\", a: \"hello\"}\n"},
		// A missing key stops the walk (breaking before it is added).
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).deconstruct_keys([:nope, :a])`, "{}\n"},
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).deconstruct_keys([:a, :nope])`, "{a: \"hello\"}\n"},
		// More keys than named captures short-circuits to {}.
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).deconstruct_keys([:a, :b, :c])`, "{}\n"},
		{`p "hi".match(/(.)(.)/).deconstruct_keys(nil)`, "{}\n"},
	})
}

// TestMatchDataDeconstructErrors covers the TypeError branches of
// #deconstruct_keys.
func TestMatchDataDeconstructErrors(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`begin; "hi".match(/(?<a>.)./).deconstruct_keys(5); rescue => e; p [e.class, e.message]; end`,
			"[TypeError, \"wrong argument type Integer (expected Array)\"]\n"},
		{`begin; "hi".match(/(?<a>.)./).deconstruct_keys(["a"]); rescue => e; p [e.class, e.message]; end`,
			"[TypeError, \"wrong argument type String (expected Symbol)\"]\n"},
	})
}

// TestMatchDataValuesAt covers #values_at with Integer, name, negative, and Range
// arguments (including the nil padding of an over-long Range).
func TestMatchDataValuesAt(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p "hello world".match(/(\w+)\s+(\w+)/).values_at(0, 1, 2)`, "[\"hello world\", \"hello\", \"world\"]\n"},
		{`p "hello world".match(/(\w+)\s+(\w+)/).values_at(1, -1)`, "[\"hello\", \"world\"]\n"},
		{`p "hello world".match(/(?<a>\w+)\s+(?<b>\w+)/).values_at(:a, :b)`, "[\"hello\", \"world\"]\n"},
		{`p "abcd".match(/(b)(c)/).values_at(1..2)`, "[\"b\", \"c\"]\n"},
		{`p "abcd".match(/(b)(c)/).values_at(0...2)`, "[\"bc\", \"b\"]\n"},
		{`p "abcd".match(/(b)(c)/).values_at(1..)`, "[\"b\", \"c\"]\n"},
		{`p "abcd".match(/(b)(c)/).values_at(..1)`, "[\"bc\", \"b\"]\n"},
		{`p "abcd".match(/(b)(c)/).values_at(-3..-1)`, "[\"bc\", \"b\", \"c\"]\n"},
		{`p "abcd".match(/(b)(c)/).values_at(0..10).length`, "11\n"},
		{`p "abcd".match(/(b)(c)/).values_at(1, 5)`, "[\"b\", nil]\n"},
	})
}

// TestMatchDataValuesAtErrors covers the RangeError branch of #values_at.
func TestMatchDataValuesAtErrors(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`begin; "abcd".match(/(b)(c)/).values_at(-99..1); rescue => e; p [e.class, e.message]; end`,
			"[RangeError, \"-99..1 out of range\"]\n"},
	})
}

// TestMatchDataNegativeIndex covers the MRI negative-index quirk of MatchData#[]:
// negative indices reach the captures but a negative index landing on 0 is out
// of range (only the literal 0 selects the whole match).
func TestMatchDataNegativeIndex(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p "abcd".match(/(b)(c)/)[-1]`, "\"c\"\n"},
		{`p "abcd".match(/(b)(c)/)[-2]`, "\"b\"\n"},
		{`p "abcd".match(/(b)(c)/)[-3]`, "nil\n"},
		{`p "abcd".match(/(b)(c)/)[-4]`, "nil\n"},
		{`p "abcd".match(/(b)(c)/)[0]`, "\"bc\"\n"},
		{`p "a".match(/a/)[-1]`, "nil\n"},
	})
}

// TestMatchDataEquality covers MatchData#== / #eql? (equal regexp, subject and
// span) and #hash, via both the operator and method forms.
func TestMatchDataEquality(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`a = "ab".match(/../); b = "ab".match(/../); p(a == b)`, "true\n"},
		{`a = "ab".match(/../); b = "ab".match(/../); p(a.eql?(b))`, "true\n"},
		{`p("ab".match(/../).eql?("x"))`, "false\n"},
		{`p("ab".match(/../).send(:==, 5))`, "false\n"},
		{`a = "ab".match(/../); b = "ab".match(/../); p(a.send(:==, b))`, "true\n"},
		{`a = "ab".match(/../); b = "ab".match(/../); p(a.hash == b.hash)`, "true\n"},
		{`a = "ab".match(/../); b = "cd".match(/../); p(a == b)`, "false\n"},
		{`a = "abab".match(/../); b = "abab".match(/../, 2); p(a == b)`, "false\n"},
		{`p("ab".match(/../) == "ab")`, "false\n"},
		{`p("ab".match(/../) == nil)`, "false\n"},
	})
}

// TestMatchGlobals covers the $~, $&, $`, $', $+, $1.. globals after a match.
func TestMatchGlobals(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`"ab".match(/(a)(b)/); p $+`, "\"b\"\n"},
		{`"a".match(/(a)(x)?/); p $+`, "\"a\"\n"},
		{`"a".match(/a/); p $+`, "nil\n"},
		{`"hello".match(/l(l)o/); p [$&, $1, $` + "`" + `, $']`, "[\"llo\", \"l\", \"he\", \"\"]\n"},
		{`"ab".match(/(a)(b)/); p $~.class`, "MatchData\n"},
	})
}
