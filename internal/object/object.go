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

// Array is a mutable, ordered list. It is a reference type (used as *Array), so
// aliasing and in-place mutation (push, []=) behave as in Ruby.
type Array struct{ Elems []Value }

func (a *Array) repr() string {
	var b strings.Builder
	b.WriteByte('[')
	for i, e := range a.Elems {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(e.Inspect())
	}
	b.WriteByte(']')
	return b.String()
}
func (a *Array) ToS() string     { return a.repr() }
func (a *Array) Inspect() string { return a.repr() }
func (a *Array) Truthy() bool    { return true }

// Hash is an insertion-ordered map (as in Ruby). Keys are compared by Go value
// equality, which matches Ruby eql?/hash for the immutable key types used in
// Phase 2 (Integer/Float/String/Symbol/Bool/Nil). It is a reference type.
type Hash struct {
	Keys []Value // insertion order
	vals map[Value]Value
}

// NewHash returns an empty hash.
func NewHash() *Hash { return &Hash{vals: map[Value]Value{}} }

// Get returns the value for k and whether it is present.
func (h *Hash) Get(k Value) (Value, bool) { v, ok := h.vals[k]; return v, ok }

// Set inserts or updates k→v, preserving first-insertion order.
func (h *Hash) Set(k, v Value) {
	if _, ok := h.vals[k]; !ok {
		h.Keys = append(h.Keys, k)
	}
	h.vals[k] = v
}

// Len returns the number of entries.
func (h *Hash) Len() int { return len(h.Keys) }

func (h *Hash) repr() string {
	if len(h.Keys) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range h.Keys {
		if i > 0 {
			b.WriteString(", ")
		}
		v := h.vals[k]
		// Ruby 4.0 (since 3.4) inspect: symbol keys use the label form
		// `name: value`; all other keys use `key => value` with spaces.
		if sym, ok := k.(Symbol); ok {
			b.WriteString(string(sym) + ": " + v.Inspect())
		} else {
			b.WriteString(k.Inspect() + " => " + v.Inspect())
		}
	}
	b.WriteByte('}')
	return b.String()
}
func (h *Hash) ToS() string     { return h.repr() }
func (h *Hash) Inspect() string { return h.repr() }
func (h *Hash) Truthy() bool    { return true }

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
