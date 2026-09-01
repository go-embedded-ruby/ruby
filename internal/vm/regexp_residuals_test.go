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
