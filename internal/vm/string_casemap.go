package vm

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file implements the shared case-mapping engine behind String#upcase,
// #downcase, #swapcase, #capitalize and their bang variants (plus the matching
// Symbol methods), matching MRI 4.0.x semantics including the option arguments
// :ascii, :fold, :turkic and :lithuanian and full (multi-character) Unicode
// case mapping such as "ß".upcase == "SS".

// caseMode selects which of the four case transforms to apply per character.
type caseMode int

const (
	caseUpcase caseMode = iota
	caseDowncase
	caseSwapcase
	caseCapitalize
)

// caseFlags holds the parsed option arguments. At most turkic+lithuanian may be
// combined; ascii and fold are mutually exclusive with everything else. fold is
// only ever set for downcasing.
type caseFlags struct {
	ascii      bool
	fold       bool
	turkic     bool
	lithuanian bool
}

// Runes referenced by the special-casing rules below.
const (
	runeSharpS    = 0x00DF // ß LATIN SMALL LETTER SHARP S
	runeDotlessI  = 0x0131 // ı LATIN SMALL LETTER DOTLESS I
	runeDottedI   = 0x0130 // İ LATIN CAPITAL LETTER I WITH DOT ABOVE
	runeDotAbove  = 0x0307 // ◌̇ COMBINING DOT ABOVE
	runeCapitalSS = 'S'
)

// parseCaseOptions mirrors MRI's rb_str_check_case_options: it validates the
// symbol option arguments for mode and returns the resulting flags, raising
// ArgumentError for any combination MRI rejects.
func parseCaseOptions(mode caseMode, args []object.Value) caseFlags {
	var f caseFlags
	if len(args) == 0 {
		return f
	}
	if len(args) > 2 {
		raise("ArgumentError", "too many options")
	}
	sym := func(v object.Value) (string, bool) {
		s, ok := v.(object.Symbol)
		return string(s), ok
	}
	first, _ := sym(args[0])
	switch first {
	case "turkic":
		f.turkic = true
		if len(args) == 2 {
			if s, ok := sym(args[1]); ok && s == "lithuanian" {
				f.lithuanian = true
			} else {
				raise("ArgumentError", "invalid second option")
			}
		}
	case "lithuanian":
		f.lithuanian = true
		if len(args) == 2 {
			if s, ok := sym(args[1]); ok && s == "turkic" {
				f.turkic = true
			} else {
				raise("ArgumentError", "invalid second option")
			}
		}
	default:
		if len(args) > 1 {
			raise("ArgumentError", "too many options")
		}
		switch first {
		case "ascii":
			f.ascii = true
		case "fold":
			if mode == caseDowncase {
				f.fold = true
			} else {
				raise("ArgumentError", "option :fold only allowed for downcasing")
			}
		default:
			raise("ArgumentError", "invalid option")
		}
	}
	return f
}

// upRune returns the uppercase mapping of r under flags f.
func upRune(r rune, f caseFlags) []rune {
	if f.ascii {
		if r >= 'a' && r <= 'z' {
			return []rune{r - 32}
		}
		return []rune{r}
	}
	if f.turkic {
		switch r {
		case 'i':
			return []rune{runeDottedI}
		case runeDotlessI:
			return []rune{'I'}
		}
	}
	if r == runeSharpS {
		return []rune{runeCapitalSS, runeCapitalSS}
	}
	return []rune{unicode.ToUpper(r)}
}

// downRune returns the lowercase (or, with f.fold, case-folded) mapping of r.
func downRune(r rune, f caseFlags) []rune {
	if f.ascii {
		if r >= 'A' && r <= 'Z' {
			return []rune{r + 32}
		}
		return []rune{r}
	}
	if f.turkic {
		switch r {
		case 'I':
			return []rune{runeDotlessI}
		case runeDottedI:
			return []rune{'i'}
		}
	}
	if f.fold && r == runeSharpS {
		return []rune{'s', 's'}
	}
	if r == runeDottedI {
		// Full Unicode mapping: İ lowercases to "i" + combining dot above.
		return []rune{'i', runeDotAbove}
	}
	return []rune{unicode.ToLower(r)}
}

// titleRune returns the titlecase mapping of r, used for the first character of
// #capitalize.
func titleRune(r rune, f caseFlags) []rune {
	if f.ascii {
		if r >= 'a' && r <= 'z' {
			return []rune{r - 32}
		}
		return []rune{r}
	}
	if f.turkic {
		switch r {
		case 'i':
			return []rune{runeDottedI}
		case runeDotlessI:
			return []rune{'I'}
		}
	}
	if r == runeSharpS {
		return []rune{runeCapitalSS, 's'}
	}
	return []rune{unicode.ToTitle(r)}
}

// swapRune upcases a lowercase r and downcases an uppercase r, leaving anything
// else untouched.
func swapRune(r rune, f caseFlags) []rune {
	if f.ascii {
		switch {
		case r >= 'a' && r <= 'z':
			return []rune{r - 32}
		case r >= 'A' && r <= 'Z':
			return []rune{r + 32}
		}
		return []rune{r}
	}
	switch {
	case unicode.IsUpper(r):
		return downRune(r, f)
	case unicode.IsLower(r):
		return upRune(r, f)
	}
	return []rune{r}
}

// caseMapUTF8 applies mode/flags to a UTF-8 string, expanding multi-character
// mappings (e.g. ß→SS). Invalid bytes are copied through unchanged.
func caseMapUTF8(s string, mode caseMode, f caseFlags) string {
	var b strings.Builder
	b.Grow(len(s))
	first := true
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			b.WriteByte(s[i])
			i++
			first = false
			continue
		}
		i += size
		var out []rune
		switch mode {
		case caseUpcase:
			out = upRune(r, f)
		case caseDowncase:
			out = downRune(r, f)
		case caseSwapcase:
			out = swapRune(r, f)
		case caseCapitalize:
			if first {
				out = titleRune(r, f)
			} else {
				out = downRune(r, f)
			}
		}
		first = false
		for _, o := range out {
			b.WriteRune(o)
		}
	}
	return b.String()
}

// caseMapBytes applies mode/flags to bytes carrying the named encoding. An
// encoding that is not ASCII-compatible (e.g. UTF-16LE) is decoded to UTF-8,
// mapped, and re-encoded so multi-byte characters are transformed correctly.
func (vm *VM) caseMapBytes(src []byte, enc string, mode caseMode, f caseFlags) []byte {
	if e, ok := vm.findEncoding(enc); ok && !e.asciiCompat {
		u := vm.decodeToUTF8(src, enc, transcodeOpts{}, "")
		mapped := caseMapUTF8(u, mode, f)
		return vm.encodeFromUTF8(mapped, enc, transcodeOpts{}, "")
	}
	return []byte(caseMapUTF8(string(src), mode, f))
}

// stringCaseMap implements the non-bang String case methods: it parses options,
// applies the transform in the receiver's encoding, and returns a fresh String
// carrying that same encoding.
func (vm *VM) stringCaseMap(self object.Value, mode caseMode, args []object.Value) object.Value {
	s := self.(*object.String)
	f := parseCaseOptions(mode, args)
	out := vm.caseMapBytes(s.Bytes(), s.EncName(), mode, f)
	return object.NewStringBytesEnc(out, s.Enc)
}

// stringCaseMapBang implements the bang String case methods: it mutates the
// receiver in place, returning nil when no change was made (and raising
// FrozenError on a frozen receiver even then).
func (vm *VM) stringCaseMapBang(self object.Value, mode caseMode, args []object.Value) object.Value {
	s := self.(*object.String)
	f := parseCaseOptions(mode, args)
	vm.checkFrozen(s)
	orig := s.Bytes()
	out := vm.caseMapBytes(orig, s.EncName(), mode, f)
	if bytes.Equal(orig, out) {
		return object.NilV
	}
	s.SetBytes(out)
	return s
}
