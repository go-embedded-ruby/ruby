package vm

import (
	"math/big"
	"strconv"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Numeric string parsing shared by String#to_r / String#to_c and the String
// argument forms of Kernel#Rational() / Kernel#Complex(). MRI has two levels:
// String#to_r/#to_c are lenient (they skip leading whitespace, ignore trailing
// garbage, and yield Rational(0,1) / Complex(0,0) when nothing parses), while
// Kernel#Rational/#Complex are strict (the whole string — bar surrounding
// whitespace — must be a valid literal, else the caller raises). The two share
// the same scanners below, differing only in the strict trailing-garbage check.

// numericCtor runs a Kernel#Rational / Kernel#Complex builder. When doRaise is
// true it simply returns the builder's result (any error propagates). When
// doRaise is false — the `exception: false` form — a Ruby error raised anywhere
// in the builder (including inside a #to_r / #to_int call it makes) is caught and
// the constructor yields nil instead. Non-Ruby panics are re-raised unchanged.
func (vm *VM) numericCtor(doRaise bool, build func() object.Value) (result object.Value) {
	if doRaise {
		return build()
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(RubyError); ok {
				result = object.NilV
				return
			}
			panic(r)
		}
	}()
	return build()
}

// numIsDigit reports whether c is an ASCII decimal digit.
func numIsDigit(c byte) bool { return c >= '0' && c <= '9' }

// numIsSpace reports whether c is ASCII whitespace (the set MRI's parser skips).
func numIsSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

// numSkipWS returns the first index at or after i whose byte is not whitespace.
func numSkipWS(s string, i int) int {
	for i < len(s) && numIsSpace(s[i]) {
		i++
	}
	return i
}

// numIsImagUnit reports whether c is one of Ruby's imaginary-unit letters.
func numIsImagUnit(c byte) bool {
	return c == 'i' || c == 'I' || c == 'j' || c == 'J'
}

// numScanDigits reads a run of decimal digits from s[i:], permitting a single
// underscore between two digits (Ruby's numeric-literal rule). A leading
// underscore, a trailing underscore, or a doubled underscore ends the run. It
// returns the digits with underscores removed and the index just past the run.
func numScanDigits(s string, i int) (string, int) {
	var b []byte
	for i < len(s) {
		c := s[i]
		if numIsDigit(c) {
			b = append(b, c)
			i++
			continue
		}
		if c == '_' && len(b) > 0 && i+1 < len(s) && numIsDigit(s[i+1]) {
			i++
			continue
		}
		break
	}
	return string(b), i
}

// numScanDecimal parses an unsigned decimal number — digits, an optional
// ".fraction", and an optional "e"/"E" exponent — from s[i:] as an exact
// rational. A leading period with a following digit (".5") is accepted; a lone
// "." is not. ok is false when no digit is present. isFloat reports whether the
// number carried a decimal point or exponent, which the Complex scanner uses to
// choose Float versus exact typing.
func numScanDecimal(s string, i int) (val *big.Rat, next int, isFloat, ok bool) {
	start := i
	intPart, j := numScanDigits(s, i)
	i = j
	fracPart := ""
	hasDot := false
	if i < len(s) && s[i] == '.' {
		fp, k := numScanDigits(s, i+1)
		if len(fp) > 0 {
			fracPart = fp
			hasDot = true
			i = k
		}
	}
	if intPart == "" && fracPart == "" {
		return nil, start, false, false
	}
	exp := int64(0)
	hasExp := false
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		k := i + 1
		esign := int64(1)
		if k < len(s) && (s[k] == '+' || s[k] == '-') {
			if s[k] == '-' {
				esign = -1
			}
			k++
		}
		ep, m := numScanDigits(s, k)
		if len(ep) > 0 {
			e, _ := strconv.ParseInt(ep, 10, 64)
			exp = esign * e
			hasExp = true
			i = m
		}
	}
	num := new(big.Int)
	num.SetString(intPart+fracPart, 10) // non-empty by the guard above
	r := new(big.Rat).SetInt(num)
	scale := exp - int64(len(fracPart)) // multiply by 10**scale
	if scale >= 0 {
		r.Mul(r, new(big.Rat).SetInt(pow10Big(scale)))
	} else {
		r.Quo(r, new(big.Rat).SetInt(pow10Big(-scale)))
	}
	return r, i, hasDot || hasExp, true
}

// stringToR parses a Ruby rational literal (String#to_r / the String form of
// Kernel#Rational). In lenient mode it skips leading whitespace, ignores trailing
// characters, and returns Rational(0,1) when nothing parses; in strict mode it
// requires the whole string to be a clean rational, returning ok=false otherwise.
// A zero denominator always raises ZeroDivisionError.
func stringToR(s string, strict bool) (*object.Rational, bool) {
	i := numSkipWS(s, 0)
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	numR, j, _, ok := numScanDecimal(s, i)
	if !ok {
		if strict {
			return nil, false
		}
		return &object.Rational{R: new(big.Rat)}, true // Rational(0,1)
	}
	i = j
	r := new(big.Rat).Set(numR)
	if neg {
		r.Neg(r)
	}
	if i < len(s) && s[i] == '/' {
		if den, k, _, dok := numScanDecimal(s, i+1); dok {
			if den.Sign() == 0 {
				raise("ZeroDivisionError", "divided by 0")
			}
			r.Quo(r, den)
			i = k
		}
	}
	if strict {
		if numSkipWS(s, i) != len(s) {
			return nil, false
		}
	}
	return &object.Rational{R: r}, true
}

// numScanComponent parses one unsigned numeric component of a complex literal
// from s[i:], returning a typed value: a float form ('.' or exponent) yields a
// Float, an "a/b" form a Rational (kept as a ratio even when whole, so "4/2i" is
// (2/1)), and a plain run of digits an Integer/Bignum. A zero denominator ("2/0i")
// raises ZeroDivisionError, as MRI does. ok is false when no digit is present.
func numScanComponent(s string, i int) (object.Value, int, bool) {
	r, j, isFloat, ok := numScanDecimal(s, i)
	if !ok {
		return nil, i, false
	}
	i = j
	if isFloat {
		f, _ := r.Float64()
		return object.Float(f), i, true
	}
	if i < len(s) && s[i] == '/' {
		if den, k, _, dok := numScanDecimal(s, i+1); dok {
			if den.Sign() == 0 {
				raise("ZeroDivisionError", "divided by 0")
			}
			return &object.Rational{R: new(big.Rat).Quo(r, den)}, k, true
		}
	}
	return object.NormInt(r.Num()), i, true // integer: denominator is 1
}

// numScanSigned parses an optional sign followed by a numeric component. ok is
// false (and the index unchanged) when no component follows the sign.
func numScanSigned(s string, i int) (object.Value, int, bool) {
	j := i
	neg := false
	if j < len(s) && (s[j] == '+' || s[j] == '-') {
		neg = s[j] == '-'
		j++
	}
	v, k, ok := numScanComponent(s, j)
	if !ok {
		return nil, i, false
	}
	if neg {
		v = negate(v)
	}
	return v, k, true
}

// numScanImaginary parses the imaginary term "[sign][magnitude]?<unit>" from
// s[i:], where the unit is one of i/I/j/J. A bare unit means 1; a lone sign means
// ±1. ok is false (index unchanged) when no unit terminates the term.
func numScanImaginary(s string, i int) (object.Value, int, bool) {
	j := i
	neg := false
	if j < len(s) && (s[j] == '+' || s[j] == '-') {
		neg = s[j] == '-'
		j++
	}
	mag, k, magOk := numScanComponent(s, j)
	if magOk {
		j = k
	}
	if j < len(s) && numIsImagUnit(s[j]) {
		v := object.Value(object.IntValue(1))
		if magOk {
			v = mag
		}
		if neg {
			v = negate(v)
		}
		return v, j + 1, true
	}
	return nil, i, false
}

// stringToC parses a Ruby complex literal (String#to_c / the String form of
// Kernel#Complex): real, imaginary ("Ni"), "a+bi", and polar ("m@a") forms with
// integer/float/rational components. In lenient mode it skips leading whitespace,
// ignores trailing characters, and returns Complex(0,0) when nothing parses; in
// strict mode the whole string (bar surrounding whitespace) must parse, else
// ok=false.
func stringToC(s string, strict bool) (*object.Complex, bool) {
	zero := object.Value(object.IntValue(0))
	i := numSkipWS(s, 0)
	mk := func(re, im object.Value, end int) (*object.Complex, bool) {
		if strict && numSkipWS(s, end) != len(s) {
			return nil, false
		}
		return &object.Complex{Re: re, Im: im}, true
	}

	realVal, ri, realOk := numScanSigned(s, i)
	pos := i
	if realOk {
		pos = ri
	}

	// Polar form: <modulus> @ <argument>.
	if realOk && pos < len(s) && s[pos] == '@' {
		if ang, ai, aok := numScanSigned(s, pos+1); aok {
			c := complexPolar([]object.Value{realVal, ang}).(*object.Complex)
			return mk(c.Re, c.Im, ai)
		}
	}
	// <number> immediately followed by a unit: the number is the imaginary part.
	if realOk && pos < len(s) && numIsImagUnit(s[pos]) {
		return mk(zero, realVal, pos+1)
	}
	// An explicit imaginary term, optionally preceded by a real part.
	if imVal, ii, imOk := numScanImaginary(s, pos); imOk {
		re := zero
		if realOk {
			re = realVal
		}
		return mk(re, imVal, ii)
	}
	if realOk {
		return mk(realVal, zero, pos)
	}
	if strict {
		return nil, false
	}
	return &object.Complex{Re: zero, Im: zero}, true
}
