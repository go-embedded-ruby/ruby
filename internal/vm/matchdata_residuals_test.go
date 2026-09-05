package vm_test

import (
	"strings"
	"testing"
)

// TestRegexpUnicodeEscapes covers translateUnicodeEscapes / parseUnicodeBraceBody
// / parseHexRune / emitLiteralRune: Ruby's \uHHHH and \u{…} escapes, which the
// engine does not accept, are rewritten to the literal characters they denote.
func TestRegexpUnicodeEscapes(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// \u{…} → non-ASCII literal (raw-UTF-8 branch of emitLiteralRune).
		{"brace_nonascii", `p("あ".match(/\u{3042}/)[0])`, "\"あ\"\n"},
		// \u{…} → ASCII alnum literal (verbatim branch).
		{"brace_alnum", `p("A".match(/\u{41}/)[0])`, "\"A\"\n"},
		// \u{…} → ASCII metacharacter kept literal (backslash-escape branch): the
		// dot matches only a dot, not an arbitrary character.
		{"brace_meta_dot", `p(".".match?(/\u{2E}/), "x".match?(/\u{2E}/))`, "true\nfalse\n"},
		// \u{…} → ASCII control (\xHH byte-escape branch): matches a real tab.
		{"brace_control_tab", `p("\t".match?(/\u{9}/), "x".match?(/\u{9}/))`, "true\nfalse\n"},
		// \u{…} with several code points.
		{"brace_multi", `p("AB".match(/\u{41 42}/)[0])`, "\"AB\"\n"},
		// A non-\u escape following a \u escape is copied through untouched.
		{"other_escape_after_u", `p("A5" =~ /\u{41}\d/)`, "0\n"},
		// A leading ordinary char before the escape exercises the plain-copy path.
		{"leading_plain", `p("xA".match(/x\u{41}/)[0])`, "\"xA\"\n"},
		// The four-hex \uHHHH form (built at runtime so the source really carries a
		// backslash-u rather than a resolved character) hits the non-brace branch.
		{"fourhex", `p("A".match(Regexp.new(92.chr + "u0041"))[0])`, "\"A\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRegexpUnicodeEscapesInvalid covers the fall-through / ok=false paths: a \u
// that is not a well-formed escape is left for the engine, which raises RegexpError
// (matching MRI, which rejects each of these). Each source is built with 92.chr so
// it carries a literal backslash-u without a pre-resolved escape.
func TestRegexpUnicodeEscapesInvalid(t *testing.T) {
	for _, suffix := range []string{
		`uZZZZ`,      // four non-hex digits after \u
		`u{ZZ}`,      // non-hex inside braces
		`u{}`,        // empty Unicode list
		`u{110000}`,  // code point past U+10FFFF
		`u{1234567}`, // run longer than six hex digits
		`u{`,         // unterminated \u{
		`u9`,         // fewer than four hex digits, no brace
	} {
		src := `Regexp.new(92.chr + "` + suffix + `")`
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "RegexpError") {
			t.Errorf("src=%q: got err=%v, want RegexpError", src, err)
		}
	}
}

// TestMatchDataDuplicateNames covers rewriteNamedGroups / needsNameRewrite /
// indexOfName for patterns whose capture groups share a name, which the engine
// cannot compile as written. Ruby resolves a shared name to the highest-indexed
// group with that name that participated in the match.
func TestMatchDataDuplicateNames(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// #[] and #named_captures prefer the later matched group.
		{"element_ref_last", `p("haystack".match(/(?<w>hay)(?<w>stack)/)[:w])`, "\"stack\"\n"},
		{"named_captures_later", `p(/\A(?<a>.)(?<b>.)(?<b>.)(?<a>.)\z/.match('0123').named_captures)`,
			"{\"a\" => \"3\", \"b\" => \"2\"}\n"},
		// The latest *matched* group wins even when a later same-named group did not
		// participate (exercises the Begin>=0 guard and the return-best fallback).
		{"named_captures_latest_matched", `p(/\A(?<a>.)(?<b>.)(?<b>.)(?<a>.)?\z/.match('012').named_captures)`,
			"{\"a\" => \"0\", \"b\" => \"2\"}\n"},
		// #begin picks the farthest (highest-indexed) matched group's offset.
		{"begin_farthest", `p(/(?<a>.)(?<a>\d+)(\d)/.match("THX1138.").begin("a"))`, "3\n"},
		// #names lists each shared name once, in first-appearance order.
		{"names_once", `p("haystack".match(/(?<hay>hay)(?<dot>.)(?<hay>tack)/).names)`,
			"[\"hay\", \"dot\"]\n"},
		// An alternation where only one same-named branch matches returns that branch.
		{"alternation_branch", `p(/(?:A(?<w>\w+)|B(?<w>\w+))/.match("Bfoo")[:w])`, "\"foo\"\n"},
		// A character class inside a rewritten pattern is scanned but not rewritten;
		// a negated class and an escaped char inside a class both survive intact.
		{"class_negated", `p("Ax".match(/(?<a>[^0-9])(?<a>.)/).captures)`, "[\"A\", \"x\"]\n"},
		{"class_escaped_dash", `p("a-b".match(/(?<a>[a\-])(?<a>.)/).captures)`, "[\"a\", \"-\"]\n"},
		// A shared name whose groups all fail to match reads as nil, not an error.
		{"all_unmatched_nil", `p(/(?<a>x)?(?<a>y)?z/.match("z")[:a])`, "nil\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestMatchDataMultibyteNames covers the non-ASCII-name path of needsNameRewrite
// and the \k / \g reference rewriting (both < > and ' ' delimiters), which the
// engine also rejects as written.
func TestMatchDataMultibyteNames(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"begin_multibyte", `p(/(?<æ>.)(?<b>\d+)(\d)/.match("THX1138.").begin("æ"))`, "2\n"},
		{"element_ref_multibyte", `p("x".match(/(?<æ>.)/)[:"æ"])`, "\"x\"\n"},
		{"names_multibyte", `p("x".match(/(?<æ>.)/).names)`, "[\"æ\"]\n"},
		// \k<name> backreference to a multibyte name.
		{"k_backref_angle", `p("aa".match(/(?<æ>.)\k<æ>/)[0])`, "\"aa\"\n"},
		// \k'name' backreference (quote delimiter).
		{"k_backref_quote", `p("aa".match(/(?<æ>.)\k'æ'/)[0])`, "\"aa\"\n"},
		// \g<name> subroutine call to a multibyte name.
		{"g_subroutine", `p("abcabc".match(/(?<æ>abc)\g<æ>/)[0])`, "\"abcabc\"\n"},
		// An unmatched multibyte-named group reads as nil.
		{"unmatched_nil", `p(/(?<æ>x)?y/.match("y")[:"æ"])`, "nil\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestMatchDataNameResolutionUnchanged guards the common paths: an ordinary
// (single, ASCII) named pattern is not rewritten (nil name map, engine handles
// names), and an unknown name still raises IndexError on both an ordinary and a
// rewritten pattern.
func TestMatchDataNameResolutionUnchanged(t *testing.T) {
	if got := eval(t, `m = "x".match(/(?<a>.)/); p(m[:a], m.begin("a"), m.named_captures)`); got != "\"x\"\n0\n{\"a\" => \"x\"}\n" {
		t.Errorf("ordinary named pattern: got %q", got)
	}
	// Unknown name on an ordinary pattern (indexOfName's engine-fallback path).
	if err := runErr(t, `"x".match(/(?<a>.)/)[:zzz]`); err == nil || !strings.Contains(err.Error(), "IndexError") {
		t.Errorf("unknown name (ordinary): got err=%v, want IndexError", err)
	}
	// Unknown name on a rewritten (duplicate-name) pattern (indexOfName's
	// name-not-in-map branch).
	if err := runErr(t, `"xy".match(/(?<a>.)(?<a>.)/)[:zzz]`); err == nil || !strings.Contains(err.Error(), "IndexError") {
		t.Errorf("unknown name (rewritten): got err=%v, want IndexError", err)
	}
	// A flagged regexp exercises compileRegexp's inline-option prefix branch.
	if got := eval(t, `p("A".match?(/a/i))`); got != "true\n" {
		t.Errorf("flagged regexp: got %q", got)
	}
}
