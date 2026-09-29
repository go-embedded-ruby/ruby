package compiler

import "testing"

// TestMagicFrozenStringLiteral pins where MRI honours
// `# frozen_string_literal:` and how it spells the pragma. Every want below was
// MEASURED against ruby 4.0.5 (+PRISM) under BOTH --parser=parse.y and
// --parser=prism, which agree on all of them, and each is explained by the
// oracle: parser_magic_comment (parse.y ruby_4_0:9493) and
// parser_set_frozen_string_literal (:9380).
func TestMagicFrozenStringLiteral(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"absent", "s = \"abc\"\n", false},
		{"true", "# frozen_string_literal: true\ns = \"abc\"\n", true},
		{"false", "# frozen_string_literal: false\ns = \"abc\"\n", false},

		// The name is compared with STRNCASECMP after `-` is rewritten to `_`
		// (:9576), so all three of these name the same pragma. ruby/spec's title
		// "creates a non-frozen String when # frozen-string-literal: true is used"
		// reads as if the dashed form did nothing; measuring says otherwise, and
		// that example's literal is interpolated, which is the real reason it is
		// not frozen.
		{"dashed name", "# frozen-string-literal: true\n", true},
		{"upper name", "# FROZEN_STRING_LITERAL: true\n", true},
		{"mixed name", "# Frozen-String-Literal: true\n", true},
		{"upper value", "# frozen_string_literal: TRUE\n", true},
		{"no space after colon", "# frozen_string_literal:true\n", true},
		{"trailing spaces", "# frozen_string_literal: true   \n", true},

		// parser_get_bool (:9348) accepts only true/false; anything else leaves
		// the setting where it was.
		{"invalid value", "# frozen_string_literal: yes\n", false},
		{"invalid value after true", "# frozen_string_literal: true\n# frozen_string_literal: yes\n", true},

		// A non-Emacs magic comment must be the WHOLE comment: after the value
		// only whitespace may follow, or MRI abandons the line before applying
		// the field it has just read (:9567-9570).
		{"trailing junk", "# frozen_string_literal: true rubocop:disable\n", false},
		{"semicolons without markers", "# encoding: utf-8; frozen_string_literal: true\n", false},

		// Emacs style: `;` separates fields only between `-*-` markers.
		{"emacs alone", "# -*- frozen_string_literal: true -*-\n", true},
		{"emacs with encoding", "# -*- encoding: utf-8; frozen_string_literal: true -*-\n", true},
		{"emacs encoding last", "# -*- frozen_string_literal: true; encoding: utf-8 -*-\n", true},
		{"emacs quoted value", "# -*- frozen_string_literal: \"true\" -*-\n", true},
		{"emacs unterminated quote", "# -*- frozen_string_literal: \"true -*-\n", false},
		{"emacs unrelated field first", "# -*- mode: ruby; frozen_string_literal: true -*-\n", true},

		// Anywhere before the first TOKEN counts, because
		// parser_set_frozen_string_literal only checks p->token_seen -- unlike the
		// encoding pragma, which additionally needs comment_at_top.
		{"after shebang", "#!/usr/bin/env ruby\n# frozen_string_literal: true\n", true},
		{"after encoding comment", "# encoding: utf-8\n# frozen_string_literal: true\n", true},
		{"after a plain comment", "# hello\n# and more\n# frozen_string_literal: true\n", true},
		{"after a blank line", "\n\n# frozen_string_literal: true\n", true},
		{"indented comment", "   # frozen_string_literal: true\n", true},
		{"after an embedded document", "=begin\nprose\n=end\n# frozen_string_literal: true\n", true},

		// And nowhere after one. A trailing comment is not the first token of its
		// line, and a comment inside a body is well past the first token.
		{"after a token", "x = 1\n# frozen_string_literal: true\n", false},
		{"trailing on a code line", "x = 1 # frozen_string_literal: true\n", false},
		{"inside a method body", "def m\n  # frozen_string_literal: true\n  \"abc\"\nend\n", false},

		// MRI assigns p->frozen_string_literal on every declaration, so the last
		// one before the first token wins.
		{"true then false", "# frozen_string_literal: true\n# frozen_string_literal: false\n", false},
		{"false then true", "# frozen_string_literal: false\n# frozen_string_literal: true\n", true},

		// "no magic_comment in shebang line" (:10548).
		{"pragma on the shebang line", "#!/usr/bin/env ruby frozen_string_literal: true\n", false},

		// Below parser_magic_comment's length floor (:9503), so not a magic
		// comment at all.
		{"too short", "# a: b\n", false},

		// A file that is only a comment, with no trailing newline, still declares.
		{"no trailing newline", "# frozen_string_literal: true", true},
		{"empty source", "", false},
	}
	for _, c := range cases {
		if got := magicFrozenStringLiteral(c.src); got != c.want {
			t.Errorf("%s: magicFrozenStringLiteral(%q) = %v, want %v", c.name, c.src, got, c.want)
		}
	}
}

// TestMagicCommentFields pins the field scanner itself, including the forms no
// pragma test above reaches: several fields on one Emacs line, a quoted value
// keeping its backslashes, and an unterminated quote.
func TestMagicCommentFields(t *testing.T) {
	collect := func(s string) []string {
		var out []string
		magicCommentFields(s, func(name, val string) { out = append(out, name+"="+val) })
		return out
	}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", " frozen_string_literal: true", "[frozen_string_literal=true]"},
		{"name canonicalised", " Frozen-String-Literal: true", "[frozen_string_literal=true]"},
		{"emacs two fields", " -*- encoding: utf-8; frozen_string_literal: true -*-",
			"[encoding=utf-8 frozen_string_literal=true]"},
		{"emacs three fields", " -*- mode: ruby; coding: big5; warn_indent: true -*-",
			"[mode=ruby coding=big5 warn_indent=true]"},
		{"emacs quoted value keeps escapes", " -*- coding: \"a\\\"b\" -*-", `[coding=a\"b]`},
		// An unterminated quote runs the value to the end of the field text, which
		// still carries the space that preceded the closing marker. MEASURED: MRI
		// reports `unknown encoding name: utf-8 ` for exactly this line, trailing
		// space included. This is the ONE input where the two parsers differ --
		// --parser=prism silently ignores it -- and parse.y, the grammar oracle,
		// is what is followed here.
		{"emacs unterminated quote", " -*- coding: \"utf-8 -*-", "[coding=utf-8 ]"},
		// The same shape with a bool value: `true ` is not `true`, so the pragma
		// is left alone (MEASURED: both parsers answer false).
		{"emacs unterminated bool", " -*- frozen_string_literal: \"true -*-", "[frozen_string_literal=true ]"},
		{"emacs opening marker only", " -*- coding: utf-8", "[]"},
		{"trailing junk abandons the line", " frozen_string_literal: true and more", "[]"},
		{"no colon at all", " just a sentence here", "[]"},
		{"too short", " a: b", "[]"},
		{"value only, no name", " : true and padding", "[]"},
	}
	for _, c := range cases {
		if got := niceFields(collect(c.in)); got != c.want {
			t.Errorf("%s: magicCommentFields(%q) = %s, want %s", c.name, c.in, got, c.want)
		}
	}
}

// niceFields renders a field list stably, including the empty one.
func niceFields(fs []string) string {
	out := "["
	for i, f := range fs {
		if i > 0 {
			out += " "
		}
		out += f
	}
	return out + "]"
}

// TestMagicCommentMarker covers magic_comment_marker's stride directly: it steps
// by 2, 3 or 4 rather than 1, so it accepts and rejects positions a plain
// substring search would not. The inputs below are the ones where the two
// differ, which is the only reason the port is faithful rather than a
// strings.Index.
func TestMagicCommentMarker(t *testing.T) {
	cases := []struct {
		in    string
		idx   int
		found bool
	}{
		// Landing on the '-' of a complete marker at i==2.
		{"-*-x", 3, true},
		// Landing on the '*' with '-' on both sides.
		{"a-*-", 4, true},
		// A '*' whose next byte is not '-' skips four.
		{"ab**cd-*-ef", 9, true},
		// No marker at all, and a string too short to hold one.
		{"abcdefg", 0, false},
		{"-*", 0, false},
		{"", 0, false},
		// A trailing '*' with nothing after it stops the scan.
		{"abc*", 0, false},
	}
	for _, c := range cases {
		idx, found := magicCommentMarker(c.in)
		if idx != c.idx || found != c.found {
			t.Errorf("magicCommentMarker(%q) = (%d, %v), want (%d, %v)", c.in, idx, found, c.idx, c.found)
		}
	}
}

// TestMagicBool covers parser_get_bool's accepted spellings and its rejection of
// everything else.
func TestMagicBool(t *testing.T) {
	cases := []struct {
		in    string
		b, ok bool
	}{
		{"true", true, true},
		{"TRUE", true, true},
		{"True", true, true},
		{"false", false, true},
		{"FALSE", false, true},
		{"truthy", false, false},
		{"1", false, false},
		{"", false, false},
	}
	for _, c := range cases {
		b, ok := magicBool(c.in)
		if b != c.b || ok != c.ok {
			t.Errorf("magicBool(%q) = (%v, %v), want (%v, %v)", c.in, b, ok, c.b, c.ok)
		}
	}
}

// TestMagicCommentsCombines checks that the two pragmas are read INDEPENDENTLY,
// which is the whole reason MagicComments exists: MRI gates the encoding on
// comment_at_top and the frozen-literal pragma on token_seen alone, so a file
// can declare either, both or neither.
func TestMagicCommentsCombines(t *testing.T) {
	cases := []struct {
		src  string
		want Magic
	}{
		{"# encoding: binary\n# frozen_string_literal: true\n", Magic{Encoding: "ASCII-8BIT", FrozenStringLiteral: true}},
		{"# frozen_string_literal: true\n", Magic{FrozenStringLiteral: true}},
		{"# encoding: binary\n", Magic{Encoding: "ASCII-8BIT"}},
		{"x = 1\n", Magic{}},
		// The encoding pragma is NOT at the top here, so MRI ignores it while
		// still honouring the frozen-literal one on the line before it. This is
		// what ruby/spec's "ignores the magic encoding comment if it is after a
		// frozen_string_literal magic comment" observes.
		{"# frozen_string_literal: true\n# encoding: binary\n", Magic{FrozenStringLiteral: true}},
	}
	for _, c := range cases {
		if got := MagicComments(c.src); got != c.want {
			t.Errorf("MagicComments(%q) = %+v, want %+v", c.src, got, c.want)
		}
	}
}
