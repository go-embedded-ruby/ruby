// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// undumpNamedEscape maps the backslash-escaped control letters String#undump
// recognizes back to their byte value (the inverse of Dump's named escapes).
var undumpNamedEscape = map[byte]byte{
	'a': 0x07, 'b': 0x08, 't': 0x09, 'n': 0x0a,
	'v': 0x0b, 'f': 0x0c, 'r': 0x0d, 'e': 0x1b,
	'"': '"', '\\': '\\', '#': '#',
}

// undumpHexNibble returns the value of a single hexadecimal digit and whether c is one.
func undumpHexNibble(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

// stringUndump implements String#undump: it parses a String#dump literal back
// into the original string. The receiver must be an ASCII-compatible encoding
// (else Encoding::CompatibilityError); the text must be wrapped in double quotes,
// optionally followed by .force_encoding("NAME"); every backslash escape is
// decoded; and a raw NUL or non-ASCII byte, a malformed escape, or trailing junk
// raises RuntimeError. Verified against ruby 4.0.6.
func (vm *VM) stringUndump(s *object.String) object.Value {
	if e, ok := vm.findEncoding(s.EncName()); ok && !e.asciiCompat {
		raise("Encoding::CompatibilityError", "ASCII incompatible encoding: %s", s.EncName())
	}
	data := s.Bytes()
	n := len(data)
	invalid := func() {
		raise("RuntimeError", `invalid dumped string; not wrapped with '"' nor '"...".force_encoding("...")' form`)
	}
	if n == 0 || data[0] != '"' {
		invalid()
	}

	var out []byte
	i := 1
	closed := false
	for i < n {
		c := data[i]
		if c == '"' {
			i++
			closed = true
			break
		}
		if c == 0 {
			raise("RuntimeError", "string contains null byte")
		}
		if c >= 0x80 {
			raise("RuntimeError", "non-ASCII character detected")
		}
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		// A backslash escape.
		i++
		if i >= n {
			raise("RuntimeError", "invalid escape")
		}
		esc := data[i]
		if b, ok := undumpNamedEscape[esc]; ok {
			out = append(out, b)
			i++
			continue
		}
		switch esc {
		case 'x':
			i++
			hi, ok1 := hexAt(data, i)
			lo, ok2 := hexAt(data, i+1)
			if !ok1 || !ok2 {
				raise("RuntimeError", "invalid hex escape")
			}
			out = append(out, byte(hi<<4|lo))
			i += 2
		case 'u':
			i++
			out = vm.undumpUnicode(data, &i, out)
		default:
			// dump never emits any other escape; MRI leaves an unrecognized one
			// as-is, keeping the backslash (\q undumps to \q).
			out = append(out, '\\', esc)
			i++
		}
	}
	if !closed {
		raise("RuntimeError", "unterminated dumped string")
	}

	enc := s.EncName()
	if rest := data[i:]; len(rest) > 0 {
		enc = vm.undumpForceEncoding(string(rest))
	}
	return object.NewStringBytesEnc(out, enc)
}

// hexAt returns the hex value of data[i], reporting false when i is out of range
// or the byte is not a hex digit.
func hexAt(data []byte, i int) (int, bool) {
	if i < 0 || i >= len(data) {
		return 0, false
	}
	return undumpHexNibble(data[i])
}

// undumpUnicode decodes a \u escape at *i (already past the 'u'): either exactly
// four hex digits (\uXXXX) or one or more space-separated hex code points in
// braces (\u{XXXX YYYY}). Each code point is appended to out as UTF-8. A
// malformed escape raises RuntimeError.
func (vm *VM) undumpUnicode(data []byte, i *int, out []byte) []byte {
	bad := func() { raise("RuntimeError", "invalid Unicode escape") }
	if *i < len(data) && data[*i] == '{' {
		*i++
		for {
			for *i < len(data) && data[*i] == ' ' {
				*i++
			}
			if *i < len(data) && data[*i] == '}' {
				*i++
				return out
			}
			start := *i
			cp := 0
			for *i < len(data) {
				v, ok := undumpHexNibble(data[*i])
				if !ok {
					break
				}
				cp = cp<<4 | v
				if cp > 0x10FFFF {
					cp = 0x110000 // clamp so many digits cannot overflow int
				}
				*i++
			}
			if *i == start {
				bad()
			}
			if cp > 0x10FFFF {
				raise("RuntimeError", "invalid Unicode codepoint (too large)")
			}
			out = utf8.AppendRune(out, rune(cp))
		}
	}
	cp := 0
	for k := 0; k < 4; k++ {
		v, ok := hexAt(data, *i)
		if !ok {
			bad()
		}
		cp = cp<<4 | v
		*i++
	}
	return utf8.AppendRune(out, rune(cp))
}

// undumpForceEncoding parses the .force_encoding("NAME") suffix that follows the
// closing quote of a dumped non-default-encoding string and returns NAME. A
// malformed suffix, or a name no known encoding matches, raises RuntimeError.
func (vm *VM) undumpForceEncoding(rest string) string {
	const pre = `.force_encoding("`
	if !strings.HasPrefix(rest, pre) || !strings.HasSuffix(rest, `")`) || len(rest) < len(pre)+2 {
		raise("RuntimeError", `invalid dumped string; not wrapped with '"' nor '"...".force_encoding("...")' form`)
	}
	name := rest[len(pre) : len(rest)-2]
	if _, ok := vm.findEncoding(name); !ok {
		raise("RuntimeError", "dumped string has unknown encoding name")
	}
	return name
}
