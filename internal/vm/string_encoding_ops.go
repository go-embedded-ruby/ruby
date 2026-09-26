package vm

import (
	"bytes"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// forceEncodingName resolves a String#force_encoding argument to a canonical
// encoding name. It is encodingName plus MRI's "internal" special name: it maps
// to Encoding.default_internal, or BINARY (ASCII-8BIT) when that is unset.
// ("external"/"locale"/"filesystem" are ordinary aliases handled by the table.)
func (vm *VM) forceEncodingName(v object.Value) string {
	if s, ok := v.(*object.String); ok && lower(s.Str()) == "internal" {
		if vm.defInternalEnc != nil {
			return vm.defInternalEnc.name
		}
		return "ASCII-8BIT"
	}
	return vm.encodingName(v)
}

// validInEncoding reports whether the bytes b form a valid sequence in the named
// encoding, backing String#valid_encoding?. ASCII-8BIT is always valid; US-ASCII
// requires 7-bit bytes; UTF-8/16/32 are validated structurally; encodings with an
// x/text codec are validated by decoding; anything else rbgo cannot inspect is
// treated as valid (matching MRI's "no coderange" fallthrough for dummy encodings).
func validInEncoding(b []byte, enc string) bool {
	switch enc {
	case "UTF-8":
		return utf8.Valid(b)
	case "ASCII-8BIT":
		return true
	case "US-ASCII":
		return asciiOnly(b)
	case "UTF-16LE":
		return validUTF16(b, false)
	case "UTF-16BE":
		return validUTF16(b, true)
	case "UTF-32LE":
		return validUTF32(b, false)
	case "UTF-32BE":
		return validUTF32(b, true)
	}
	// An encoding with a scrub scanner has a character automaton, and
	// rb_enc_str_coderange (string.c) asks that same automaton whether the string is
	// BROKEN: validity and scrubbing cannot be allowed to disagree about what a
	// well-formed character is, so both read the one scanner.
	if scan, ok := scrubScannerFor(enc); ok {
		for i := 0; i < len(b); {
			n, valid := scan(b[i:])
			if !valid {
				return false
			}
			i += n
		}
		return true
	}
	if _, ok := xtextEncodings[enc]; ok {
		return xtextValid(b, enc)
	}
	return true
}

// validUTF16 reports whether b is a well-formed UTF-16 byte stream (be selects
// big-endian): an even byte count and every surrogate correctly paired.
func validUTF16(b []byte, be bool) bool {
	if len(b)%2 != 0 {
		return false
	}
	unit := func(i int) uint16 {
		if be {
			return uint16(b[i])<<8 | uint16(b[i+1])
		}
		return uint16(b[i+1])<<8 | uint16(b[i])
	}
	for i := 0; i < len(b); i += 2 {
		u := unit(i)
		switch {
		case u >= 0xD800 && u <= 0xDBFF: // high surrogate — a low surrogate must follow
			if i+4 > len(b) {
				return false
			}
			if lo := unit(i + 2); lo < 0xDC00 || lo > 0xDFFF {
				return false
			}
			i += 2
		case u >= 0xDC00 && u <= 0xDFFF: // unpaired low surrogate
			return false
		}
	}
	return true
}

// validUTF32 reports whether b is a well-formed UTF-32 byte stream (be selects
// big-endian): a byte count divisible by four and every code point a scalar value.
func validUTF32(b []byte, be bool) bool {
	if len(b)%4 != 0 {
		return false
	}
	for i := 0; i < len(b); i += 4 {
		var u uint32
		if be {
			u = uint32(b[i])<<24 | uint32(b[i+1])<<16 | uint32(b[i+2])<<8 | uint32(b[i+3])
		} else {
			u = uint32(b[i+3])<<24 | uint32(b[i+2])<<16 | uint32(b[i+1])<<8 | uint32(b[i])
		}
		if u > 0x10FFFF || (u >= 0xD800 && u <= 0xDFFF) {
			return false
		}
	}
	return true
}

// xtextValid reports whether b decodes cleanly in the x/text-backed encoding
// named enc. x/text substitutes ill-formed input with U+FFFD instead of erroring,
// so a replacement character in the decoded output that the source could not have
// encoded literally signals an invalid byte sequence.
func xtextValid(b []byte, enc string) bool {
	// x/text decoders substitute rather than error, so validity is detected by a
	// U+FFFD appearing in the decoded output for a source that could not encode one.
	out, _ := xtextEncodings[enc].NewDecoder().Bytes(b)
	return !bytes.ContainsRune(out, utf8.RuneError)
}

// scrubScan reports the length of the next token in a scrub scan of b and whether
// it is a well-formed unit in the encoding. For an ill-formed token the length is
// that of the maximal ill-formed subpart, which collapses to one replacement.
type scrubScan func(b []byte) (n int, valid bool)

// scrubScannerFor returns the scanner for an encoding, or (nil, false) when rbgo
// has no scrubber for it — in which case the string is treated as always valid
// (ASCII-8BIT, and any legacy multibyte encoding without a rbgo scanner).
func scrubScannerFor(enc string) (scrubScan, bool) {
	switch enc {
	case "UTF-8":
		return scanUTF8Token, true
	case "US-ASCII":
		return scanASCIIToken, true
	case "UTF-16LE":
		return func(b []byte) (int, bool) { return scanUTF16Token(b, false) }, true
	case "UTF-16BE":
		return func(b []byte) (int, bool) { return scanUTF16Token(b, true) }, true
	case "UTF-32LE":
		return func(b []byte) (int, bool) { return scanUTF32Token(b, false) }, true
	case "UTF-32BE":
		return func(b []byte) (int, bool) { return scanUTF32Token(b, true) }, true
	case "Emacs-Mule":
		return scanEmacsMuleToken, true
	}
	return nil, false
}

// emacsMuleNext is one step of the Emacs-Mule character automaton transcribed from
// the `trans` table in enc/emacs_mule.c. It returns the next state for byte c, or
// emacsMuleAccept when the character is complete and emacsMuleFail when the byte
// cannot continue it. The grammar the table encodes is stated in that file:
//
//	CHARACTER        := ASCII_CHAR | MULTIBYTE_CHAR
//	PRIMARY_CHAR_1   := LEADING_CODE_PRI C1              (0x81..0x8F + one C byte)
//	PRIMARY_CHAR_2   := LEADING_CODE_PRI C1 C2           (0x90..0x99 + two C bytes)
//	SECONDARY_CHAR   := LEADING_CODE_SEC LEADING_CODE_EXT C1 [C2]
//	C1, C2, LEADING_CODE_EXT := 0xA0..0xFF
//
// The three secondary leads take DIFFERENT extension ranges (0x9A/0x9B accept
// 0xE0..0xEF, 0x9C accepts 0xF0..0xF4 and 0x9D accepts 0xF5..0xFE), which is why
// the automaton is transcribed rather than derived from the grammar comment: the
// EncLen_EmacsMule table is only an upper bound on a character's length, and MRI
// judges validity with the automaton (precise_mbc_enc_len), not with EncLen.
//
// The table's S3 is not transcribed: no S0 transition reaches it, so it can never
// be entered for any input.
const (
	emacsMuleAccept = -1
	emacsMuleFail   = -2
)

func emacsMuleNext(state int, c byte) int {
	switch state {
	case 0: // S0: the first byte of a character
		switch {
		case c <= 0x7F:
			return emacsMuleAccept
		case c >= 0x81 && c <= 0x8F:
			return 1
		case c >= 0x90 && c <= 0x99:
			return 2
		case c == 0x9A || c == 0x9B:
			return 4
		case c == 0x9C:
			return 5
		case c == 0x9D:
			return 6
		}
	case 1: // S1: the last C byte
		if c >= 0xA0 {
			return emacsMuleAccept
		}
	case 2: // S2: a C byte with one more to follow
		if c >= 0xA0 {
			return 1
		}
	case 4: // S4: the extension byte after 0x9A / 0x9B
		if c >= 0xE0 && c <= 0xEF {
			return 1
		}
	case 5: // S5: the extension byte after 0x9C
		if c >= 0xF0 && c <= 0xF4 {
			return 2
		}
	case 6: // S6: the extension byte after 0x9D
		if c >= 0xF5 && c <= 0xFE {
			return 2
		}
	}
	return emacsMuleFail
}

// scanEmacsMuleToken is the scrub scanner for Emacs-Mule: it runs the automaton
// over the head of b. An ill-formed head reports the maximal subpart consumed
// before the automaton failed (at least one byte, so the walk always advances),
// and a character cut off by the end of the input is ill-formed too.
func scanEmacsMuleToken(b []byte) (int, bool) {
	state := 0
	for i := 0; i < len(b); i++ {
		switch next := emacsMuleNext(state, b[i]); next {
		case emacsMuleAccept:
			return i + 1, true
		case emacsMuleFail:
			if i == 0 {
				return 1, false
			}
			return i, false // the bytes already consumed are the ill-formed subpart
		default:
			state = next
		}
	}
	return len(b), false // the input ended mid-character
}

// scrubBytesIn is rb_enc_str_scrub over raw bytes: every ill-formed token in
// encoding enc becomes the transcoding options' `replace:` string, or the
// encoding's own default replacement when none was given. String#encode's
// same-encoding path reaches for it because str_transcode0 (transcode.c) calls
// rb_enc_str_scrub there rather than building a converter. An encoding rbgo has no
// scanner for is left untouched, which is how #scrub already treats it.
func scrubBytesIn(b []byte, enc string, opts transcodeOpts) []byte {
	scan, ok := scrubScannerFor(enc)
	if !ok {
		return b
	}
	rep := scrubDefaultRepl(enc)
	if opts.hasReplace {
		rep = []byte(opts.replace)
	}
	out, _ := scrubWalk(b, scan, func([]byte) []byte { return rep })
	return out
}

func scanUTF8Token(b []byte) (int, bool) {
	r, size := utf8.DecodeRune(b)
	if r == utf8.RuneError && size == 1 {
		return illFormedUTF8Len(string(b)), false
	}
	return size, true
}

// scanASCIIToken walks US-ASCII one byte at a time; every byte ≥ 0x80 is its own
// ill-formed token, so a run of high bytes yields one replacement per byte.
func scanASCIIToken(b []byte) (int, bool) { return 1, b[0] < 0x80 }

func scanUTF16Token(b []byte, be bool) (int, bool) {
	if len(b) < 2 {
		return len(b), false // trailing partial code unit
	}
	unit := func(i int) uint16 {
		if be {
			return uint16(b[i])<<8 | uint16(b[i+1])
		}
		return uint16(b[i+1])<<8 | uint16(b[i])
	}
	u := unit(0)
	switch {
	case u >= 0xD800 && u <= 0xDBFF: // high surrogate — needs a low surrogate next
		if len(b) < 4 {
			return 2, false
		}
		if lo := unit(2); lo >= 0xDC00 && lo <= 0xDFFF {
			return 4, true
		}
		return 2, false
	case u >= 0xDC00 && u <= 0xDFFF: // unpaired low surrogate
		return 2, false
	}
	return 2, true
}

func scanUTF32Token(b []byte, be bool) (int, bool) {
	if len(b) < 4 {
		return len(b), false
	}
	var u uint32
	if be {
		u = uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	} else {
		u = uint32(b[3])<<24 | uint32(b[2])<<16 | uint32(b[1])<<8 | uint32(b[0])
	}
	if u > 0x10FFFF || (u >= 0xD800 && u <= 0xDFFF) {
		return 4, false
	}
	return 4, true
}

// scrubDefaultRepl is MRI's default scrub replacement for an encoding: U+FFFD
// encoded in the Unicode encodings, and "?" for US-ASCII.
func scrubDefaultRepl(enc string) []byte {
	switch enc {
	case "UTF-8":
		return []byte("�")
	case "UTF-16LE":
		return []byte{0xFD, 0xFF}
	case "UTF-16BE":
		return []byte{0xFF, 0xFD}
	case "UTF-32LE":
		return []byte{0xFD, 0xFF, 0x00, 0x00}
	case "UTF-32BE":
		return []byte{0x00, 0x00, 0xFF, 0xFD}
	default: // US-ASCII and other ASCII-compatible non-Unicode encodings
		return []byte("?")
	}
}

// scrubWalk copies well-formed tokens of b verbatim and replaces every ill-formed
// token with repl(token), returning the rebuilt bytes and whether anything changed.
func scrubWalk(b []byte, scan scrubScan, repl func(bad []byte) []byte) (out []byte, changed bool) {
	out = make([]byte, 0, len(b))
	for i := 0; i < len(b); {
		n, valid := scan(b[i:]) // scanners always advance by at least one byte
		if valid {
			out = append(out, b[i:i+n]...)
		} else {
			out = append(out, repl(b[i:i+n])...)
			changed = true
		}
		i += n
	}
	return out, changed
}

// scrubReplacer builds the ill-formed-token replacement callback for String#scrub
// from its argument/block, and validates an explicit replacement's encoding (a
// non-String replacement is a TypeError; one invalid in, or incompatible with, the
// receiver's encoding is an ArgumentError — both as MRI raises).
func (vm *VM) scrubReplacer(recvEnc string, args []object.Value, blk *Proc) func(bad []byte) []byte {
	if blk != nil {
		return func(bad []byte) []byte {
			r := vm.callBlock(blk, []object.Value{object.NewStringBytesEnc(append([]byte(nil), bad...), recvEnc)})
			return []byte(strArg(r))
		}
	}
	if len(args) > 0 {
		if _, isNil := args[0].(object.Nil); !isNil {
			rs, ok := args[0].(*object.String)
			if !ok {
				raise("TypeError", "no implicit conversion of %s into String", classNameOf(args[0]))
			}
			if !validInEncoding(rs.Bytes(), rs.EncName()) || (rs.EncName() != recvEnc && !asciiOnly(rs.Bytes())) {
				raise("ArgumentError", "replacement must be valid byte sequence '%s'", rs.Inspect())
			}
			replBytes := append([]byte(nil), rs.Bytes()...)
			return func(_ []byte) []byte { return replBytes }
		}
	}
	def := scrubDefaultRepl(recvEnc)
	return func(_ []byte) []byte { return def }
}

// stringScrub implements String#scrub and #scrub!. For an already-valid string it
// is a no-op (a copy for #scrub, self for #scrub!); otherwise ill-formed tokens are
// replaced. #scrub! mutates the receiver in place and returns it; #scrub returns a
// new plain String in the receiver's encoding.
func (vm *VM) stringScrub(self object.Value, args []object.Value, blk *Proc, bang bool) object.Value {
	s := self.(*object.String)
	enc := s.EncName()
	scan, ok := scrubScannerFor(enc)
	if !ok || validInEncoding(s.Bytes(), enc) {
		if bang {
			return s // already valid (or unscrubbable): no modification, no frozen check
		}
		return object.NewStringBytesEnc(append([]byte(nil), s.Bytes()...), s.Enc)
	}
	repl := vm.scrubReplacer(enc, args, blk)
	out, _ := scrubWalk(s.Bytes(), scan, repl)
	if bang {
		vm.checkFrozen(s)
		s.SetBytes(out)
		return s
	}
	return object.NewStringBytesEnc(out, s.Enc)
}
