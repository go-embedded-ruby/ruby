package compiler

import "strings"

// Magic holds what a source file's leading magic-comment block declared. It is
// what the compiler needs from the source TEXT, which the compiler never sees:
// it is handed an already-parsed ast.Program, so every such property has to be
// read by the caller and passed in. Grouping them in one value means the next
// pragma MRI grows (`# shareable_constant_value:`, `# warn_indent:` — see the
// magic_comments table, parse.y ruby_4_0:9448) costs no call-site churn.
type Magic struct {
	// Encoding is the canonical registry encoding name a `# encoding:` /
	// `# coding:` comment declared, or "" for the UTF-8 default.
	Encoding string
	// FrozenStringLiteral is true when `# frozen_string_literal: true` was
	// declared, making every non-interpolated String literal in the file a
	// shared frozen object.
	FrozenStringLiteral bool
}

// MagicComments reads src's leading magic-comment block.
//
// The two pragmas are gated DIFFERENTLY in MRI, which is why they are scanned
// separately here:
//
//   - the encoding comment only counts at the top of the file — magic_comment_encoding
//     returns early unless comment_at_top(p), i.e. line 1, or line 2 after a
//     shebang (parse.y ruby_4_0:9328 and 9340);
//   - frozen_string_literal counts anywhere before the first TOKEN —
//     parser_set_frozen_string_literal only checks p->token_seen
//     (parse.y ruby_4_0:9380).
//
// So `# encoding: X` on line 2 after a plain comment is ignored while
// `# frozen_string_literal: true` there is honoured, which is what ruby/spec's
// "ignores the magic encoding comment if it is after a frozen_string_literal
// magic comment" (core/kernel/eval_spec.rb) is really observing.
func MagicComments(src string) Magic {
	return Magic{
		Encoding:            magicSourceEncoding(src),
		FrozenStringLiteral: magicFrozenStringLiteral(src),
	}
}

// magicFrozenStringLiteral reports whether src declares
// `# frozen_string_literal: true` before its first token.
//
// The scan walks the leading region of the file: a shebang on line 1, blank
// lines, `=begin`/`=end` block comments and `#` comments — magic or not — all
// leave p->token_seen false in MRI, so the search continues past them; anything
// else is a token and ends it. A later declaration overrides an earlier one
// (MRI simply assigns p->frozen_string_literal each time), and a value that is
// neither `true` nor `false` leaves the setting alone (parser_get_bool returns
// -1 and the setter returns early, parse.y ruby_4_0:9348 and 9390).
//
// Measured against ruby 4.0.5 under BOTH --parser=parse.y and --parser=prism,
// which agree: `# frozen-string-literal: true` is honoured (the name's `-` is
// rewritten to `_`, parse.y:9576), `# frozen_string_literal: true rubocop:disable`
// is NOT (a non-Emacs magic comment must be the whole comment, parse.y:9569),
// and neither is `# encoding: utf-8; frozen_string_literal: true` without the
// `-*-` markers (`;` only separates fields inside them).
func magicFrozenStringLiteral(src string) bool {
	frozen := false
	inBlockComment := false
	for lineNo := 1; src != ""; lineNo++ {
		var line string
		line, src, _ = strings.Cut(src, "\n")
		line = strings.TrimSuffix(line, "\r")
		if inBlockComment {
			// MRI ends an embedded document at a line starting with `=end`
			// (parser_yylex's `=begin` handling); the rest of the line is free text.
			inBlockComment = !strings.HasPrefix(line, "=end")
			continue
		}
		// "no magic_comment in shebang line" (parse.y ruby_4_0:10548): a `#!` first
		// line is consumed by parser_prepare before the lexer sees a comment.
		if lineNo == 1 && strings.HasPrefix(line, "#!") {
			continue
		}
		if strings.HasPrefix(line, "=begin") {
			inBlockComment = true
			continue
		}
		t := strings.TrimLeft(line, " \t\v\f")
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "#") {
			// A token. Everything after this point is "ignored after any tokens".
			return frozen
		}
		magicCommentFields(t[1:], func(name, val string) {
			if name != "frozen_string_literal" {
				return
			}
			if b, ok := magicBool(val); ok {
				frozen = b
			}
		})
	}
	return frozen
}

// magicBool parses a pragma's boolean value, MRI's parser_get_bool
// (parse.y ruby_4_0:9348): `true` and `false`, case-insensitively, and nothing
// else. A second return of false means "invalid", which MRI warns about and
// otherwise ignores, leaving the previous setting in place.
func magicBool(val string) (b, ok bool) {
	switch {
	case strings.EqualFold(val, "true"):
		return true, true
	case strings.EqualFold(val, "false"):
		return false, true
	}
	return false, false
}

// magicCommentFields calls fn(name, value) for each pragma a comment declares,
// a port of MRI's parser_magic_comment (parse.y ruby_4_0:9493). s is the comment
// text AFTER the leading `#`, which is what MRI's lexer hands it.
//
// Two forms exist and they are not equivalent:
//
//   - Emacs style, `-*- name: value; name: value -*-`, where `;` separates
//     several fields and trailing text outside the markers is ignored;
//   - plain style, `# name: value`, where the field must be the WHOLE comment:
//     anything but whitespace after the value abandons the line entirely
//     (parse.y:9569), so `# frozen_string_literal: true rubocop:disable`
//     declares nothing at all.
//
// Names are matched with `-` rewritten to `_` and case-insensitively
// (parse.y:9575-9579), so fn always receives a lower-cased, underscored name.
// A quoted value keeps its backslashes: MRI copies the raw bytes between the
// quotes (str_copy at 9584) without unescaping them.
func magicCommentFields(s string, fn func(name, val string)) {
	// MRI's length floor: the shortest interesting field is longer than this.
	if len(s) <= 7 {
		return
	}
	indicator := false
	if b, ok := magicCommentMarker(s); ok {
		e, ok := magicCommentMarker(s[b:])
		if !ok {
			return
		}
		// The second marker's end is b+e; the field text stops 3 bytes earlier,
		// before the closing `-*-`.
		s = s[b : b+e-3]
		indicator = true
	}
	for i := 0; i < len(s); {
		// Skip the separators and whitespace that precede a name.
		for ; i < len(s); i++ {
			if isMagicSep(s[i]) {
				continue
			}
			if !isMagicSpace(s[i]) {
				break
			}
		}
		beg := i
		for ; i < len(s); i++ {
			if isMagicSep(s[i]) || isMagicSpace(s[i]) {
				break
			}
		}
		name := s[beg:i]
		for ; i < len(s) && isMagicSpace(s[i]); i++ {
		}
		if i >= len(s) {
			return
		}
		if s[i] != ':' {
			if !indicator {
				// Not a `name: value` comment at all.
				return
			}
			continue
		}
		for i++; i < len(s) && isMagicSpace(s[i]); i++ {
		}
		if i >= len(s) {
			return
		}
		var val string
		if s[i] == '"' {
			i++
			vbeg := i
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' {
					i++
				}
				i++
			}
			val = s[vbeg:min(i, len(s))]
			if i < len(s) {
				i++ // past the closing quote
			}
		} else {
			vbeg := i
			for ; i < len(s) && s[i] != '"' && s[i] != ';' && !isMagicSpace(s[i]); i++ {
			}
			val = s[vbeg:i]
		}
		if indicator {
			for ; i < len(s) && (s[i] == ';' || isMagicSpace(s[i])); i++ {
			}
		} else {
			for ; i < len(s) && isMagicSpace(s[i]); i++ {
			}
			if i < len(s) {
				// Trailing junk: MRI abandons the line WITHOUT applying the field
				// it has just read, because the check precedes the setter call.
				return
			}
		}
		fn(magicName(name), val)
	}
}

// magicName canonicalises a pragma name the way MRI compares it: `-` is
// rewritten to `_` in place (parse.y ruby_4_0:9576) and the comparison is
// STRNCASECMP, so `Frozen-String-Literal` names the same pragma as
// `frozen_string_literal`.
func magicName(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "-", "_"))
}

// isMagicSep reports whether b is one of the bytes MRI treats as a field
// separator rather than part of a name (parse.y ruby_4_0:9521 and 9528).
func isMagicSep(b byte) bool {
	return b == '\'' || b == '"' || b == ':' || b == ';'
}

// isMagicSpace is MRI's ISSPACE over the bytes a single comment line can hold.
func isMagicSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\v' || b == '\f' || b == '\r'
}

// magicCommentMarker finds the `-*-` that opens or closes an Emacs-style magic
// comment and returns the index just PAST it, a port of MRI's
// magic_comment_marker (parse.y ruby_4_0:9459). Its stride is not 1: it steps by
// 2, 3 or 4 depending on the byte it lands on, which is how `-*-` is found
// without scanning every position, and a plain search for the substring would
// accept markers this does not.
func magicCommentMarker(s string) (int, bool) {
	for i := 2; i < len(s); {
		switch s[i] {
		case '-':
			if s[i-1] == '*' && s[i-2] == '-' {
				return i + 1, true
			}
			i += 2
		case '*':
			if i+1 >= len(s) {
				return 0, false
			}
			switch {
			case s[i+1] != '-':
				i += 4
			case s[i-1] != '-':
				i += 2
			default:
				return i + 2, true
			}
		default:
			i += 3
		}
	}
	return 0, false
}
