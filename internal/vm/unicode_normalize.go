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

// registerStringUnicodeNormalize adds the unicode_normalize core-ext String
// methods. (Called from the String setup so it shares cString.)
func (vm *VM) registerStringUnicodeNormalize() {
	vm.cString.define("unicode_normalize", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		form := normForm(args)
		return object.NewString(norm.Normalize(normSource(self), form))
	})
	vm.cString.define("unicode_normalized?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		form := normForm(args)
		return object.Bool(norm.IsNormalized(normSource(self), form))
	})
}
