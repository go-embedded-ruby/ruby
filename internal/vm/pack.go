package vm

import (
	binpkg "encoding/binary"
	"math"
	"math/big"
	"sync"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// ptrSpec is the native-width encoding of a machine pointer, shared by the P/p
// directives (an 8-byte native-endian word on the LP64 platforms rbgo targets).
var ptrSpec = intSpec{8, binpkg.NativeEndian, false}

// packPtrTable backs the P/p (pointer) directives. A real machine pointer cannot
// round-trip through pure Go, so pack registers the referenced string under a
// synthetic, monotonically increasing id and emits that id as a native-width
// word; unpack looks the string back up. Ids are never reused (so a value packed
// and later unpacked resolves), and a null pointer is the reserved id 0.
var packPtrTable = struct {
	mu   sync.Mutex
	m    map[uint64]*object.String
	next uint64
}{m: map[uint64]*object.String{}}

func registerPackPtr(s *object.String) uint64 {
	packPtrTable.mu.Lock()
	defer packPtrTable.mu.Unlock()
	packPtrTable.next++
	packPtrTable.m[packPtrTable.next] = s
	return packPtrTable.next
}

func lookupPackPtr(id uint64) (*object.String, bool) {
	packPtrTable.mu.Lock()
	defer packPtrTable.mu.Unlock()
	s, ok := packPtrTable.m[id]
	return s, ok
}

// registerPackUnpack installs Array#pack and String#unpack/#unpack1, supporting
// the common format directives with MRI-compatible semantics:
//
//	C/c          unsigned/signed 8-bit
//	S/s L/l Q/q  native-endian unsigned/signed 16/32/64-bit
//	I/i          native-endian unsigned/signed C int (32-bit)
//	J/j          native-endian unsigned/signed intptr_t (64-bit)
//	n/N          big-endian unsigned 16/32-bit
//	v/V          little-endian unsigned 16/32-bit
//	a/A/Z        binary string (null-padded / space-padded / null-terminated)
//	H/h          hex string, high/low nibble first
//	b/B          bit string, low/high bit first
//	m            base64-encoded string (RFC 2045 / RFC 4648, count-0 variant)
//	M            quoted-printable-encoded string
//	U            UTF-8 character / codepoint
//
// The integer directives s/S/i/I/l/L/q/Q/j/J accept the byte-order modifiers
// '<' (little-endian) and '>' (big-endian) and the native-size modifiers '!'
// and '_' (which widen 'l'/'L' from a fixed 32-bit int to the platform's C
// long, i.e. 64-bit on LP64). Each directive takes an optional count N, or '*'
// for "all remaining"; spaces in the format are ignored.
func (vm *VM) registerPackUnpack() {
	vm.cArray.define("pack", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		elems := self.(*object.Array).Elems
		// The buffer: keyword (a trailing kwargs Hash) directs the packed bytes into
		// an existing String, which is mutated and returned. Packing begins at the
		// buffer's current end (an @ directive then repositions absolutely), so the
		// result keeps the buffer's leading content and encoding.
		buf := vm.packBuffer(args)
		var initial []byte
		if buf != nil {
			vm.checkFrozen(buf)
			initial = buf.Bytes()
		}
		out, enc := vm.packBytes(elems, vm.packFormat(args), initial)
		if buf != nil {
			buf.SetBytes(out)
			return buf
		}
		return object.NewStringBytesEnc(out, enc)
	})
	vm.cString.define("unpack", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		data := self.(*object.String).Bytes()
		return object.NewArrayFromSlice(unpackElems(data, vm.packFormat(args)))
	})
	vm.cString.define("unpack1", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		data := self.(*object.String).Bytes()
		elems := unpackElems(data, vm.packFormat(args))
		if len(elems) == 0 {
			return object.NilV
		}
		return elems[0]
	})
}

// packFormat extracts the mandatory format argument, which is a String or any
// object that responds to #to_str.
func (vm *VM) packFormat(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	v := args[0]
	if s, ok := v.(*object.String); ok {
		return string(s.Bytes())
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return string(s.Bytes())
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	return ""
}

// packBuffer returns the String named by Array#pack's buffer: keyword (a trailing
// kwargs Hash), or nil when the option is absent. A non-String buffer is a
// TypeError. packFormat still reads the format from args[0]; the trailing Hash it
// simply ignores.
func (vm *VM) packBuffer(args []object.Value) *object.String {
	h := trailingKwHash(args)
	if h == nil {
		return nil
	}
	v, ok := h.Get(object.Symbol("buffer"))
	if !ok {
		return nil
	}
	s, isStr := v.(*object.String)
	if !isStr {
		raise("TypeError", "buffer must be String, not %s", classNameOf(v))
	}
	return s
}

// packDir is one parsed directive: its letter, a count, whether the count was
// the '*' wildcard ("all remaining"), and the byte-order / native-size
// modifiers that followed the letter.
type packDir struct {
	code     byte
	count    int
	star     bool
	explicit bool // an explicit decimal count was given (distinguishes '@' default)
	little   bool // '<' modifier
	big      bool // '>' modifier
	bang     bool // '!' or '_' modifier
}

// parseFormat splits a pack/unpack format string into directives. A directive
// is a single letter optionally followed by any of the modifiers '<', '>', '!'
// and '_' (only on the integer directives sSiIlLqQjJ) and then a decimal count
// or '*'; with no count the directive applies once. Spaces are ignored; any
// other character raises. verb is "pack" or "unpack" for the error message.
func parseFormat(fmtStr, verb string) []packDir {
	var dirs []packDir
	i := 0
	for i < len(fmtStr) {
		c := fmtStr[i]
		if isPackSpace(c) {
			i++
			continue
		}
		if c == '#' { // comment: skip to end of line
			for i < len(fmtStr) && fmtStr[i] != '\n' {
				i++
			}
			continue
		}
		if !isPackCode(c) {
			raise("ArgumentError", "unknown %s directive '%c' in '%s'", verb, c, fmtStr)
		}
		i++
		d := packDir{code: c, count: 1}
		// Consume any run of byte-order / native-size modifiers.
		for i < len(fmtStr) {
			switch fmtStr[i] {
			case '<':
				d.little = true
			case '>':
				d.big = true
			case '!', '_':
				d.bang = true
			default:
				goto modsDone
			}
			i++
		}
	modsDone:
		if (d.little || d.big || d.bang) && !modifiableInt(c) {
			raise("ArgumentError", "'%c' allowed only after types sSiIlLqQjJ", firstMod(d))
		}
		if d.little && d.big {
			raise("ArgumentError", "can't use both '<' and '>'")
		}
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
			d.explicit = true
		}
		dirs = append(dirs, d)
	}
	return dirs
}

// firstMod returns the modifier character to name in an "allowed only after"
// error, preferring the byte-order modifiers.
func firstMod(d packDir) byte {
	switch {
	case d.little:
		return '<'
	case d.big:
		return '>'
	default:
		return '!'
	}
}

// isPackSpace reports whether c is whitespace ignored in a format string,
// matching MRI's use of the C ISSPACE classification.
func isPackSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// isPackCode reports whether c is a supported directive letter.
func isPackCode(c byte) bool {
	switch c {
	case 'C', 'c', 'S', 's', 'L', 'l', 'Q', 'q', 'I', 'i', 'J', 'j',
		'n', 'N', 'v', 'V', 'a', 'A', 'Z', 'H', 'h', 'U',
		'f', 'F', 'd', 'D', 'e', 'E', 'g', 'G',
		'x', 'X', '@', 'b', 'B', 'm', 'M', 'u', 'w', 'P', 'p':
		return true
	}
	return false
}

// floatSpec resolves a float directive to its byte width and order, or reports
// ok=false for a non-float directive. MRI: f/F single & d/D double in native
// order; e/E little-endian; g/G big-endian.
func floatSpec(d packDir) (width int, bo binpkg.ByteOrder, ok bool) {
	switch d.code {
	case 'f', 'F':
		return 4, binpkg.NativeEndian, true
	case 'e':
		return 4, binpkg.LittleEndian, true
	case 'g':
		return 4, binpkg.BigEndian, true
	case 'd', 'D':
		return 8, binpkg.NativeEndian, true
	case 'E':
		return 8, binpkg.LittleEndian, true
	case 'G':
		return 8, binpkg.BigEndian, true
	}
	return 0, nil, false
}

// isFloatDir reports whether d is one of the float directives f/F/d/D/e/E/g/G.
func isFloatDir(d packDir) bool {
	_, _, ok := floatSpec(d)
	return ok
}

// packFloatArg coerces a pack float argument to a float64 the way MRI's
// rb_to_float does: an Integer/Bignum/Float is used directly; any other Numeric
// (Rational/Complex/BigDecimal) is converted via #to_f; a non-Numeric (nil, a
// String, an arbitrary object) raises TypeError — pack does NOT parse numeric
// strings or call #to_f on non-Numerics.
func (vm *VM) packFloatArg(v object.Value) float64 {
	switch n := v.(type) {
	case object.Float:
		return float64(n)
	case object.Integer:
		return float64(int64(n))
	case *object.Bignum:
		f, _ := new(big.Float).SetInt(n.I).Float64()
		return f
	}
	if isNumericValue(v) {
		if f, ok := vm.send(v, "to_f", nil, nil).(object.Float); ok {
			return float64(f)
		}
	}
	raise("TypeError", "can't convert %s into Float", classNameOf(v))
	return 0
}

// modifiableInt reports whether c accepts the '<'/'>'/'!'/'_' modifiers.
func modifiableInt(c byte) bool {
	switch c {
	case 'S', 's', 'I', 'i', 'L', 'l', 'Q', 'q', 'J', 'j':
		return true
	}
	return false
}

// intSpec describes how an integer directive is encoded: its byte width, byte
// order, and whether it is signed (for unpack sign extension).
type intSpec struct {
	width  int
	bo     binpkg.ByteOrder
	signed bool
}

// intSpecFor resolves the concrete integer encoding for a directive, or reports
// ok=false when the directive is not an integer directive.
func intSpecFor(d packDir) (intSpec, bool) {
	var s intSpec
	switch d.code {
	case 'C':
		s = intSpec{1, binpkg.NativeEndian, false}
	case 'c':
		s = intSpec{1, binpkg.NativeEndian, true}
	case 'S':
		s = intSpec{2, binpkg.NativeEndian, false}
	case 's':
		s = intSpec{2, binpkg.NativeEndian, true}
	case 'I':
		s = intSpec{4, binpkg.NativeEndian, false}
	case 'i':
		s = intSpec{4, binpkg.NativeEndian, true}
	case 'L':
		s = intSpec{4, binpkg.NativeEndian, false}
	case 'l':
		s = intSpec{4, binpkg.NativeEndian, true}
	case 'Q':
		s = intSpec{8, binpkg.NativeEndian, false}
	case 'q':
		s = intSpec{8, binpkg.NativeEndian, true}
	case 'J':
		s = intSpec{8, binpkg.NativeEndian, false}
	case 'j':
		s = intSpec{8, binpkg.NativeEndian, true}
	case 'n':
		s = intSpec{2, binpkg.BigEndian, false}
	case 'N':
		s = intSpec{4, binpkg.BigEndian, false}
	case 'v':
		s = intSpec{2, binpkg.LittleEndian, false}
	case 'V':
		s = intSpec{4, binpkg.LittleEndian, false}
	default:
		return s, false
	}
	// '!' / '_' select the platform's C long for l/L (64-bit on LP64); the
	// other integer directives already have their native width.
	if d.bang && (d.code == 'L' || d.code == 'l') {
		s.width = 8
	}
	// '<' / '>' override the byte order.
	if d.little {
		s.bo = binpkg.LittleEndian
	}
	if d.big {
		s.bo = binpkg.BigEndian
	}
	return s, true
}

// putUint appends the low width bytes of v to out in the given byte order.
func putUint(out []byte, v uint64, s intSpec) []byte {
	var buf [8]byte
	switch s.width {
	case 1:
		return append(out, byte(v))
	case 2:
		s.bo.PutUint16(buf[:], uint16(v))
		return append(out, buf[:2]...)
	case 4:
		s.bo.PutUint32(buf[:], uint32(v))
		return append(out, buf[:4]...)
	default: // 8
		s.bo.PutUint64(buf[:], v)
		return append(out, buf[:8]...)
	}
}

// getUint decodes width bytes from b as an unsigned integer in the given byte
// order.
func getUint(b []byte, s intSpec) uint64 {
	switch s.width {
	case 1:
		return uint64(b[0])
	case 2:
		return uint64(s.bo.Uint16(b))
	case 4:
		return uint64(s.bo.Uint32(b))
	default: // 8
		return s.bo.Uint64(b)
	}
}

// intResult converts a raw width-byte unsigned value into the Ruby Integer the
// directive decodes to: signed directives sign-extend to a (possibly negative)
// Integer; unsigned 64-bit values above 2**63 become a Bignum.
func intResult(u uint64, s intSpec) object.Value {
	if s.signed {
		shift := uint(64 - s.width*8)
		return object.IntValue(int64(u<<shift) >> shift)
	}
	if s.width == 8 {
		return object.NormInt(new(big.Int).SetUint64(u))
	}
	return object.IntValue(int64(u))
}

// packMask64 masks off the low 64 bits of a big.Int (two's complement for
// negatives, matching MRI's "least significant bits" packing).
var packMask64 = new(big.Int).SetUint64(^uint64(0))

// packIntArg coerces a pack integer argument to its low 64 bits, accepting
// Integer, Bignum, Float (truncated toward zero) and any object implementing
// #to_int.
func (vm *VM) packIntArg(v object.Value) uint64 {
	switch n := v.(type) {
	case object.Integer:
		return uint64(int64(n))
	case *object.Bignum:
		return new(big.Int).And(n.I, packMask64).Uint64()
	case object.Float:
		z, _ := big.NewFloat(float64(n)).Int(nil)
		return new(big.Int).And(z, packMask64).Uint64()
	}
	if vm.respondsToDynamic(v, "to_int") {
		return vm.packIntArg(vm.send(v, "to_int", nil, nil))
	}
	raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(v))
	return 0
}

// packUnicodeArg coerces a 'U' (Unicode codepoint) pack argument to a codepoint
// in the range MRI's UTF-8 packer accepts: an Integer is used directly, any other
// object is converted through #to_int (which must return an Integer, else
// TypeError). A negative value or one above 0x7FFFFFFF is out of range (RangeError).
func (vm *VM) packUnicodeArg(v object.Value) uint32 {
	switch n := v.(type) {
	case object.Integer:
		if n < 0 || int64(n) > 0x7FFFFFFF {
			raise("RangeError", "pack(U): value out of range")
		}
		return uint32(n)
	case *object.Bignum:
		// A Bignum is always outside int64, hence far above 0x7FFFFFFF (or below 0).
		raise("RangeError", "pack(U): value out of range")
	}
	if vm.respondsToDynamic(v, "to_int") {
		r := vm.send(v, "to_int", nil, nil)
		switch r.(type) {
		case object.Integer, *object.Bignum:
			return vm.packUnicodeArg(r)
		}
		raise("TypeError", "can't convert %s to Integer (%s#to_int gives %s)",
			classNameOf(v), classNameOf(v), classNameOf(r))
	}
	raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(v))
	return 0
}

// appendPackU appends the MRI extended-UTF-8 encoding of a codepoint (up to
// 0x7FFFFFFF, six bytes) to out. Unlike Go's utf8.AppendRune it does not clamp at
// U+10FFFF, so [0x110000].pack("U") yields the raw four-byte form MRI produces.
func appendPackU(out []byte, uv uint32) []byte {
	switch {
	case uv <= 0x7f:
		return append(out, byte(uv))
	case uv <= 0x7ff:
		return append(out, byte(0xc0|(uv>>6)), byte(0x80|(uv&0x3f)))
	case uv <= 0xffff:
		return append(out, byte(0xe0|(uv>>12)), byte(0x80|((uv>>6)&0x3f)), byte(0x80|(uv&0x3f)))
	case uv <= 0x1fffff:
		return append(out, byte(0xf0|(uv>>18)), byte(0x80|((uv>>12)&0x3f)),
			byte(0x80|((uv>>6)&0x3f)), byte(0x80|(uv&0x3f)))
	case uv <= 0x3ffffff:
		return append(out, byte(0xf8|(uv>>24)), byte(0x80|((uv>>18)&0x3f)),
			byte(0x80|((uv>>12)&0x3f)), byte(0x80|((uv>>6)&0x3f)), byte(0x80|(uv&0x3f)))
	default: // uv <= 0x7fffffff
		return append(out, byte(0xfc|(uv>>30)), byte(0x80|((uv>>24)&0x3f)),
			byte(0x80|((uv>>18)&0x3f)), byte(0x80|((uv>>12)&0x3f)),
			byte(0x80|((uv>>6)&0x3f)), byte(0x80|(uv&0x3f)))
	}
}

// packBytes serialises elems according to fmtStr.
func (vm *VM) packBytes(elems []object.Value, fmtStr string, initial []byte) ([]byte, string) {
	dirs := parseFormat(fmtStr, "pack")
	out := append([]byte{}, initial...)
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
		case isIntDir(d):
			s, _ := intSpecFor(d)
			count := d.count
			if d.star {
				count = len(elems) - idx
			}
			for k := 0; k < count; k++ {
				out = putUint(out, vm.packIntArg(next()), s)
			}
		case isFloatDir(d):
			w, bo, _ := floatSpec(d)
			count := d.count
			if d.star {
				count = len(elems) - idx
			}
			for k := 0; k < count; k++ {
				f := vm.packFloatArg(next())
				if w == 4 {
					out = putUint(out, uint64(math.Float32bits(float32(f))), intSpec{4, bo, false})
				} else {
					out = putUint(out, math.Float64bits(f), intSpec{8, bo, false})
				}
			}
		case d.code == 'U':
			count := d.count
			if d.star {
				count = len(elems) - idx
			}
			for k := 0; k < count; k++ {
				out = appendPackU(out, vm.packUnicodeArg(next()))
			}
		case d.code == 'x':
			n := d.count
			if d.star {
				n = 0
			}
			for k := 0; k < n; k++ {
				out = append(out, 0)
			}
		case d.code == 'X':
			n := d.count
			if d.star {
				n = 0
			}
			if n > len(out) {
				raise("ArgumentError", "X outside of string")
			}
			out = out[:len(out)-n]
		case d.code == '@':
			n := d.count // implicit count of one when unspecified
			if d.star {
				n = len(out)
			}
			if n <= len(out) {
				out = out[:n]
			} else {
				for len(out) < n {
					out = append(out, 0)
				}
			}
		case d.code == 'a' || d.code == 'A' || d.code == 'Z':
			// A nil argument packs as an empty string (padded with spaces for A,
			// NULs for a/Z), matching MRI — it is not coerced via #to_str.
			arg := next()
			var b []byte
			if !object.IsNil(arg) {
				b = vm.packStrArg(arg)
			}
			out = packString(out, d, b)
		case d.code == 'H' || d.code == 'h':
			out = packHex(out, d, vm.packStrArg(next()))
		case d.code == 'B' || d.code == 'b':
			out = packBits(out, d, vm.packStrArg(next()))
		case d.code == 'm':
			out = packBase64(out, d, vm.packStrArg(next()))
		case d.code == 'M':
			out = qpencode(out, []byte(vm.displayStr(next())), packQPLen(d))
		case d.code == 'u':
			out = packUuencode(out, d, vm.packStrArg(next()))
		case d.code == 'w':
			count := d.count
			if d.star {
				count = len(elems) - idx
			}
			for k := 0; k < count; k++ {
				z := vm.packBERArg(next())
				if z.Sign() < 0 {
					raise("ArgumentError", "can't compress negative numbers")
				}
				out = packBER(out, z)
			}
		case d.code == 'P' || d.code == 'p':
			// Pointer: emit a native-width word standing in for a pointer to the
			// argument string (nil is a null pointer). The referenced string is
			// registered so a later unpack("P"/"p") can recover it.
			arg := next()
			var id uint64
			if !object.IsNil(arg) {
				b := vm.packStrArg(arg)
				id = registerPackPtr(object.NewStringBytesEnc(append([]byte(nil), b...), "ASCII-8BIT"))
			}
			out = putUint(out, id, ptrSpec)
		}
	}
	return out, packEncoding(dirs)
}

// packEncoding computes the encoding MRI associates with a pack result. It
// starts optimistic at US-ASCII; a 'U' directive upgrades that to UTF-8; the
// base64/quoted-printable/uuencode directives 'm'/'M'/'u' keep it (they emit
// only ASCII); every other directive can emit arbitrary bytes and so drops the
// result to ASCII-8BIT (BINARY).
func packEncoding(dirs []packDir) string {
	enc := 1 // 1 = US-ASCII, 2 = UTF-8, 0 = ASCII-8BIT
	for _, d := range dirs {
		switch d.code {
		case 'U':
			if enc == 1 {
				enc = 2
			}
		case 'm', 'M', 'u':
			// keep the current encoding
		default:
			enc = 0
		}
	}
	switch enc {
	case 1:
		return "US-ASCII"
	case 2:
		return "UTF-8"
	default:
		return "ASCII-8BIT"
	}
}

// isIntDir reports whether d is one of the integer directives.
func isIntDir(d packDir) bool {
	_, ok := intSpecFor(d)
	return ok
}

// packStrArg returns the String argument's bytes for an a/A/Z/H/h directive,
// coercing a non-String argument via #to_str.
func (vm *VM) packStrArg(v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return s.Bytes()
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	return nil
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

// packBits implements the B/b directives: each of the count characters (default
// 1; '*' = the whole string) contributes its low bit, packed MSB-first (B) or
// LSB-first (b) into successive bytes.
func packBits(out []byte, d packDir, b []byte) []byte {
	n := d.count
	if d.star {
		n = len(b)
	}
	var cur byte
	for i := 0; i < n; i++ {
		var bit byte
		if i < len(b) {
			bit = b[i] & 1
		}
		if d.code == 'B' { // most-significant bit first
			cur |= bit << uint(7-i%8)
		} else { // b: least-significant bit first
			cur |= bit << uint(i%8)
		}
		if i%8 == 7 {
			out = append(out, cur)
			cur = 0
		}
	}
	if n%8 != 0 {
		out = append(out, cur)
	}
	return out
}

// unpackBits reads n bits from data as a "0"/"1" string, MSB-first (B) or
// LSB-first (b).
func unpackBits(data []byte, code byte, n int) string {
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		b := data[i/8]
		var bit byte
		if code == 'B' {
			bit = (b >> uint(7-i%8)) & 1
		} else {
			bit = (b >> uint(i%8)) & 1
		}
		out = append(out, '0'+bit)
	}
	return string(out)
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

// b64Table is the MRI base64 alphabet used by the 'm' directive.
const b64Table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// b64Rev maps a byte to its base64 value, or -1 when it is not a base64
// character. Built once from b64Table.
var b64Rev = func() [256]int {
	var t [256]int
	for i := range t {
		t[i] = -1
	}
	for i := 0; i < len(b64Table); i++ {
		t[b64Table[i]] = i
	}
	return t
}()

// b64val returns c's base64 value, or -1 when c is not a base64 character.
func b64val(c byte) int { return b64Rev[c] }

// b64EncodeChunk appends the base64 encoding of the ≤3-byte chunk s to out,
// with '=' padding for a final short group and, when tailLF, a trailing newline
// — matching MRI's encodes() for the 'm' directive.
func b64EncodeChunk(out, s []byte, tailLF bool) []byte {
	i := 0
	n := len(s)
	for i+3 <= n {
		c0, c1, c2 := s[i], s[i+1], s[i+2]
		out = append(out,
			b64Table[(c0>>2)&0x3f],
			b64Table[((c0<<4)&0x30)|((c1>>4)&0x0f)],
			b64Table[((c1<<2)&0x3c)|((c2>>6)&0x03)],
			b64Table[c2&0x3f])
		i += 3
	}
	switch n - i {
	case 2:
		c0, c1 := s[i], s[i+1]
		out = append(out,
			b64Table[(c0>>2)&0x3f],
			b64Table[((c0<<4)&0x30)|((c1>>4)&0x0f)],
			b64Table[(c1<<2)&0x3c],
			'=')
	case 1:
		c0 := s[i]
		out = append(out,
			b64Table[(c0>>2)&0x3f],
			b64Table[(c0<<4)&0x30],
			'=', '=')
	}
	if tailLF {
		out = append(out, '\n')
	}
	return out
}

// packBase64 implements Array#pack's 'm' directive. With an explicit count of 0
// the whole string is encoded on a single line with no trailing newline;
// otherwise the count sets the number of input bytes per line (defaulting to 45
// for a count of 0<count≤2, '*', or no count, and rounded down to a multiple of
// 3 above that), each line terminated by a newline.
func packBase64(out []byte, d packDir, b []byte) []byte {
	if d.explicit && d.count == 0 {
		return b64EncodeChunk(out, b, false)
	}
	length := d.count
	if length <= 2 {
		length = 45
	} else {
		length = length / 3 * 3
	}
	for len(b) > 0 {
		todo := length
		if len(b) < todo {
			todo = len(b)
		}
		out = b64EncodeChunk(out, b[:todo], true)
		b = b[todo:]
	}
	return out
}

// packQPLen resolves the maximum line length for the 'M' (quoted-printable)
// directive: a count of 0, 1, '*', or no count means 72; otherwise the count.
func packQPLen(d packDir) int {
	length := d.count
	if length <= 1 {
		length = 72
	}
	return length
}

// hexUpper is the uppercase hex alphabet used by quoted-printable encoding.
const hexUpper = "0123456789ABCDEF"

// qpencode appends the quoted-printable encoding of from to out, wrapping lines
// with a "=\n" soft break once a line would exceed length characters. It mirrors
// MRI's qpencode(): bytes >126, control bytes other than tab/newline, and '='
// are emitted as "=XX"; a trailing space or tab before a literal newline is
// protected with "="; and a soft break is appended at the end unless the last
// line is empty.
func qpencode(out, from []byte, length int) []byte {
	n := 0
	prev := -1
	for _, s := range from {
		switch {
		case s > 126 || (s < 32 && s != '\n' && s != '\t') || s == '=':
			out = append(out, '=', hexUpper[s>>4], hexUpper[s&0x0f])
			n += 3
			prev = -1
		case s == '\n':
			if prev == ' ' || prev == '\t' {
				out = append(out, '=', s)
			}
			out = append(out, s)
			n = 0
			prev = int(s)
		default:
			out = append(out, s)
			n++
			prev = int(s)
		}
		if n > length {
			out = append(out, '=', '\n')
			n = 0
			prev = '\n'
		}
	}
	if n > 0 {
		out = append(out, '=', '\n')
	}
	return out
}

// hex2num decodes a single hex digit to its value, or -1 for a non-hex byte.
func hex2num(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// base64Strict decodes s as strict base64 (the count-0 form of the 'm'
// directive), reporting ok=false on any invalid character or malformed padding.
func base64Strict(s []byte) ([]byte, bool) {
	var out []byte
	pos, end := 0, len(s)
	a, b, c, dd := -1, -1, 0, 0
	for pos < end {
		a, b, c, dd = -1, -1, -1, -1
		a = b64val(s[pos])
		pos++
		if pos >= end || a == -1 {
			return nil, false
		}
		b = b64val(s[pos])
		pos++
		if pos >= end || b == -1 {
			return nil, false
		}
		if s[pos] == '=' {
			if pos+2 == end && s[pos+1] == '=' {
				break
			}
			return nil, false
		}
		c = b64val(s[pos])
		pos++
		if pos >= end || c == -1 {
			return nil, false
		}
		if pos+1 == end && s[pos] == '=' {
			break
		}
		dd = b64val(s[pos])
		pos++
		if dd == -1 {
			return nil, false
		}
		out = append(out, byte(a<<2|b>>4), byte(b<<4|c>>2), byte(c<<6|dd))
	}
	if c == -1 {
		out = append(out, byte(a<<2|b>>4))
		if b&0xf != 0 {
			return nil, false
		}
	} else if dd == -1 {
		out = append(out, byte(a<<2|b>>4), byte(b<<4|c>>2))
		if c&0x3 != 0 {
			return nil, false
		}
	}
	return out, true
}

// base64Lenient decodes s as base64 the way the counted 'm' directive does:
// non-base64 characters are skipped and a truncated final group still yields
// its available bytes.
func base64Lenient(s []byte) []byte {
	var out []byte
	pos, end := 0, len(s)
	a, b, c, dd := -1, -1, -1, -1
	for pos < end {
		a, b, c, dd = -1, -1, -1, -1
		for pos < end {
			if a = b64val(s[pos]); a != -1 {
				break
			}
			pos++
		}
		if pos >= end {
			break
		}
		pos++
		for pos < end {
			if b = b64val(s[pos]); b != -1 {
				break
			}
			pos++
		}
		if pos >= end {
			break
		}
		pos++
		for pos < end && s[pos] != '=' {
			if c = b64val(s[pos]); c != -1 {
				break
			}
			pos++
		}
		if pos >= end || s[pos] == '=' {
			break
		}
		pos++
		for pos < end && s[pos] != '=' {
			if dd = b64val(s[pos]); dd != -1 {
				break
			}
			pos++
		}
		if pos >= end || s[pos] == '=' {
			break
		}
		pos++
		out = append(out, byte(a<<2|b>>4), byte(b<<4|c>>2), byte(c<<6|dd))
		a = -1
	}
	if a != -1 && b != -1 {
		if c == -1 {
			out = append(out, byte(a<<2|b>>4))
		} else {
			out = append(out, byte(a<<2|b>>4), byte(b<<4|c>>2))
		}
	}
	return out
}

// qpdecode decodes quoted-printable data, mirroring MRI's unpack 'M': "=XX"
// escapes decode to a byte, "=\n" (and "=\r\n") soft breaks are dropped, and a
// malformed or truncated escape leaves the remainder of the input verbatim.
func qpdecode(s []byte) []byte {
	var out []byte
	pos, end := 0, len(s)
	ss := 0
	for pos < end {
		if s[pos] == '=' {
			pos++
			if pos == end {
				break
			}
			if pos+1 < end && s[pos] == '\r' && s[pos+1] == '\n' {
				pos++
			}
			if s[pos] != '\n' {
				c1 := hex2num(s[pos])
				if c1 == -1 {
					break
				}
				pos++
				if pos == end {
					break
				}
				c2 := hex2num(s[pos])
				if c2 == -1 {
					break
				}
				out = append(out, byte(c1<<4|c2))
			}
		} else {
			out = append(out, s[pos])
		}
		pos++
		ss = pos
	}
	return append(out, s[ss:]...)
}

// unpackElems deserialises data according to fmtStr.
func unpackElems(data []byte, fmtStr string) []object.Value {
	dirs := parseFormat(fmtStr, "unpack")
	var out []object.Value
	pos := 0
	for _, d := range dirs {
		switch {
		case isIntDir(d):
			s, _ := intSpecFor(d)
			w := s.width
			count := d.count
			if d.star {
				count = (len(data) - pos) / w
			}
			for k := 0; k < count; k++ {
				if pos+w > len(data) {
					out = append(out, object.NilV)
					continue
				}
				out = append(out, intResult(getUint(data[pos:pos+w], s), s))
				pos += w
			}
		case isFloatDir(d):
			w, bo, _ := floatSpec(d)
			count := d.count
			if d.star {
				count = (len(data) - pos) / w
			}
			for k := 0; k < count; k++ {
				if pos+w > len(data) {
					out = append(out, object.NilV)
					continue
				}
				bits := getUint(data[pos:pos+w], intSpec{w, bo, false})
				if w == 4 {
					out = append(out, object.Float(float64(math.Float32frombits(uint32(bits)))))
				} else {
					out = append(out, object.Float(math.Float64frombits(bits)))
				}
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
		case d.code == 'x':
			n := d.count
			if d.star {
				n = len(data) - pos
			}
			if pos+n > len(data) {
				raise("ArgumentError", "x outside of string")
			}
			pos += n
		case d.code == 'X':
			n := d.count // 'X*' backs up one byte, like a bare 'X'
			if n > pos {
				raise("ArgumentError", "X outside of string")
			}
			pos -= n
		case d.code == '@':
			if !d.star { // '*' has no effect on '@'
				n := 0 // implicit count of zero when unspecified
				if d.explicit {
					n = d.count
				}
				if n > len(data) {
					raise("ArgumentError", "@ outside of string")
				}
				pos = n
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
		case d.code == 'B' || d.code == 'b':
			n := d.count
			avail := (len(data) - pos) * 8
			if d.star || n > avail {
				n = avail
			}
			out = append(out, object.NewString(unpackBits(data[pos:], d.code, n)))
			pos += (n + 7) / 8
		case d.code == 'm':
			var dec []byte
			if d.explicit && d.count == 0 {
				var ok bool
				if dec, ok = base64Strict(data[pos:]); !ok {
					raise("ArgumentError", "invalid base64")
				}
			} else {
				dec = base64Lenient(data[pos:])
			}
			pos = len(data)
			out = append(out, object.NewStringBytesEnc(dec, "ASCII-8BIT"))
		case d.code == 'M':
			dec := qpdecode(data[pos:])
			pos = len(data)
			out = append(out, object.NewStringBytesEnc(dec, "ASCII-8BIT"))
		case d.code == 'u':
			dec := uudecode(data[pos:])
			pos = len(data)
			out = append(out, object.NewStringBytesEnc(dec, "ASCII-8BIT"))
		case d.code == 'w':
			// A BER integer is variable-length, so '*' means "until the input is
			// exhausted"; a numeric count decodes at most that many.
			for k := 0; (d.star || k < d.count) && pos < len(data); k++ {
				var z *big.Int
				z, pos = berDecodeAt(data, pos)
				out = append(out, object.NormInt(z))
			}
		case d.code == 'P' || d.code == 'p':
			// Pointer: read a native-width word and recover the string pack
			// registered for it. An unknown or null pointer unpacks to nil; 'P'
			// takes the leading count bytes, 'p' the whole registered string.
			w := ptrSpec.width
			if pos+w > len(data) {
				out = append(out, object.NilV)
				continue
			}
			id := getUint(data[pos:pos+w], ptrSpec)
			pos += w
			s, ok := lookupPackPtr(id)
			switch {
			case !ok:
				out = append(out, object.NilV)
			case d.code == 'p':
				out = append(out, object.NewStringBytesEnc(append([]byte(nil), s.Bytes()...), "ASCII-8BIT"))
			default: // 'P' takes the leading count bytes
				b := s.Bytes()
				n := d.count
				if n > len(b) {
					n = len(b)
				}
				out = append(out, object.NewStringBytesEnc(append([]byte(nil), b[:n]...), "ASCII-8BIT"))
			}
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
