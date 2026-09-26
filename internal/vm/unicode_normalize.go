package vm

import (
	"unicode/utf8"

	norm "github.com/go-ruby-unicode-normalize/unicode-normalize"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// unicode_normalize wires the unicode_normalize standard library — the String
// instance methods #unicode_normalize and #unicode_normalized? — onto the String
// class. In MRI these core extensions are always available (no require needed),
// so they are registered unconditionally from the String setup. The actual
// normalization is delegated to the pure-Go (CGO=0)
// github.com/go-ruby-unicode-normalize/unicode-normalize library, which is
// MRI-byte-compatible (it patches x/text/unicode/norm up to Unicode 17.0.0).

// formOf maps a Ruby normalization-form argument to the library's Form. MRI only
// accepts the four bare symbols :nfc, :nfd, :nfkc and :nfkd; any other value
// (including the equivalent strings) raises ArgumentError "Invalid normalization
// form <to_s>." — the to_s of the offending argument. ok reports whether the
// argument was one of the four recognised symbols.
func formOf(v object.Value) (form norm.Form, ok bool) {
	if s, isSym := v.(object.Symbol); isSym {
		switch string(s) {
		case "nfc":
			return norm.NFC, true
		case "nfd":
			return norm.NFD, true
		case "nfkc":
			return norm.NFKC, true
		case "nfkd":
			return norm.NFKD, true
		}
	}
	return 0, false
}

// normForm extracts the form from a method's args (defaulting to :nfc when the
// argument is omitted) and raises ArgumentError for an unrecognised form, exactly
// as MRI does.
func normForm(args []object.Value) norm.Form {
	if len(args) == 0 {
		return norm.NFC // default form is :nfc
	}
	form, ok := formOf(args[0])
	if !ok {
		raise("ArgumentError", "Invalid normalization form %s.", args[0].ToS())
	}
	return form
}

// normSource returns the receiver's UTF-8 contents, raising ArgumentError
// "invalid byte sequence in UTF-8" when the string is not valid UTF-8 — the same
// error MRI raises before attempting to normalize.
func normSource(self object.Value) string {
	s := self.(*object.String)
	if !utf8.Valid(s.Bytes()) {
		raise("ArgumentError", "invalid byte sequence in UTF-8")
	}
	return string(s.Bytes())
}

// unicodeNormalizeVia is the UNICODE_ENCODINGS list of
// lib/unicode_normalize/normalize.rb: the encodings whose strings are normalized
// by round-tripping through UTF-8. UCS-2BE and UCS-4BE are aliases of UTF-16BE and
// UTF-32BE, so they arrive here under their canonical names.
func unicodeNormalizeVia(enc string) bool {
	switch enc {
	case "UTF-16BE", "UTF-16LE", "UTF-32BE", "UTF-32LE", "GB18030":
		return true
	}
	return false
}

// normTarget performs the encoding dispatch at the head of
// UnicodeNormalize.normalize and .normalized?
// (lib/unicode_normalize/normalize.rb): UTF-8 is normalized directly, US-ASCII is
// already normalized in every form, the other Unicode encodings are normalized
// through UTF-8 and encoded back, and every other encoding is an
// Encoding::CompatibilityError — normalization is not defined for it.
//
// The encoding decides BEFORE the bytes are looked at, which is why an
// ISO-8859-1 string of one high byte reports the incompatibility rather than
// "invalid byte sequence in UTF-8": in MRI that ArgumentError can only come from
// the UTF-8 branch's gsub.
//
// back is the encoding the normalized text must be encoded into again ("" when the
// receiver was already UTF-8); done reports a receiver that needs no normalizing
// at all.
func (vm *VM) normTarget(self object.Value) (src, back string, done bool) {
	s := self.(*object.String)
	switch enc := vm.canonicalEncName(s.EncName()); enc {
	case "UTF-8":
		return normSource(self), "", false
	case "US-ASCII":
		return string(s.Bytes()), "", true
	default:
		if !unicodeNormalizeVia(enc) {
			raise("Encoding::CompatibilityError", "Unicode Normalization not appropriate for %s", enc)
		}
		return vm.stringEncode(s, []object.Value{object.NewString("UTF-8")}).Str(), enc, false
	}
}

// normResult encodes a normalized UTF-8 result back into the receiver's encoding
// when it was one of the non-UTF-8 Unicode encodings (`.encode(encoding)` at the
// tail of UnicodeNormalize.normalize).
func (vm *VM) normResult(out, back string) *object.String {
	u := object.NewString(out)
	if back == "" {
		return u
	}
	return vm.stringEncode(u, []object.Value{object.NewString(back)})
}

// registerStringUnicodeNormalize adds the unicode_normalize core-ext String
// methods. (Called from the String setup so it shares cString.)
func (vm *VM) registerStringUnicodeNormalize() {
	vm.cString.define("unicode_normalize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		form := normForm(args)
		src, back, done := vm.normTarget(self)
		if done {
			return object.NewStringBytesEnc([]byte(src), self.(*object.String).Enc)
		}
		return vm.normResult(norm.Normalize(src, form), back)
	})
	vm.cString.define("unicode_normalized?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		form := normForm(args)
		src, _, done := vm.normTarget(self)
		if done {
			return object.Bool(true)
		}
		return object.Bool(norm.IsNormalized(src, form))
	})
	// unicode_normalize! normalizes the receiver in place and returns self (even
	// when already normalized). The form argument is validated before the frozen
	// check, matching MRI.
	vm.cString.define("unicode_normalize!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		form := normForm(args)
		s := self.(*object.String)
		src, back, done := vm.normTarget(self)
		vm.checkFrozen(s)
		if !done {
			s.SetBytes(append([]byte(nil), vm.normResult(norm.Normalize(src, form), back).Bytes()...))
		}
		return self
	})
}
