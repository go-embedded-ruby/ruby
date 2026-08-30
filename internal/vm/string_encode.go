package vm

import (
	binpkg "encoding/binary"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// transcodeOpts holds the parsed keyword options of String#encode.
type transcodeOpts struct {
	invalidReplace   bool   // invalid: :replace — substitute undecodable source bytes
	undefReplace     bool   // undef: :replace — substitute characters absent from the target
	replace          string // replace: "..." — the substitution string
	hasReplace       bool
	crNewline        bool         // cr_newline: replace \n and \r\n with \r
	crlfNewline      bool         // crlf_newline: replace \n with \r\n
	universalNewline bool         // universal_newline: replace \r\n and \r with \n
	xml              string       // xml: :text or :attr — HTML/XML-escape the text
	fallback         object.Value // fallback: a Hash/Proc/Method/#[]-object mapping an unrepresentable character to its substitute
	hasFallback      bool
}

// isUnicodeEncoding reports whether name is one of Ruby's Unicode encodings, for
// which the default replacement character is U+FFFD (it is "?" elsewhere).
func isUnicodeEncoding(name string) bool {
	switch name {
	case "UTF-8", "UTF-16", "UTF-16LE", "UTF-16BE", "UTF-32", "UTF-32LE", "UTF-32BE":
		return true
	}
	return false
}

// defaultReplacement is the substitution MRI uses when none is given: U+FFFD for a
// Unicode destination, "?" otherwise.
func defaultReplacement(to string) string {
	if isUnicodeEncoding(to) {
		return "�"
	}
	return "?"
}

// parseEncodeOpts reads the trailing keyword Hash of String#encode into
// transcodeOpts. Unknown keys are ignored (as MRI does for encode).
func parseEncodeOpts(h *object.Hash) transcodeOpts {
	var o transcodeOpts
	sym := func(k string) (object.Value, bool) { return h.Get(object.Symbol(k)) }
	if v, ok := sym("invalid"); ok {
		o.invalidReplace = v == object.Symbol("replace")
	}
	if v, ok := sym("undef"); ok {
		o.undefReplace = v == object.Symbol("replace")
	}
	if v, ok := sym("replace"); ok {
		if s, isStr := v.(*object.String); isStr {
			o.replace, o.hasReplace = s.Str(), true
		}
	}
	if v, ok := sym("cr_newline"); ok {
		o.crNewline = truthyValue(v)
	}
	if v, ok := sym("crlf_newline"); ok {
		o.crlfNewline = truthyValue(v)
	}
	if v, ok := sym("universal_newline"); ok {
		o.universalNewline = truthyValue(v)
	}
	if v, ok := sym("xml"); ok {
		switch v {
		case object.Symbol("text"):
			o.xml = "text"
		case object.Symbol("attr"):
			o.xml = "attr"
		default:
			raise("ArgumentError", "unexpected value for xml option: %s", v.Inspect())
		}
	}
	if v, ok := sym("fallback"); ok && truthyValue(v) {
		o.fallback, o.hasFallback = v, true
	}
	return o
}

// applyXML performs the xml: :text / :attr escaping on the UTF-8 intermediate:
// & < > (and, for :attr, ") become entities, and :attr wraps the result in
// double quotes.
func applyXML(s string, o transcodeOpts) string {
	if o.xml == "" {
		return s
	}
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	if o.xml == "attr" {
		s = strings.ReplaceAll(s, `"`, "&quot;")
		s = `"` + s + `"`
	}
	return s
}

func truthyValue(v object.Value) bool { return v != object.NilV && v != object.Bool(false) }

// charEncodableIn reports whether rune r can be represented in encoding `to`. The
// :fallback pre-pass uses it to route only the genuinely unrepresentable
// characters through the fallback, leaving the rest for the normal encoder.
func charEncodableIn(r rune, to string) bool {
	switch to {
	case "UTF-8", "UTF-16", "UTF-16LE", "UTF-16BE", "UTF-32", "UTF-32LE", "UTF-32BE":
		return true
	case "US-ASCII", "ASCII-8BIT":
		return r < 0x80
	case "ISO-8859-1":
		return r < 0x100
	}
	if enc, ok := xtextEncodings[to]; ok {
		_, err := enc.NewEncoder().Bytes([]byte(string(r)))
		return err == nil
	}
	// An unknown target has no codec; leave the character in place so the encoder
	// raises Encoding::ConverterNotFoundError rather than the fallback masking it.
	return true
}

// applyFallback rewrites the UTF-8 intermediate u, replacing each character that
// cannot be represented in the target `to` with the result of the :fallback
// option — a Hash, Proc, Method or any object queried with #[], passed the
// character as a String. A nil result (a Hash miss included) leaves the character
// in place for the normal undefined-conversion error; a non-String, non-nil
// result raises TypeError, matching MRI. It is called only when undef: :replace is
// off (that option pre-empts the fallback entirely).
func (vm *VM) applyFallback(u, to string, fb object.Value) string {
	var b strings.Builder
	for _, r := range u {
		if charEncodableIn(r, to) {
			b.WriteRune(r)
			continue
		}
		res := vm.send(fb, "[]", []object.Value{object.NewString(string(r))}, nil)
		switch v := res.(type) {
		case *object.String:
			b.WriteString(v.Str())
		default:
			if res == object.NilV {
				b.WriteRune(r)
				continue
			}
			raise("TypeError", "no implicit conversion of %s into String", vm.classOf(res).name)
		}
	}
	return b.String()
}

// applyNewline rewrites newline sequences in the UTF-8 intermediate per the
// cr_newline / crlf_newline / universal_newline options.
func applyNewline(s string, o transcodeOpts) string {
	switch {
	case o.universalNewline:
		return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	case o.crlfNewline:
		return strings.ReplaceAll(s, "\n", "\r\n")
	case o.crNewline:
		return strings.ReplaceAll(s, "\n", "\r")
	}
	return s
}

// decodeToUTF8 decodes src (in encoding `from`) to a UTF-8 intermediate. Bytes it
// cannot decode raise Encoding::InvalidByteSequenceError, unless invalid:replace
// substitutes them; an unsupported source encoding raises
// Encoding::ConverterNotFoundError.
func (vm *VM) decodeToUTF8(src []byte, from string, o transcodeOpts, repl string) string {
	switch from {
	case "UTF-8":
		if utf8.Valid(src) {
			return string(src)
		}
		if o.invalidReplace {
			return scrubUTF8(string(src), repl)
		}
		raise("Encoding::InvalidByteSequenceError", "invalid byte sequence in UTF-8")
	case "US-ASCII":
		// A byte >= 0x80 is invalid in US-ASCII.
		var b strings.Builder
		for _, c := range src {
			if c < 0x80 {
				b.WriteByte(c)
			} else if o.invalidReplace {
				b.WriteString(repl)
			} else {
				raise("Encoding::InvalidByteSequenceError", "invalid byte sequence in US-ASCII")
			}
		}
		return b.String()
	case "ASCII-8BIT":
		// Binary bytes are always valid, but a byte >= 0x80 has no Unicode mapping,
		// so transcoding it out is an *undefined* conversion, not an invalid one.
		var b strings.Builder
		for _, c := range src {
			if c < 0x80 {
				b.WriteByte(c)
			} else if o.undefReplace {
				b.WriteString(repl)
			} else {
				raise("Encoding::UndefinedConversionError", "\"\\x%02X\" from ASCII-8BIT to UTF-8", c)
			}
		}
		return b.String()
	case "ISO-8859-1":
		var b strings.Builder
		for _, c := range src {
			b.WriteRune(rune(c)) // Latin-1 code points equal byte values
		}
		return b.String()
	case "UTF-16LE", "UTF-16BE":
		return vm.decodeUTF16(src, from == "UTF-16BE", o, repl)
	case "UTF-32LE", "UTF-32BE":
		return vm.decodeUTF32(src, from == "UTF-32BE", o, repl)
	}
	if u, ok := xtextDecode(src, from); ok {
		return u
	}
	raise("Encoding::ConverterNotFoundError", "code converter not found (%s to UTF-8)", from)
	return ""
}

func (vm *VM) decodeUTF16(src []byte, be bool, o transcodeOpts, repl string) string {
	order := binpkg.ByteOrder(binpkg.LittleEndian)
	if be {
		order = binpkg.BigEndian
	}
	var us []uint16
	i := 0
	for ; i+1 < len(src); i += 2 {
		us = append(us, order.Uint16(src[i:i+2]))
	}
	var b strings.Builder
	bad := func() {
		if !o.invalidReplace {
			raise("Encoding::InvalidByteSequenceError", "invalid byte sequence")
		}
		b.WriteString(repl)
	}
	// Decode unit by unit so a legitimate U+FFFD is kept while unpaired surrogates
	// are reported as invalid (utf16.Decode maps both to U+FFFD, losing that
	// distinction).
	for j := 0; j < len(us); j++ {
		u := us[j]
		switch {
		case u >= 0xD800 && u <= 0xDBFF: // high surrogate: needs a following low
			if j+1 < len(us) && us[j+1] >= 0xDC00 && us[j+1] <= 0xDFFF {
				b.WriteRune(utf16.DecodeRune(rune(u), rune(us[j+1])))
				j++
			} else {
				bad()
			}
		case u >= 0xDC00 && u <= 0xDFFF: // lone low surrogate
			bad()
		default:
			b.WriteRune(rune(u))
		}
	}
	if i != len(src) { // a trailing odd byte is an incomplete sequence
		bad()
	}
	return b.String()
}

func (vm *VM) decodeUTF32(src []byte, be bool, o transcodeOpts, repl string) string {
	order := binpkg.ByteOrder(binpkg.LittleEndian)
	if be {
		order = binpkg.BigEndian
	}
	var b strings.Builder
	i := 0
	for ; i+3 < len(src); i += 4 {
		r := rune(order.Uint32(src[i : i+4]))
		if r < 0 || r > utf8.MaxRune || (r >= 0xD800 && r <= 0xDFFF) {
			if !o.invalidReplace {
				raise("Encoding::InvalidByteSequenceError", "invalid byte sequence")
			}
			b.WriteString(repl)
			continue
		}
		b.WriteRune(r)
	}
	if i != len(src) {
		if !o.invalidReplace {
			raise("Encoding::InvalidByteSequenceError", "incomplete byte sequence")
		}
		b.WriteString(repl)
	}
	return b.String()
}

// encodeFromUTF8 encodes the UTF-8 intermediate u into encoding `to`. Characters
// absent from the target raise Encoding::UndefinedConversionError, unless
// undef:replace substitutes them; an unsupported target raises
// Encoding::ConverterNotFoundError.
func (vm *VM) encodeFromUTF8(u, to string, o transcodeOpts, repl string) []byte {
	switch to {
	case "UTF-8":
		return []byte(u)
	case "US-ASCII", "ASCII-8BIT":
		return vm.encodeByteRange(u, to, 0x80, o, repl)
	case "ISO-8859-1":
		return vm.encodeByteRange(u, to, 0x100, o, repl)
	case "UTF-16LE", "UTF-16BE":
		return encodeUTF16(u, to == "UTF-16BE")
	case "UTF-32LE", "UTF-32BE":
		return encodeUTF32(u, to == "UTF-32BE")
	}
	if b, ok := xtextEncode(u, to, o.undefReplace, repl); ok {
		return b
	}
	raise("Encoding::ConverterNotFoundError", "code converter not found (UTF-8 to %s)", to)
	return nil
}

// encodeByteRange encodes each rune of u to a single byte when it is below limit
// (0x80 for ASCII, 0x100 for Latin-1); a character outside the range is undefined
// in the target.
func (vm *VM) encodeByteRange(u, to string, limit rune, o transcodeOpts, repl string) []byte {
	var b []byte
	for _, r := range u {
		if r < limit {
			b = append(b, byte(r))
			continue
		}
		if !o.undefReplace {
			raise("Encoding::UndefinedConversionError", "U+%04X from UTF-8 to %s", r, to)
		}
		b = append(b, []byte(repl)...)
	}
	return b
}

func encodeUTF16(u string, be bool) []byte {
	order := binpkg.ByteOrder(binpkg.LittleEndian)
	if be {
		order = binpkg.BigEndian
	}
	us := utf16.Encode([]rune(u))
	b := make([]byte, 2*len(us))
	for i, x := range us {
		order.PutUint16(b[2*i:], x)
	}
	return b
}

func encodeUTF32(u string, be bool) []byte {
	order := binpkg.ByteOrder(binpkg.LittleEndian)
	if be {
		order = binpkg.BigEndian
	}
	rs := []rune(u)
	b := make([]byte, 4*len(rs))
	for i, r := range rs {
		order.PutUint32(b[4*i:], uint32(r))
	}
	return b
}

// stringEncode implements String#encode(dst = default, src = self.encoding, **opts).
func (vm *VM) stringEncode(s *object.String, args []object.Value) *object.String {
	var positional []object.Value
	var opts transcodeOpts
	if h, ok := trailingHash(args); ok {
		opts = parseEncodeOpts(h)
		positional = args[:len(args)-1]
	} else {
		positional = args
	}

	to := s.EncName()
	from := s.EncName()
	if len(positional) >= 1 {
		to = vm.encodingArg(positional[0]).name
	}
	if len(positional) >= 2 {
		from = vm.encodingArg(positional[1]).name
	}

	repl := opts.replace
	if !opts.hasReplace {
		repl = defaultReplacement(to)
	}

	inter := vm.decodeToUTF8(s.Bytes(), from, opts, repl)
	inter = applyNewline(inter, opts)
	inter = applyXML(inter, opts)
	// The :fallback option supplies substitutes for unrepresentable characters,
	// but only where undef: :replace does not already handle them — that option
	// pre-empts the fallback (MRI never consults it once undef substitution is on).
	if opts.hasFallback && !opts.undefReplace {
		inter = vm.applyFallback(inter, to, opts.fallback)
	}
	out := vm.encodeFromUTF8(inter, to, opts, repl)
	return object.NewStringBytesEnc(out, to)
}

// registerStringEncodeMethods wires String#encode / #encode! and the
// Encoding::*Error classes. Called from registerStringEncoding.
func (vm *VM) registerStringEncodeMethods() {
	vm.cString.define("encode", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.stringEncode(self.(*object.String), args)
	})
	vm.cString.define("encode!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := self.(*object.String)
		enc := vm.stringEncode(s, args)
		s.SetBytes(append([]byte(nil), enc.Bytes()...))
		s.Enc = enc.Enc
		return s
	})
}
