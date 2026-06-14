// Package object holds the runtime value representation.
//
// Phase 0 keeps Value minimal: a small interface plus concrete value types.
// Per plan-rbgo.md §4 (and the critique folded into it), the eventual design
// must add RClass(*VM) for dynamic dispatch and split Fixnum/Bignum so the
// common integer does not carry a nil *big.Int word. None of that exists yet —
// this is the deliberately-thin vertical slice.
package object

import (
	"math"
	"strconv"
	"strings"
)

// Value is the interface implemented by every Ruby value.
//
// ToS  backs to_s / puts / string interpolation.
// Inspect backs inspect / p.
// Truthy implements Ruby truthiness: everything is truthy except false and nil.
type Value interface {
	ToS() string
	Inspect() string
	Truthy() bool
}

// Integer is a 64-bit integer. Phase 2 will introduce a separate Bignum type
// backed by math/big and promote on overflow.
type Integer int64

func (i Integer) ToS() string     { return strconv.FormatInt(int64(i), 10) }
func (i Integer) Inspect() string { return i.ToS() }
func (i Integer) Truthy() bool    { return true }

// Float is a 64-bit float.
type Float float64

func (f Float) ToS() string {
	if math.IsInf(float64(f), 1) {
		return "Infinity"
	}
	if math.IsInf(float64(f), -1) {
		return "-Infinity"
	}
	if math.IsNaN(float64(f)) {
		return "NaN"
	}
	// Ruby always shows a decimal point for floats (1.0 not 1).
	s := strconv.FormatFloat(float64(f), 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
func (f Float) Inspect() string { return f.ToS() }
func (f Float) Truthy() bool    { return true }

// String is a Ruby string. Phase 2 replaces this with a []byte + *Encoding
// representation that supports mutation and multi-encoding (see plan §4, §11).
type String string

func (s String) ToS() string { return string(s) }
func (s String) Inspect() string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range string(s) {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
func (s String) Truthy() bool { return true }

// Symbol is an interned name (:foo). It is an immutable value type, so equality
// and use as a hash key are just value comparison.
type Symbol string

func (s Symbol) ToS() string     { return string(s) }
func (s Symbol) Inspect() string { return ":" + string(s) }
func (s Symbol) Truthy() bool    { return true }

// Bool is true or false.
type Bool bool

func (b Bool) ToS() string {
	if b {
		return "true"
	}
	return "false"
}
func (b Bool) Inspect() string { return b.ToS() }
func (b Bool) Truthy() bool    { return bool(b) }

// Nil is the single nil value.
type Nil struct{}

func (Nil) ToS() string     { return "" }
func (Nil) Inspect() string { return "nil" }
func (Nil) Truthy() bool    { return false }

// Singletons shared across the VM.
var (
	True  = Bool(true)
	False = Bool(false)
	NilV  = Nil{}
)

// Main is the top-level self ("main" object) used while executing the program
// body. Phase 1 promotes this to a real Object instance of class Object.
type Main struct{}

func (Main) ToS() string     { return "main" }
func (Main) Inspect() string { return "main" }
func (Main) Truthy() bool    { return true }
