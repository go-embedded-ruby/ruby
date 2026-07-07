// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"
)

// relineHead wires an in-memory input StringIO carrying the scripted key bytes
// and a throwaway output StringIO, so a Reline.readline session runs
// deterministically with no real terminal.
func relineHead(input string) string {
	return `require "reline"
Reline.input = StringIO.new(` + relineByteStr(input) + `)
Reline.output = StringIO.new
`
}

// relineByteStr renders s as a Ruby double-quoted string literal preserving arbitrary
// bytes via \xNN escapes.
func relineByteStr(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c >= 0x20 && c < 0x7f:
			b.WriteByte(c)
		default:
			b.WriteString("\\x")
			const hex = "0123456789ABCDEF"
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
		}
	}
	b.WriteByte('"')
	return b.String()
}

// TestRelineReadlineBasics exercises readline: a submitted line, EOF (empty and
// partial), the prompt/no-prompt argument, add_hist, and vi/emacs mode.
func TestRelineReadlineBasics(t *testing.T) {
	cases := []struct{ src, want string }{
		// A submitted line.
		{relineHead("hello\n") + `p Reline.readline("> ")`, "\"hello\"\n"},
		// No prompt argument (relinePrompt nil branch, add_hist arg absent).
		{relineHead("abc\n") + `p Reline.readline`, "\"abc\"\n"},
		// EOF on empty input returns nil.
		{relineHead("") + `p Reline.readline("> ")`, "nil\n"},
		// EOF after a partial (unterminated) line returns that line.
		{relineHead("ab") + `p Reline.readline("> ")`, "\"ab\"\n"},
		// add_hist appends a non-empty line to HISTORY.
		{relineHead("cmd\n") + "Reline.readline(\"> \", true)\np Reline::HISTORY.to_a", "[\"cmd\"]\n"},
		// add_hist does NOT append an empty line.
		{relineHead("\n") + "Reline.readline(\"> \", true)\np Reline::HISTORY.size", "0\n"},
		// vi editing mode (then back to emacs) still submits a typed line.
		{`require "reline"
Reline.vi_editing_mode
Reline.input = StringIO.new("xy\n")
Reline.output = StringIO.new
r = Reline.readline
Reline.emacs_editing_mode
p r`, "\"xy\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRelineEditingKeys drives the editing keys the loop decodes: cursor
// movement, kill/yank, backspace, transpose, history search, clear-screen, and
// an ignored control byte.
func TestRelineEditingKeys(t *testing.T) {
	cases := []struct{ src, want string }{
		// C-a (move to beg) then insert.
		{relineHead("hello\x01X\n") + `p Reline.readline`, "\"Xhello\"\n"},
		// C-e (move to end) after C-a.
		{relineHead("hi\x01\x05Z\n") + `p Reline.readline`, "\"hiZ\"\n"},
		// backspace (0x7f) deletes previous char.
		{relineHead("ab\x7f\n") + `p Reline.readline`, "\"a\"\n"},
		// C-h (0x08) also deletes previous char.
		{relineHead("ab\x08\n") + `p Reline.readline`, "\"a\"\n"},
		// C-k kill to end, C-y yank it back.
		{relineHead("hello\x01\x0b\x19\n") + `p Reline.readline`, "\"hello\"\n"},
		// C-u unix-line-discard clears to start.
		{relineHead("hello\x15z\n") + `p Reline.readline`, "\"z\"\n"},
		// C-l (clear screen) leaves the buffer intact (ClearScreen seam).
		{relineHead("ab\x0ccd\n") + `p Reline.readline`, "\"abcd\"\n"},
		// An ignored control byte (BEL, 0x07) does nothing.
		{relineHead("a\x07b\n") + `p Reline.readline`, "\"ab\"\n"},
		// C-b (prev char) then insert, then C-f style continues typing.
		{relineHead("ac\x02b\n") + `p Reline.readline`, "\"abc\"\n"},
		// C-d (em_delete) removes the char under the cursor.
		{relineHead("abc\x01\x04\n") + `p Reline.readline`, "\"bc\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRelineEscapeAndUTF8 covers the escape-sequence decoder (CSI/SS3 arrows,
// unknown finals, ESC-not-a-sequence via UngetC, ESC at EOF) and the multibyte
// UTF-8 decoder (2/3/4-byte runes, an invalid lead, a truncated lead).
func TestRelineEscapeAndUTF8(t *testing.T) {
	cases := []struct{ src, want string }{
		// CSI left-arrow moves the cursor left; insert lands before it.
		{relineHead("ab\x1b[Dc\n") + `p Reline.readline`, "\"acb\"\n"},
		// SS3 left-arrow (ESC O D) does the same (the c=='O' branch).
		{relineHead("ab\x1bODc\n") + `p Reline.readline`, "\"acb\"\n"},
		// An unknown CSI final byte (ESC [ Z) is ignored.
		{relineHead("a\x1b[Zb\n") + `p Reline.readline`, "\"ab\"\n"},
		// ESC not followed by [ or O: the peeked byte is pushed back (UngetC).
		{relineHead("a\x1bbc\n") + `p Reline.readline`, "\"abc\"\n"},
		// ESC at end of input is ignored; the partial line is returned.
		{relineHead("a\x1b") + `p Reline.readline`, "\"a\"\n"},
		// 2-byte rune (é).
		{relineHead("caf\xc3\xa9\n") + `p Reline.readline`, "\"caf\xc3\xa9\"\n"},
		// 3-byte rune (€).
		{relineHead("a\xe2\x82\xacb\n") + `p Reline.readline`, "\"a\xe2\x82\xacb\"\n"},
		// 4-byte rune (😀).
		{relineHead("a\xf0\x9f\x98\x80b\n") + `p Reline.readline`, "\"a\xf0\x9f\x98\x80b\"\n"},
		// Invalid UTF-8 lead byte (0x80) is ignored.
		{relineHead("a\x80b\n") + `p Reline.readline`, "\"ab\"\n"},
		// A truncated lead byte at EOF (0xC3 then end) is ignored → empty line.
		{relineHead("\xc3") + `p Reline.readline`, "nil\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRelineCompletion covers the completion bridge: a 1-arity proc, a 3-arity
// proc, a non-Array result (no completion), and the append character.
func TestRelineCompletion(t *testing.T) {
	cases := []struct{ src, want string }{
		// 1-arity completion proc completes "fo" -> "foo".
		{`require "reline"
Reline.completion_proc = ->(t){ ["foo"] }
Reline.input = StringIO.new("fo\t\n")
Reline.output = StringIO.new
p Reline.readline("> ")`, "\"foo\"\n"},
		// 3-arity completion proc (target, pre, post).
		{`require "reline"
Reline.completion_proc = proc { |t, pre, post| ["foobar"] }
Reline.input = StringIO.new("fo\t\n")
Reline.output = StringIO.new
p Reline.readline("> ")`, "\"foobar\"\n"},
		// A non-Array result means no completion; the word is unchanged.
		{`require "reline"
Reline.completion_proc = ->(t){ nil }
Reline.input = StringIO.new("fo\t\n")
Reline.output = StringIO.new
p Reline.readline("> ")`, "\"fo\"\n"},
		// The append character is added after a unique completion.
		{`require "reline"
Reline.completion_proc = ->(t){ ["foo"] }
Reline.completion_append_character = "!"
Reline.input = StringIO.new("fo\t\n")
Reline.output = StringIO.new
p Reline.readline("> ").start_with?("foo")`, "true\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRelineConfigAccessors covers the completion_proc / append-character
// getters+setters and the input=/output= TypeError guard.
func TestRelineConfigAccessors(t *testing.T) {
	cases := []struct{ src, want string }{
		// completion_proc getter: nil, then set, then reset by a non-proc.
		{`require "reline"
p Reline.completion_proc
pr = ->(t){ [] }
Reline.completion_proc = pr
p Reline.completion_proc.equal?(pr)
Reline.completion_proc = 5
p Reline.completion_proc`, "nil\ntrue\nnil\n"},
		// completion_append_character getter: nil, set, cleared by nil.
		{`require "reline"
p Reline.completion_append_character
Reline.completion_append_character = " "
p Reline.completion_append_character
Reline.completion_append_character = nil
p Reline.completion_append_character`, "nil\n\" \"\nnil\n"},
		// input= rejects a non-IO with TypeError.
		{`require "reline"
begin; Reline.input = 5; rescue => e; p e.class; end`, "TypeError\n"},
		// output= rejects a non-IO with TypeError.
		{`require "reline"
begin; Reline.output = 5; rescue => e; p e.class; end`, "TypeError\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRelineMultiline covers readmultiline: the termination proc decides when
// Enter submits, and the multi-row buffer exercises the multi-line render path.
func TestRelineMultiline(t *testing.T) {
	src := `require "reline"
Reline.input = StringIO.new("a\nb\n")
Reline.output = StringIO.new
p Reline.readmultiline("> ") { |buf| buf.count("\n") >= 2 }`
	if got := runFS(t, src); got != "\"a\\nb\"\n" {
		t.Errorf("got=%q", got)
	}
}

// TestRelineDefaultStreams covers the default input/output resolution: a readline
// with no injected streams reads $stdin and writes $stdout, and a non-IO $stdin
// falls back to the STDIN constant.
func TestRelineDefaultStreams(t *testing.T) {
	// Default $stdin (empty) → nil; default $stdout receives the prompt redraw.
	got := runFS(t, `require "reline"
p Reline.readline("P> ")`)
	if !strings.Contains(got, "P> ") || !strings.Contains(got, "nil") {
		t.Errorf("default-stream readline: got=%q", got)
	}
	// A non-IO $stdin falls back to the STDIN constant (also empty → nil).
	got = runFS(t, `require "reline"
$stdin = 5
Reline.output = StringIO.new
p Reline.readline("> ")`)
	if got != "nil\n" {
		t.Errorf("stdin-fallback readline: got=%q", got)
	}
}

// TestRelineHistory covers the Reline::HISTORY Array-subset protocol and its
// index-error mapping.
func TestRelineHistory(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "reline"
h = Reline::HISTORY
h << "a"
h.push("b", "c")
p [h.size, h.length]
p [h[0], h[-1]]
h[1] = "B"
p h[1]
p h.to_a
p h.empty?
p h.last
p h.last(2)
p h.last(9)
p h.delete_at(0)
p h.pop
r = []; h.each { |x| r << x }; p r
p h.each
h.clear
p h.empty?
p h.pop
p h.last`,
			`[3, 3]
["a", "c"]
"B"
["a", "B", "c"]
false
"c"
["B", "c"]
["a", "B", "c"]
"a"
"c"
["B"]
#<Reline::History>
true
nil
nil
`},
		// Inspect / to_s / truthiness of the HISTORY object.
		{`require "reline"
p Reline::HISTORY
puts Reline::HISTORY.to_s
p !!Reline::HISTORY`, "#<Reline::History>\n#<Reline::History>\ntrue\n"},
		// Out-of-range index → IndexError; an oversized index → RangeError.
		{`require "reline"
begin; Reline::HISTORY[5]; rescue => e; p e.class; end
begin; Reline::HISTORY[100000000000]; rescue => e; p e.class; end
begin; Reline::HISTORY[3] = "x"; rescue => e; p e.class; end
begin; Reline::HISTORY.delete_at(9); rescue => e; p e.class; end`,
			"IndexError\nRangeError\nIndexError\nIndexError\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRelineRequire covers the provided-feature registration: require returns
// true once, then false.
func TestRelineRequire(t *testing.T) {
	if got := runFS(t, `p require "reline"
p require "reline"`); got != "true\nfalse\n" {
		t.Errorf("got=%q", got)
	}
}
