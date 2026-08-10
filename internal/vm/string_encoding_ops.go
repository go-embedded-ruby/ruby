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
	}
	return nil, false
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
		checkFrozen(s)
		s.SetBytes(out)
		return s
	}
	return object.NewStringBytesEnc(out, s.Enc)
}
