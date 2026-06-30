package vm

import (
	b64 "github.com/go-ruby-base64/base64"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerBase64 installs the Base64 module (require "base64") as a binding of
// github.com/go-ruby-base64/base64, a pure-Go, MRI-4.0.5-faithful Base64 whose
// standard-alphabet encode runs on go-simd kernels. encode64 wraps at 60 columns
// with a trailing newline; the strict_/urlsafe_ variants don't wrap.
//
// The binding does NOT round-trip through Go strings on the hot path. A Ruby
// String is a []byte (object.String.B); the library API is string->string, so a
// naive binding pays three copies per call — []byte->string in, the library's
// own transform, then string->[]byte out — which, plus the lenient decoder's
// byte-at-a-time append growth and urlsafe's rune-by-rune strings.Map, made the
// Ruby-visible methods ~1.3x-6x slower than MRI's pack/unpack.
//
// So every method takes a tight scalar path here that reads s.B directly and
// writes once into a single pre-sized []byte wrapped straight into the result
// String (no intermediate string). The one exception is large strict_encode64,
// routed back through the library so the go-simd kernel runs the raw transform
// (see encodeSIMDThreshold). The scalar paths reproduce the library's exact MRI
// semantics; the differential-vs-MRI oracle in stdlib_test.go guards them.
func (vm *VM) registerBase64() {
	mod := newClass("Base64", nil)
	mod.isModule = true
	vm.consts["Base64"] = mod
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	def("encode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// encode64 is ALWAYS scalar: the library's Encode64 runs SIMD then re-copies
		// the whole output through wrap60 to insert 60-column newlines, and that
		// extra pass cancels the SIMD win — the single-pass scalar encoder (which
		// emits the newlines inline) matches or beats it at every size. So there is
		// no SIMD crossover for the wrapped form.
		return &object.String{B: encodeScalar(base64Bytes(args[0]), b64StdAlphabet, true)}
	})
	def("strict_encode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		src := base64Bytes(args[0])
		if len(src) >= encodeSIMDThreshold {
			return object.NewString(b64.StrictEncode64(string(src)))
		}
		return &object.String{B: encodeScalar(src, b64StdAlphabet, false)}
	})
	def("urlsafe_encode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// urlsafe always pads here (rbgo never passed the padding: kwarg), so the
		// scalar encoder produces the final bytes with no post-pass remap.
		return &object.String{B: encodeScalar(base64Bytes(args[0]), b64URLAlphabet, false)}
	})
	def("decode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return &object.String{B: decodeLenient(base64Bytes(args[0]))}
	})
	def("strict_decode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		out, ok := decodeStrict(base64Bytes(args[0]))
		if !ok {
			raiseBase64Invalid()
		}
		return &object.String{B: out}
	})
	def("urlsafe_decode64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// MRI urlsafe_decode64 translates -_ -> +/ (and, like its tr-based impl,
		// leaves any +/ in the input untouched), re-pads unpadded input whose length
		// isn't a multiple of 4, then delegates to strict_decode64. So normalise to
		// the standard alphabet here and run the same strict decoder.
		out, ok := decodeStrict(urlsafeToStd(base64Bytes(args[0])))
		if !ok {
			raiseBase64Invalid()
		}
		return &object.String{B: out}
	})
}

// encodeSIMDThreshold is the input size (bytes) at or above which strict_encode64
// routes back through the go-ruby-base64 library so the go-simd kernel does the
// transform. Below it the scalar encoder here is faster: it skips the
// []byte->string->[]byte copies the string-API binding pays, which dominate the
// SIMD win for small inputs. At and above it the SIMD throughput overtakes those
// fixed copies. Measured on arm64 (Apple M4): scalar and SIMD cross at ~2048 B
// (~1.05 us each); 2048 sits exactly on the crossover. Only strict_encode64 uses
// this — encode64 is always scalar (its library path re-copies through wrap60,
// cancelling the SIMD win), and decodes are always scalar (the library has no
// SIMD decode for the lenient path and re-pads/strings.Maps the rest).
const encodeSIMDThreshold = 2048

const (
	b64StdAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b64URLAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
)

// b64StdDec maps a byte to its 6-bit value in the standard +/ alphabet, 0xFF for
// "not in the alphabet" (so a single table lookup per byte enforces the alphabet
// with no extra symbol checks). '=' and stray bytes are 0xFF. The decoders work in
// the standard alphabet: decode64/strict_decode64 use it directly, urlsafe first
// translates -_ -> +/. Built once at init.
var b64StdDec [256]byte

func init() {
	for i := range b64StdDec {
		b64StdDec[i] = 0xFF
	}
	for i := 0; i < 64; i++ {
		b64StdDec[b64StdAlphabet[i]] = byte(i)
	}
}

// encodeScalar base64-encodes src with the given 64-byte alphabet, writing once
// into a single pre-sized buffer. When wrap is true it inserts MRI's pack("m")
// framing: a newline every 60 output columns plus a trailing newline (and the
// empty input encodes to the empty string, matching MRI). When wrap is false it
// is the strict / urlsafe form: padded, no newlines.
func encodeScalar(src []byte, alphabet string, wrap bool) []byte {
	if len(src) == 0 {
		return []byte{} // both Encode64("") and StrictEncode64("") are ""
	}
	n := len(src)
	// 4 base64 chars per 3 input bytes, rounded up (padding fills the last quad).
	enc := (n + 2) / 3 * 4
	total := enc
	if wrap {
		// MRI's wrap60 inserts a '\n' only *between* 60-column lines (its loop runs
		// while strictly more than 60 chars remain) and a single trailing '\n'. So a
		// run of exactly k*60 chars gets k newlines total, not k+1. Interior breaks
		// = (enc-1)/60; plus the one trailing newline.
		total = enc + (enc-1)/60 + 1
	}
	dst := make([]byte, total)
	a := alphabet

	if !wrap {
		// strict / urlsafe: a single tight loop, no newline tracking.
		di := 0
		si := 0
		for ; si+3 <= n; si += 3 {
			v := uint32(src[si])<<16 | uint32(src[si+1])<<8 | uint32(src[si+2])
			dst[di] = a[v>>18&0x3F]
			dst[di+1] = a[v>>12&0x3F]
			dst[di+2] = a[v>>6&0x3F]
			dst[di+3] = a[v&0x3F]
			di += 4
		}
		encodeTail(dst[di:], a, src[si:n])
		return dst
	}

	// wrap (encode64): MRI's pack("m") puts a '\n' *between* 60-column lines plus a
	// single trailing '\n'. 60 columns == 15 quads == 45 input bytes. Emit each full
	// line that has content after it (si+lineBytes < n, strictly, so the final line
	// — full or ragged — is left to the tail below and gets exactly one '\n'), then
	// the last line, then the trailing newline. All inline, single pass.
	const lineBytes = 45 // input bytes per 60-char output line
	di := 0
	si := 0
	for ; si+lineBytes < n; si += lineBytes {
		for j := si; j < si+lineBytes; j += 3 {
			v := uint32(src[j])<<16 | uint32(src[j+1])<<8 | uint32(src[j+2])
			dst[di] = a[v>>18&0x3F]
			dst[di+1] = a[v>>12&0x3F]
			dst[di+2] = a[v>>6&0x3F]
			dst[di+3] = a[v&0x3F]
			di += 4
		}
		dst[di] = '\n'
		di++
	}
	// Final partial line: whole quads then the 1/2-byte tail.
	for ; si+3 <= n; si += 3 {
		v := uint32(src[si])<<16 | uint32(src[si+1])<<8 | uint32(src[si+2])
		dst[di] = a[v>>18&0x3F]
		dst[di+1] = a[v>>12&0x3F]
		dst[di+2] = a[v>>6&0x3F]
		dst[di+3] = a[v&0x3F]
		di += 4
	}
	di += encodeTail(dst[di:], a, src[si:n])
	dst[di] = '\n'
	di++
	return dst[:di]
}

// encodeTail encodes the final 1 or 2 leftover bytes (with '=' padding) into dst,
// returning the number of bytes written (0 when src is empty/a whole quad).
func encodeTail(dst []byte, a string, src []byte) int {
	switch len(src) {
	case 1:
		v := uint32(src[0]) << 16
		dst[0] = a[v>>18&0x3F]
		dst[1] = a[v>>12&0x3F]
		dst[2] = '='
		dst[3] = '='
		return 4
	case 2:
		v := uint32(src[0])<<16 | uint32(src[1])<<8
		dst[0] = a[v>>18&0x3F]
		dst[1] = a[v>>12&0x3F]
		dst[2] = a[v>>6&0x3F]
		dst[3] = '='
		return 4
	}
	return 0
}

// decodeLenient reproduces MRI's Base64.decode64 (String#unpack1("m")): bytes
// outside the standard alphabet are skipped; '=' on a 2- or 3-char partial quad
// finalises it and stops; '=' on a quad boundary is ignored; a lone trailing
// sextet is discarded. It writes once into a buffer sized for the maximum
// possible output (every input char a real sextet -> len*3/4 bytes).
func decodeLenient(src []byte) []byte {
	dst := make([]byte, len(src)*3/4+3)
	di := 0
	var quad [4]byte
	q := 0
	for _, c := range src {
		v := b64StdDec[c]
		if v == 0xFF {
			// Not a standard-alphabet byte. '=' on a 2/3-char partial quad
			// finalises it and stops; otherwise (stray byte, or '=' on a quad
			// boundary) it is skipped. Only '=' can terminate.
			if c == '=' && q >= 2 {
				dst[di] = quad[0]<<2 | quad[1]>>4
				di++
				if q == 3 {
					dst[di] = quad[1]<<4 | quad[2]>>2
					di++
				}
				return dst[:di]
			}
			continue
		}
		quad[q] = v
		q++
		if q == 4 {
			dst[di] = quad[0]<<2 | quad[1]>>4
			dst[di+1] = quad[1]<<4 | quad[2]>>2
			dst[di+2] = quad[2]<<6 | quad[3]
			di += 3
			q = 0
		}
	}
	// End of input: a 2- or 3-char remainder yields whole bytes; a single orphan
	// sextet is discarded.
	if q >= 2 {
		dst[di] = quad[0]<<2 | quad[1]>>4
		di++
		if q == 3 {
			dst[di] = quad[1]<<4 | quad[2]>>2
			di++
		}
	}
	return dst[:di]
}

// decodeStrict reproduces MRI's strict_decode64 / urlsafe_decode64: input must be
// a well-formed padded (or, for urlsafe, correctly padded-or-unpadded) base64
// string over the standard +/ alphabet with mandatory '=' padding; anything else
// returns ok=false. It reproduces the go-ruby-base64 library's StrictDecode64,
// which rejects any non-alphabet byte (newlines included) and then delegates to
// encoding/base64.StdEncoding: the total length (data + padding) must be a
// multiple of 4, with 0..2 trailing '=' in the final quad and none elsewhere.
// urlsafe_decode64 reaches this after normalising -_ -> +/ and re-padding.
func decodeStrict(src []byte) ([]byte, bool) {
	// StdEncoding requires the padded length to be a multiple of 4. (The empty
	// string is the one valid len%4==0 case with no output.)
	if len(src)%4 != 0 {
		return nil, false
	}
	n := len(src)
	pad := 0
	for n > 0 && src[n-1] == '=' {
		pad++
		n--
	}
	if pad > 2 {
		return nil, false // at most two '=' may pad the final quad
	}
	body := src[:n]
	dst := make([]byte, len(body)*3/4+2)
	di := 0
	tab := &b64StdDec
	// All whole quads except possibly the last (the padded one). Read 4 chars, 4
	// lookups, write 3 bytes; any 0xFF (incl. a stray '=' that isn't trailing) is
	// malformed. No stray-skipping — strict mode consumes a full quad at a time.
	end := len(body)
	if pad > 0 {
		end -= 4 - pad // hold back the final padded quad for the tail handling
	}
	bi := 0
	for ; bi < end; bi += 4 {
		a0 := tab[body[bi]]
		a1 := tab[body[bi+1]]
		a2 := tab[body[bi+2]]
		a3 := tab[body[bi+3]]
		if a0|a1|a2|a3 == 0xFF {
			return nil, false
		}
		dst[di] = a0<<2 | a1>>4
		dst[di+1] = a1<<4 | a2>>2
		dst[di+2] = a2<<6 | a3
		di += 3
	}
	// Final padded quad: pad==1 -> 3 data chars -> 2 bytes; pad==2 -> 2 chars -> 1.
	switch pad {
	case 1:
		q0, q1, q2 := tab[body[bi]], tab[body[bi+1]], tab[body[bi+2]]
		if q0|q1|q2 == 0xFF {
			return nil, false
		}
		dst[di] = q0<<2 | q1>>4
		dst[di+1] = q1<<4 | q2>>2
		di += 2
	case 2:
		q0, q1 := tab[body[bi]], tab[body[bi+1]]
		if q0|q1 == 0xFF {
			return nil, false
		}
		dst[di] = q0<<2 | q1>>4
		di++
	}
	return dst[:di], true
}

// urlsafeToStd reproduces the input normalisation MRI's urlsafe_decode64 does
// before delegating to strict_decode64: translate the url-safe -_ alphabet to the
// standard +/ (any +/ already present is left as-is, matching MRI's tr), and, when
// the input is unpadded and its length is not a multiple of 4, right-pad with '='
// to the next quad boundary. Returns bytes ready for decodeStrict.
func urlsafeToStd(src []byte) []byte {
	pad := 0
	if (len(src) == 0 || src[len(src)-1] != '=') && len(src)%4 != 0 {
		pad = (4 - len(src)%4) % 4
	}
	out := make([]byte, len(src)+pad)
	for i, c := range src {
		switch c {
		case '-':
			out[i] = '+'
		case '_':
			out[i] = '/'
		default:
			out[i] = c
		}
	}
	for i := len(src); i < len(out); i++ {
		out[i] = '='
	}
	return out
}

// raiseBase64Invalid maps an invalid strict/urlsafe decode to Ruby's
// ArgumentError("invalid base64"), matching MRI's strict_/urlsafe_decode64.
func raiseBase64Invalid() {
	raise("ArgumentError", "invalid base64")
}

// base64Arg returns the String argument's bytes as a Go string, raising TypeError
// otherwise. Kept for the OpenSSL/Digest bindings that share this coercion.
func base64Arg(v object.Value) string {
	return string(base64Bytes(v))
}

// base64Bytes returns the String argument's underlying bytes without copying,
// raising TypeError for a non-String. The Base64 hot paths read these directly.
func base64Bytes(v object.Value) []byte {
	s, ok := v.(*object.String)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	}
	return s.B
}
