package vm

import (
	binpkg "encoding/binary"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerPackUnpack installs Array#pack and String#unpack/#unpack1, supporting
// the common format directives with MRI-compatible semantics:
//
//	C/c          unsigned/signed 8-bit
//	S/s L/l Q/q  native-endian (little) unsigned/signed 16/32/64-bit
//	n/N          big-endian unsigned 16/32-bit
//	v/V          little-endian unsigned 16/32-bit
//	a/A/Z        binary string (null-padded / space-padded / null-terminated)
//	H/h          hex string, high/low nibble first
//	U            UTF-8 character / codepoint
//
// Each directive takes an optional count N, or '*' for "all remaining"; spaces
// in the format are ignored.
func (vm *VM) registerPackUnpack() {
	vm.cArray.define("pack", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		elems := object.Kind[*object.Array](self).Elems
		fmtStr := packFormat(args)
		return object.NewStringBytesEnc(packBytes(elems, fmtStr), "ASCII-8BIT")
	})
	vm.cString.define("unpack", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		data := object.Kind[*object.String](self).Bytes()
		return object.NewArrayFromSlice(unpackElems(data, packFormat(args)))
	})
	vm.cString.define("unpack1", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		data := object.Kind[*object.String](self).Bytes()
		elems := unpackElems(data, packFormat(args))
		if len(elems) == 0 {
			return object.NilV
		}
		return elems[0]
	})
}

// packFormat extracts the mandatory String format argument.
func packFormat(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	s, ok := object.KindOK[*object.String](args[0])
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(args[0]))
	}
	return string(s.Bytes())
}

// packDir is one parsed directive: its letter, a count, and whether the count
// was the '*' wildcard ("all remaining").
type packDir struct {
	code  byte
	count int
	star  bool
}

// parseFormat splits a pack/unpack format string into directives. A directive is
// a single letter optionally followed by a decimal count or '*'; with no count
// the directive applies once. Spaces are ignored; any other character raises.
func parseFormat(fmtStr string) []packDir {
	var dirs []packDir
	i := 0
	for i < len(fmtStr) {
		c := fmtStr[i]
		if c == ' ' {
			i++
			continue
		}
		if !isPackCode(c) {
			raise("ArgumentError", "unknown pack directive '%c' in '%s'", c, fmtStr)
		}
		i++
		d := packDir{code: c, count: 1}
		switch {
		case i < len(fmtStr) && fmtStr[i] == '*':
			d.star = true
			i++
		case i < len(fmtStr) && fmtStr[i] >= '0' && fmtStr[i] <= '9':
			n := 0
			for i < len(fmtStr) && fmtStr[i] >= '0' && fmtStr[i] <= '9' {
				n = n*10 + int(fmtStr[i]-'0')
				i++
			}
			d.count = n
		}
		dirs = append(dirs, d)
	}
	return dirs
}

// isPackCode reports whether c is a supported directive letter.
func isPackCode(c byte) bool {
	switch c {
	case 'C', 'c', 'S', 's', 'L', 'l', 'Q', 'q', 'n', 'N', 'v', 'V',
		'a', 'A', 'Z', 'H', 'h', 'U':
		return true
	}
	return false
}

// intWidth returns the byte width of an integer directive (0 for non-integer).
func intWidth(code byte) int {
	switch code {
	case 'C', 'c':
		return 1
	case 'S', 's', 'n', 'v':
		return 2
	case 'L', 'l', 'N', 'V':
		return 4
	case 'Q', 'q':
		return 8
	}
	return 0
}

// putInt appends a width-byte encoding of v in the directive's endianness. It is
// only called with an integer directive (intWidth(code) > 0); the final case is
// the catch-all for the 64-bit codes.
func putInt(out []byte, code byte, v uint64) []byte {
	var buf [8]byte
	switch code {
	case 'C', 'c':
		return append(out, byte(v))
	case 'S', 's', 'v':
		binpkg.LittleEndian.PutUint16(buf[:], uint16(v))
		return append(out, buf[:2]...)
	case 'L', 'l', 'V':
		binpkg.LittleEndian.PutUint32(buf[:], uint32(v))
		return append(out, buf[:4]...)
	case 'n':
		binpkg.BigEndian.PutUint16(buf[:], uint16(v))
		return append(out, buf[:2]...)
	case 'N':
		binpkg.BigEndian.PutUint32(buf[:], uint32(v))
		return append(out, buf[:4]...)
	default: // 'Q', 'q'
		binpkg.LittleEndian.PutUint64(buf[:], v)
		return append(out, buf[:8]...)
	}
}

// getInt decodes a width-byte integer from b in the directive's endianness,
// sign-extending the signed directives. It is only called with an integer
// directive; the final case is the catch-all for the unsigned 64-bit codes.
func getInt(b []byte, code byte) int64 {
	switch code {
	case 'C':
		return int64(b[0])
	case 'c':
		return int64(int8(b[0]))
	case 'S', 'v':
		return int64(binpkg.LittleEndian.Uint16(b))
	case 's':
		return int64(int16(binpkg.LittleEndian.Uint16(b)))
	case 'L', 'V':
		return int64(binpkg.LittleEndian.Uint32(b))
	case 'l':
		return int64(int32(binpkg.LittleEndian.Uint32(b)))
	case 'n':
		return int64(binpkg.BigEndian.Uint16(b))
	case 'N':
		return int64(binpkg.BigEndian.Uint32(b))
	default: // 'Q', 'q'
		return int64(binpkg.LittleEndian.Uint64(b))
	}
}

// packBytes serialises elems according to dirs.
func packBytes(elems []object.Value, fmtStr string) []byte {
	dirs := parseFormat(fmtStr)
	out := []byte{}
	idx := 0
	next := func() object.Value {
		if idx >= len(elems) {
			raise("ArgumentError", "too few arguments")
		}
		v := elems[idx]
		idx++
		return v
	}
	for _, d := range dirs {
		switch {
		case intWidth(d.code) > 0:
			count := d.count
			if d.star {
				count = len(elems) - idx
			}
			for k := 0; k < count; k++ {
				out = putInt(out, d.code, uint64(toInt(next())))
			}
		case d.code == 'U':
			count := d.count
			if d.star {
				count = len(elems) - idx
			}
			for k := 0; k < count; k++ {
				out = utf8.AppendRune(out, rune(toInt(next())))
			}
		case d.code == 'a' || d.code == 'A' || d.code == 'Z':
			out = packString(out, d, packStrArg(next()))
		case d.code == 'H' || d.code == 'h':
			out = packHex(out, d, packStrArg(next()))
		}
	}
	return out
}

// packStrArg returns the String argument's bytes for an a/A/Z/H/h directive.
func packStrArg(v object.Value) []byte {
	s, ok := object.KindOK[*object.String](v)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	}
	return s.Bytes()
}

// packString implements the a/A/Z directives: pad/truncate to the count (with
// NUL for a/Z, space for A); '*' takes the whole string (Z then appends one NUL).
func packString(out []byte, d packDir, b []byte) []byte {
	pad := byte(0)
	if d.code == 'A' {
		pad = ' '
	}
	if d.star {
		out = append(out, b...)
		if d.code == 'Z' {
			out = append(out, 0)
		}
		return out
	}
	n := d.count
	if len(b) >= n {
		return append(out, b[:n]...)
	}
	out = append(out, b...)
	for i := len(b); i < n; i++ {
		out = append(out, pad)
	}
	return out
}

// packHex implements the H/h directives: each nibble of the count consumes one
// hex character (default count 1; '*' = the whole string). H is high-nibble
// first, h is low-nibble first.
func packHex(out []byte, d packDir, b []byte) []byte {
	n := d.count
	if d.star {
		n = len(b)
	}
	var cur byte
	for i := 0; i < n; i++ {
		var nib byte
		if i < len(b) {
			nib = hexNibble(b[i])
		}
		if i%2 == 0 {
			if d.code == 'H' {
				cur = nib << 4
			} else {
				cur = nib
			}
		} else {
			if d.code == 'H' {
				cur |= nib
			} else {
				cur |= nib << 4
			}
			out = append(out, cur)
			cur = 0
		}
	}
	if n%2 == 1 {
		out = append(out, cur)
	}
	return out
}

// hexNibble decodes one hex character to a 4-bit nibble, matching MRI's lenient
// pack formula: a letter contributes (c & 15) + 9 (so 'a'/'A'..'f'/'F' map to
// 10..15), any other byte contributes c & 15.
func hexNibble(c byte) byte {
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
		return (c&15 + 9) & 0x0f
	}
	return c & 0x0f
}

// unpackElems deserialises data according to fmtStr.
func unpackElems(data []byte, fmtStr string) []object.Value {
	dirs := parseFormat(fmtStr)
	var out []object.Value
	pos := 0
	for _, d := range dirs {
		switch {
		case intWidth(d.code) > 0:
			w := intWidth(d.code)
			count := d.count
			if d.star {
				count = (len(data) - pos) / w
			}
			for k := 0; k < count; k++ {
				if pos+w > len(data) {
					out = append(out, object.NilV)
					continue
				}
				out = append(out, object.IntValue(getInt(data[pos:pos+w], d.code)))
				pos += w
			}
		case d.code == 'U':
			count := d.count
			if d.star {
				count = -1
			}
			for k := 0; count < 0 || k < count; k++ {
				if pos >= len(data) {
					break
				}
				r, sz := utf8.DecodeRune(data[pos:])
				out = append(out, object.IntValue(int64(r)))
				pos += sz
			}
		case d.code == 'a' || d.code == 'A' || d.code == 'Z':
			var seg []byte
			if d.star {
				seg = data[pos:]
				pos = len(data)
			} else {
				end := pos + d.count
				if end > len(data) {
					end = len(data)
				}
				seg = data[pos:end]
				pos = end
			}
			out = append(out, object.NewString(unpackString(seg, d.code)))
		case d.code == 'H' || d.code == 'h':
			n := d.count
			avail := (len(data) - pos) * 2
			if d.star || n > avail {
				n = avail
			}
			out = append(out, object.NewString(unpackHex(data[pos:], d.code, n)))
			pos += (n + 1) / 2
		}
	}
	return out
}

// unpackString applies the a/A/Z trailing-trim rules: a keeps everything, A
// strips trailing spaces and NULs, Z stops at the first NUL.
func unpackString(seg []byte, code byte) string {
	switch code {
	case 'A':
		end := len(seg)
		for end > 0 && (seg[end-1] == ' ' || seg[end-1] == 0) {
			end--
		}
		return string(seg[:end])
	case 'Z':
		for i, c := range seg {
			if c == 0 {
				return string(seg[:i])
			}
		}
		return string(seg)
	}
	return string(seg)
}

// unpackHex reads n hex nibbles from data (H high-first, h low-first).
func unpackHex(data []byte, code byte, n int) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		b := data[i/2]
		var nib byte
		if code == 'H' {
			if i%2 == 0 {
				nib = b >> 4
			} else {
				nib = b & 0x0f
			}
		} else {
			if i%2 == 0 {
				nib = b & 0x0f
			} else {
				nib = b >> 4
			}
		}
		out = append(out, hexdigits[nib])
	}
	return string(out)
}
