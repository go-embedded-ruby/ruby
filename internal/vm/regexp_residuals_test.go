package vm_test

import "testing"

// TestRegexpUnionResiduals covers Regexp.union operand handling: a lone Regexp
// (or #to_regexp) is returned verbatim, each Regexp in an alternation contributes
// its #to_s (options ride along), Strings/Symbols are quoted, an Array is the
// pattern list, and a multi-argument Symbol (which lacks #to_str) raises
// TypeError while a lone Symbol is accepted. Checked against MRI ruby 4.0.
func TestRegexpUnionResiduals(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p Regexp.union.source`, "\"(?!)\"\n"},
		{`p Regexp.union([]).source`, "\"(?!)\"\n"},
		{`p Regexp.union(/dogs/, /cats/i).source`, "\"(?-mix:dogs)|(?i-mx:cats)\"\n"},
		{`p Regexp.union("skiing", "sledding").source`, "\"skiing|sledding\"\n"},
		{`p Regexp.union("n", ".").source`, "\"n|\\\\.\"\n"},
		{`p Regexp.union(["+", "-"]).source`, "\"\\\\+|\\\\-\"\n"},
		// A lone Symbol is accepted (its name is quoted); a lone Regexp is verbatim.
		{`p Regexp.union(:foo).source`, "\"foo\"\n"},
		{`p Regexp.union(/dogs/i)`, "/dogs/i\n"},
		{`p Regexp.union([/dogs/i])`, "/dogs/i\n"},
		// #to_regexp and #to_str coercion in union.
		{`o = Object.new; def o.to_regexp; /foo/; end; p Regexp.union(o)`, "/foo/\n"},
		{`o = Object.new; def o.to_str; "foo"; end; p Regexp.union(o, "bar").source`, "\"foo|bar\"\n"},
		// A Symbol in a multi-argument union has no #to_str: TypeError.
		{`begin; Regexp.union(:foo, "bar"); rescue => e; p e.class; end`, "TypeError\n"},
	})
}

// TestRegexpQuoteEscapeResiduals covers Regexp.quote/escape: MRI's exact escape
// set (including '-', space, '#' and the control-char escapes) and Symbol input,
// plus the escape/quote Method-identity alias.
func TestRegexpQuoteEscapeResiduals(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`print Regexp.quote("a-b c.d#e")`, "a\\-b\\ c\\.d\\#e"},
		{`print Regexp.escape("\t\n\r\f\v")`, "\\t\\n\\r\\f\\v"},
		{`print Regexp.quote(:symbol)`, "symbol"},
		{`p Regexp.method(:escape) == Regexp.method(:quote)`, "true\n"},
	})
}

// TestRegexpInspectSlash covers the forward-slash escaping of #inspect and #to_s,
// without double-escaping an already-escaped slash.
func TestRegexpInspectSlash(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p Regexp.new("/foo/bar")`, "/\\/foo\\/bar/\n"},
		{`p Regexp.new("//")`, "/\\/\\//\n"},
		{`p(/\/a\/b/)`, "/\\/a\\/b/\n"},
		{`puts Regexp.new("/x").to_s`, "(?-mix:\\/x)\n"},
	})
}

// TestRegexpMatchResiduals covers Regexp#match with a Symbol subject, a nil
// subject (which resets $~), a block (yielding the MatchData and returning the
// block's value, or nil with no match), and the uninitialized-Regexp TypeError.
func TestRegexpMatchResiduals(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p(/(.)(.)/.match(:ab).is_a?(MatchData))`, "true\n"},
		{`/./.match("a"); /1/.match(nil); p $~`, "nil\n"},
		{`p(/./.match("abc") { :res })`, ":res\n"},
		{`p(/z/.match("abc") { :res })`, "nil\n"},
		{`/./.match("abc") { |m| $captured = m }; p $captured.is_a?(MatchData)`, "true\n"},
		{`begin; Regexp.allocate.match("foo"); rescue => e; p e.class; end`, "TypeError\n"},
		{`begin; Regexp.allocate.options; rescue => e; p e.class; end`, "TypeError\n"},
	})
}

// TestRegexpMatchDataAliases covers the genuine built-in aliases (shared method
// records) and the small Ruby-3.4 accessors added alongside them.
func TestRegexpMatchDataAliases(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p Regexp.instance_method(:eql?) == Regexp.instance_method(:==)`, "true\n"},
		{`p MatchData.instance_method(:eql?) == MatchData.instance_method(:==)`, "true\n"},
		{`p MatchData.instance_method(:length) == MatchData.instance_method(:size)`, "true\n"},
		{`p MatchData.instance_method(:deconstruct) == MatchData.instance_method(:captures)`, "true\n"},
		// #deconstruct still behaves as #captures.
		{`p "hi".match(/(.)(.)/).deconstruct`, "[\"h\", \"i\"]\n"},
		// MatchData#match_length: character length of a group, nil when absent.
		{`p(/(.)(.)(\d+)(\d)/.match("THX1138.").match_length(3))`, "3\n"},
		{`p(/\d+(\w)?/.match("THX1138.").match_length(1))`, "nil\n"},
		{`m = "haystack".match(/(?<t>t(?<a>ack))/); p m.match_length(:t)`, "4\n"},
		// MatchData.allocate is undefined.
		{`begin; MatchData.allocate; rescue => e; p e.class; end`, "NoMethodError\n"},
	})
}

// TestRegexpInitialize covers the private #initialize that always refuses: a
// frozen literal raises FrozenError, an already-initialized non-literal (and a
// subclass instance) raises TypeError, and #initialize is registered private.
func TestRegexpInitialize(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p Regexp.private_instance_methods(false).include?(:initialize)`, "true\n"},
		{`begin; //.send(:initialize, ""); rescue => e; p e.class; end`, "FrozenError\n"},
		{`begin; Regexp.new("").send(:initialize, ""); rescue => e; p e.class; end`, "TypeError\n"},
		{`r = Class.new(Regexp).new(""); begin; r.send(:initialize, ""); rescue => e; p e.class; end`, "TypeError\n"},
	})
}

// TestRegexpCaseCompareToStr covers Regexp#=== coercing a string-like operand
// through #to_str (and still clearing $~ / returning false for a non-string).
func TestRegexpCaseCompareToStr(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`o = Object.new; def o.to_str; "abc"; end; p(/b/ === o)`, "true\n"},
		{`o = Object.new; def o.to_str; "xyz"; end; p(/b/ === o)`, "false\n"},
		{`p(/b/ === 5)`, "false\n"},
	})
}

// TestMatchDataInspectNamed covers MatchData#inspect: when the pattern has named
// groups only those are shown (by name), omitting unnamed numbered groups;
// otherwise every numbered group is shown.
func TestMatchDataInspectNamed(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p "abc def ghi".match(/(?<first>\w+)\s+(?<last>\w+)\s+(\w+)/).inspect`,
			"\"#<MatchData \\\"abc def ghi\\\" first:\\\"abc\\\" last:\\\"def\\\">\"\n"},
		{`p "ab".match(/(.)(.)/).inspect`,
			"\"#<MatchData \\\"ab\\\" 1:\\\"a\\\" 2:\\\"b\\\">\"\n"},
	})
}

// TestMatchDataDeconstructKeysArity covers the zero-argument ArgumentError of
// MatchData#deconstruct_keys.
func TestMatchDataDeconstructKeysArity(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`begin; "a".match(/(?<x>.)/).deconstruct_keys; rescue ArgumentError => e; p e.message; end`,
			"\"wrong number of arguments (given 0, expected 1)\"\n"},
	})
}

// TestMatchDataMatchMethod covers MatchData#match(n) / #match(name) (Ruby 3.4):
// the scalar subset of #[], returning nil for a non-participating group.
func TestMatchDataMatchMethod(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p(/(.)(.)(\d+)(\d)/.match("THX1138.").match(0))`, "\"HX1138\"\n"},
		{`p(/(.)(.)(\d+)(\d)/.match("THX1138.").match(3))`, "\"113\"\n"},
		{`p(/\d+(\w)?/.match("THX1138.").match(1))`, "nil\n"},
		{`m = "haystack".match(/(?<t>t(?<a>ack))/); p m.match(:a)`, "\"ack\"\n"},
		{`m = "haystack".match(/(?<t>t(?<a>ack))/); p m.match(:t)`, "\"tack\"\n"},
	})
}
