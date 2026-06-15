package vm_test

import (
	"strings"
	"testing"
)

// Phase 6a: Regexp literals, the Regexp class, MatchData, and the String
// regex-matching methods (=~, match, match?). Behaviour is pinned against MRI
// (Ruby 4.0). Offsets are character-based (Ruby semantics); the underlying
// go-onigmo engine reports byte offsets which this package converts.

func TestRegexpBasics(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"source", `p(/a(b)c/.source)`, "\"a(b)c\"\n"},
		{"class", `p(/x/.class)`, "Regexp\n"},
		{"inspect_noflags", `p(/abc/.inspect)`, "\"/abc/\"\n"},
		{"inspect_i", `p(/abc/i.inspect)`, "\"/abc/i\"\n"},
		{"inspect_m", `p(/abc/m.inspect)`, "\"/abc/m\"\n"},
		{"inspect_imx_order", `p(/abc/xmi.inspect)`, "\"/abc/mix\"\n"},
		{"to_s_noflags", `p(/abc/.to_s)`, "\"(?-mix:abc)\"\n"},
		{"to_s_m", `p(/abc/m.to_s)`, "\"(?m-ix:abc)\"\n"},
		{"to_s_x", `p(/abc/x.to_s)`, "\"(?x-mi:abc)\"\n"},
		{"to_s_imx", `p(/abc/imx.to_s)`, "\"(?mix:abc)\"\n"},
		{"to_s_im", `p(/abc/im.to_s)`, "\"(?mi-x:abc)\"\n"},
		{"match_q_true", `p(/l+/.match?("hello"))`, "true\n"},
		{"match_q_false", `p(/z/.match?("hello"))`, "false\n"},
		{"match_class", `p(/l/.match("hello").class)`, "MatchData\n"},
		{"match_nil", `p(/z/.match("hello"))`, "nil\n"},
		{"match_nil_arg", `p(/x/.match(nil))`, "nil\n"},
		{"match_q_nil_arg", `p(/x/.match?(nil))`, "false\n"},
		{"flag_i", `p(/abc/i.match?("xABCy"))`, "true\n"},
		{"flag_m_dotall", `p(/a.b/m.match?("a\nb"))`, "true\n"},
		{"flag_x_extended", `p(/a b c/x.match?("abc"))`, "true\n"},
		{"flag_other_ignored", `p(/abc/o.source)`, "\"abc\"\n"},
		{"eq_true", `p(/abc/ == /abc/)`, "true\n"},
		{"eq_flags_differ", `p(/abc/i == /abc/)`, "false\n"},
		{"eq_same_flags", `p(/abc/i == /abc/i)`, "true\n"},
		{"eq_nonregexp", `p(/abc/ == "abc")`, "false\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestRegexpMatchOperators(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"caseeq_string_true", `p(/foo/ === "afoob")`, "true\n"},
		{"caseeq_string_false", `p(/foo/ === "bar")`, "false\n"},
		{"caseeq_symbol", `p(/fo/ === :foo)`, "true\n"},
		{"caseeq_nonstring", `p(/foo/ === 123)`, "false\n"},
		{"re_match_index", `p(/x/ =~ "axb")`, "1\n"},
		{"re_match_index_nil", `p(/z/ =~ "axb")`, "nil\n"},
		{"re_match_symbol", `p(/o/ =~ :foo)`, "1\n"},
		{"re_match_nil_subject", `p(/x/ =~ nil)`, "nil\n"},
		{"str_match_index", `p("axb" =~ /x/)`, "1\n"},
		{"str_match_nil", `p("axb" =~ /z/)`, "nil\n"},
		{"str_match_method", `p("hello".match(/l+/).class)`, "MatchData\n"},
		{"str_match_string_pattern", `p("hello".match("l+").class)`, "MatchData\n"},
		{"str_match_no_match", `p("hello".match(/z/))`, "nil\n"},
		{"str_match_q", `p("hello".match?(/l+/))`, "true\n"},
		{"str_match_q_string", `p("hello".match?("l+"))`, "true\n"},
		{"str_match_q_false", `p("hello".match?(/z/))`, "false\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestMatchData(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"group0", `p(/(\d+)-(\d+)/.match("a12-34b")[0])`, "\"12-34\"\n"},
		{"group1", `p(/(\d+)-(\d+)/.match("a12-34b")[1])`, "\"12\"\n"},
		{"group2", `p(/(\d+)-(\d+)/.match("a12-34b")[2])`, "\"34\"\n"},
		{"group_oob", `p(/(a)/.match("a")[5])`, "nil\n"},
		{"pre_match", `p(/c/.match("abcde").pre_match)`, "\"ab\"\n"},
		{"post_match", `p(/c/.match("abcde").post_match)`, "\"de\"\n"},
		{"to_a", `p(/(\d+)-(\d+)/.match("12-34").to_a)`, "[\"12-34\", \"12\", \"34\"]\n"},
		{"captures", `p(/(\d+)-(\d+)/.match("12-34").captures)`, "[\"12\", \"34\"]\n"},
		{"captures_empty", `p(/abc/.match("abc").captures)`, "[]\n"},
		{"begin0", `p(/cd/.match("abcde").begin(0))`, "2\n"},
		{"end0", `p(/cd/.match("abcde").end(0))`, "4\n"},
		{"begin1", `p(/(cd)/.match("abcde").begin(1))`, "2\n"},
		{"size", `p(/(\d+)-(\d+)/.match("12-34").size)`, "3\n"},
		{"length", `p(/(\d+)-(\d+)/.match("12-34").length)`, "3\n"},
		{"to_s", `p(/cd/.match("abcde").to_s)`, "\"cd\"\n"},
		{"inspect", `puts(/(\d+)/.match("a12b").inspect)`, "#<MatchData \"12\" 1:\"12\">\n"},
		{"inspect_named", `puts(/(?<n>\d+)/.match("a12b").inspect)`, "#<MatchData \"12\" n:\"12\">\n"},
		{"inspect_p", `p(/(\d+)/.match("a12b"))`, "#<MatchData \"12\" 1:\"12\">\n"},
		{"optional_nil", `p(/(a)(b)?/.match("a")[2])`, "nil\n"},
		{"optional_begin_nil", `p(/(a)(b)?/.match("a").begin(2))`, "nil\n"},
		{"optional_end_nil", `p(/(a)(b)?/.match("a").end(2))`, "nil\n"},
		{"optional_captures", `p(/(a)(b)?/.match("a").captures)`, "[\"a\", nil]\n"},
		{"optional_to_a", `p(/(a)(b)?/.match("a").to_a)`, "[\"a\", \"a\", nil]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestMatchDataNamed(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"by_symbol", `p(/(?<yr>\d+)-(?<mo>\d+)/.match("2026-06")[:yr])`, "\"2026\"\n"},
		{"by_string", `p(/(?<yr>\d+)-(?<mo>\d+)/.match("2026-06")["mo"])`, "\"06\"\n"},
		{"named_captures", `p(/(?<yr>\d+)-(?<mo>\d+)/.match("2026-06").named_captures)`,
			"{\"yr\" => \"2026\", \"mo\" => \"06\"}\n"},
		{"named_captures_empty", `p(/abc/.match("abc").named_captures)`, "{}\n"},
		{"named_captures_nil", `p(/(?<a>x)(?<b>y)?/.match("x").named_captures)`,
			"{\"a\" => \"x\", \"b\" => nil}\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRegexpToSAndTruthy covers the Go-level ToS/Truthy value methods, reached
// when a Regexp or MatchData flows through puts (ToS) or a boolean context.
func TestRegexpToSAndTruthy(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"regexp_puts", `puts(/abc/m)`, "(?m-ix:abc)\n"},
		{"matchdata_puts", `puts(/cd/.match("abcde"))`, "cd\n"},
		{"regexp_truthy", `r = /a/; puts(r ? "t" : "f")`, "t\n"},
		{"matchdata_truthy", `m = /a/.match("a"); puts(m ? "t" : "f")`, "t\n"},
		{"regexp_interp", `r = /x/m; puts "re=#{r}"`, "re=(?m-ix:x)\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRegexpClassNameInErrors hits classNameOf's Regexp/MatchData arms via the
// TypeError raised when such a value is used as a MatchData index.
func TestRegexpClassNameInErrors(t *testing.T) {
	if err := runErr(t, `/(a)/.match("a")[/x/]`); err == nil ||
		!strings.Contains(err.Error(), "Regexp") {
		t.Errorf("Regexp-keyed index: got %v", err)
	}
	if err := runErr(t, `m = /(a)/.match("a"); m[m]`); err == nil ||
		!strings.Contains(err.Error(), "MatchData") {
		t.Errorf("MatchData-keyed index: got %v", err)
	}
}

// TestRegexpLexerEdges covers the lexer's regexp-literal edge cases: an
// unterminated literal (EOF before the closing slash), a trailing backslash at
// EOF, and non-imx trailing letters being consumed but ignored.
func TestRegexpLexerEdges(t *testing.T) {
	// A non-i/m/x trailing flag letter is consumed and ignored.
	if got := eval(t, `p(/ab/n.source)`); got != "\"ab\"\n" {
		t.Errorf("ignored flag: got %q", got)
	}
	// Unterminated literal at EOF: the lexer yields a regexp token covering the
	// remaining bytes; parsing then fails (no closing paren) rather than panicking.
	if err := runErr(t, `p(/abc`); err == nil {
		t.Error("unterminated regexp: expected a parse error")
	}
	// A trailing backslash at EOF (no escaped byte) must also terminate cleanly.
	if err := runErr(t, "p(/abc\\"); err == nil {
		t.Error("trailing backslash regexp: expected a parse error")
	}
}

// TestRegexpCompileErrorLiteral exercises compileRegexp's error path: a literal
// the engine rejects raises a (catchable) RegexpError at evaluation time.
func TestRegexpCompileErrorLiteral(t *testing.T) {
	if err := runErr(t, `/(/`); err == nil || !strings.Contains(err.Error(), "RegexpError") {
		t.Errorf("got %v, want RegexpError", err)
	}
}

// TestRegexpMultibyte pins the byte-vs-character offset boundary: =~ and
// MatchData#begin report CHARACTER offsets even with a multibyte prefix, while
// the matched substring is representation-independent.
func TestRegexpMultibyte(t *testing.T) {
	if got := eval(t, `p("héllo" =~ /l/)`); got != "2\n" {
		t.Errorf("=~ char offset: got %q", got)
	}
	if got := eval(t, `p("héllo".match(/l/).begin(0))`); got != "2\n" {
		t.Errorf("begin char offset: got %q", got)
	}
	if got := eval(t, `p("héllo".match(/l/)[0])`); got != "\"l\"\n" {
		t.Errorf("matched substring: got %q", got)
	}
}

// TestRegexpDivisionAmbiguity confirms a '/' after a value is division, while a
// '/' in value-expected position opens a regexp literal.
func TestRegexpDivisionAmbiguity(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"div_after_local", "a = 10\nputs a / 2", "5\n"},
		{"div_chain", `puts 10 / 2 / 1`, "5\n"},
		{"div_after_paren", `puts (8) / 2`, "4\n"},
		{"regex_begin", `puts(/x/.source)`, "x\n"},
		{"regex_after_comma", `p [/a/.source, /b/.source]`, "[\"a\", \"b\"]\n"},
		{"escaped_slash", `p(/a\/b/.match?("a/b"))`, "true\n"},
		{"escaped_d", `p(/\d+/.match?("x42"))`, "true\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestRegexpErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"begin_oob", `/(a)/.match("a").begin(5)`, "IndexError"},
		{"end_oob", `/(a)/.match("a").end(9)`, "IndexError"},
		{"begin_negative", `/(a)/.match("a").begin(-1)`, "IndexError"},
		{"bad_name", `/(?<a>x)/.match("x")[:nope]`, "IndexError"},
		{"str_match_typeerror", `"x" =~ "y"`, "TypeError"},
		{"index_bad_type", `/(a)/.match("a")[1.5]`, "TypeError"},
		{"str_match_bad_type", `"x".match(42)`, "TypeError"},
		{"regexp_eqmatch_nonstring", `/x/ =~ 42`, "TypeError"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q\n got err=%v\nwant contains %q", tc.src, err, tc.want)
			}
		})
	}
}

// TestRegexpCompileError exercises the runtime RegexpError path: a malformed
// pattern reaching String#match (string form) raises RegexpError (in MRI the
// equivalent literal is a parse-time SyntaxError; we compile lazily, so the
// error surfaces at the matching call instead — and is catchable).
func TestRegexpCompileError(t *testing.T) {
	err := runErr(t, `"x".match("(")`)
	if err == nil || !strings.Contains(err.Error(), "RegexpError") {
		t.Errorf("got err=%v, want RegexpError", err)
	}
}

// TestRegexpNamedGroupParsing covers namedGroups' escape and look-behind
// skipping: an escaped paren and a (?<=...) look-behind must not be counted as
// named captures.
func TestRegexpNamedGroupParsing(t *testing.T) {
	// Escaped open-paren before a named group: only the real named group counts.
	if got := eval(t, `p(/\((?<n>\d+)\)/.match("(42)").named_captures)`); got != "{\"n\" => \"42\"}\n" {
		t.Errorf("escaped paren: got %q", got)
	}
	// Look-behind (?<=x) is not a named capture.
	if got := eval(t, `p(/(?<=a)(?<m>b)/.match("ab").named_captures)`); got != "{\"m\" => \"b\"}\n" {
		t.Errorf("lookbehind: got %q", got)
	}
}
