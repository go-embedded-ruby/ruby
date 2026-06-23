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
	"math/big"
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

// Integer is a 64-bit integer (the common case). Arithmetic that overflows
// int64 promotes to a Bignum; both report their class as Integer, so the
// Fixnum/Bignum split is transparent (as in Ruby 4.0).
type Integer int64

func (i Integer) ToS() string     { return strconv.FormatInt(int64(i), 10) }
func (i Integer) Inspect() string { return i.ToS() }
func (i Integer) Truthy() bool    { return true }

// Bignum is an arbitrary-precision integer (the int64 overflow case). It is held
// immutably: results are always fresh big.Ints, never mutated in place.
type Bignum struct{ I *big.Int }

func (b *Bignum) ToS() string     { return b.I.String() }
func (b *Bignum) Inspect() string { return b.I.String() }
func (b *Bignum) Truthy() bool    { return true }

// NormInt returns an Integer when z fits in int64, else a Bignum — so a result
// that shrinks back into range demotes automatically.
func NormInt(z *big.Int) Value {
	if z.IsInt64() {
		return IntValue(z.Int64())
	}
	return &Bignum{I: z}
}

// BigOf returns the big.Int value of an Integer or Bignum (ok=false otherwise).
func BigOf(v Value) (*big.Int, bool) {
	switch x := v.(type) {
	case Integer:
		return big.NewInt(int64(x)), true
	case *Bignum:
		return x.I, true
	}
	return nil, false
}

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

// Complex is a Ruby Complex number. Its real and imaginary parts are themselves
// numeric Values (Integer/Bignum/Float), so Complex(1, 2) keeps Integer parts
// and renders "(1+2i)" while Complex(1.5, 2) renders "(1.5+2i)", matching MRI.
// (Rational components are not supported — there is no Rational type — so exact
// division of integer components yields Float parts instead.)
type Complex struct{ Re, Im Value }

func (c *Complex) ToS() string     { return c.Re.ToS() + imagPart(c.Im) }
func (c *Complex) Inspect() string { return "(" + c.ToS() + ")" }
func (c *Complex) Truthy() bool    { return true }

// Rational is an exact ratio of two integers (Ruby Rational), held as a
// normalised math/big.Rat — always reduced, with a positive denominator — so
// Rational(4, 2) prints "2/1" and Rational(1, -2) prints "-1/2", matching MRI.
type Rational struct{ R *big.Rat }

func (r *Rational) ToS() string     { return r.R.Num().String() + "/" + r.R.Denom().String() }
func (r *Rational) Inspect() string { return "(" + r.ToS() + ")" }
func (r *Rational) Truthy() bool    { return true }

// imagPart renders the imaginary term with its sign, e.g. "+2i" or "-2.0i". The
// component's own ToS already carries a leading "-" when negative, so the sign
// is taken from it directly (no negation, which keeps Bignum/edge values exact).
func imagPart(im Value) string {
	s := im.ToS()
	if strings.HasPrefix(s, "-") {
		return s + "i"
	}
	return "+" + s + "i"
}

// String is a Ruby string: a mutable, reference-typed byte sequence (always used
// as *String), so aliasing and in-place mutation (<<, []=, replace, the bang
// methods) behave as in Ruby. Frozen marks a string that may not be mutated
// (Hash keys are frozen snapshots; a frozen literal raises on mutation).
type String struct {
	B      []byte
	Frozen bool
}

// NewString builds a String from a Go string.
func NewString(s string) *String { return &String{B: []byte(s)} }

// Str returns the string's contents as a Go string.
func (s *String) Str() string { return string(s.B) }

// Dup returns an unfrozen shallow copy with its own backing array.
func (s *String) Dup() *String {
	b := make([]byte, len(s.B))
	copy(b, s.B)
	return &String{B: b}
}

func (s *String) ToS() string { return string(s.B) }
func (s *String) Inspect() string {
	var b strings.Builder
	b.WriteByte('"')
	rs := []rune(string(s.B))
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '#':
			// Escape `#` only before the interpolation sigils, as Ruby does, so the
			// inspected form round-trips.
			if i+1 < len(rs) && (rs[i+1] == '{' || rs[i+1] == '$' || rs[i+1] == '@') {
				b.WriteString(`\#`)
			} else {
				b.WriteByte('#')
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
func (s *String) Truthy() bool { return true }

// Symbol is an interned name (:foo). It is an immutable value type, so equality
// and use as a hash key are just value comparison.
type Symbol string

func (s Symbol) ToS() string { return string(s) }
func (s Symbol) Inspect() string {
	if isPlainSymbol(string(s)) {
		return ":" + string(s)
	}
	// A symbol that is not a bare identifier/operator is quoted like a string.
	return ":" + NewString(string(s)).Inspect()
}
func (s Symbol) Truthy() bool { return true }

// plainOperatorSymbols are the operator names a symbol prints bare (`:+`).
var plainOperatorSymbols = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true, "**": true,
	"==": true, "===": true, "!=": true, "<": true, ">": true, "<=": true,
	">=": true, "<=>": true, "<<": true, ">>": true, "&": true, "|": true,
	"^": true, "~": true, "!": true, "[]": true, "[]=": true, "+@": true,
	"-@": true, "=~": true,
}

// isPlainSymbol reports whether s prints as a bare `:name` rather than a quoted
// `:"…"`: an operator name, or an identifier (optionally @/@@/$-prefixed, with a
// single trailing ? ! or =).
func isPlainSymbol(s string) bool {
	if s == "" {
		return false
	}
	if plainOperatorSymbols[s] {
		return true
	}
	i := 0
	switch {
	case strings.HasPrefix(s, "@@"):
		i = 2
	case s[0] == '@' || s[0] == '$':
		i = 1
	}
	if i >= len(s) || !(isSymLetter(s[i]) || s[i] == '_') {
		return false
	}
	for i++; i < len(s); i++ {
		c := s[i]
		if isSymLetter(c) || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		// A trailing ? ! or = is allowed only as the final character.
		return (c == '?' || c == '!' || c == '=') && i == len(s)-1
	}
	return true
}

func isSymLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

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

// Hash is an insertion-ordered map (as in Ruby). Keys are normalised by hashKey:
// a String keys by its byte content (Ruby dups+freezes string keys, so a stored
// key is a frozen snapshot), every other value keys by itself — value types by
// value, reference types by identity. It is a reference type.
type Hash struct {
	Keys []Value // insertion order (string keys held as frozen snapshots)
	vals map[any]Value
	// Default is the value returned for a missing key (Hash.new(default)); nil
	// means none. DefaultProc, when set, is a Proc (held as a Value to avoid an
	// import cycle) called with (hash, key) on a miss (Hash.new { … }); it takes
	// precedence over Default. Both nil ⇒ a missing key reads as nil.
	Default     Value
	DefaultProc Value
}

// strKey is the comparable map key for a Ruby String, distinct from a Symbol of
// the same name (different dynamic type ⇒ no collision in an `any` map key).
type strKey string

// hashKey normalises a key to its comparable map form.
// KeyUnwrapper is implemented by a wrapper around a built-in value (an instance
// of a user subclass of String/Array/Hash) so that, used as a Hash key, it hashes
// and compares as the value it wraps rather than by object identity. Defined here
// (not in the vm package) to keep the hash key logic free of an import cycle.
type KeyUnwrapper interface {
	HashUnwrap() (Value, bool)
}

func hashKey(k Value) any {
	if u, ok := k.(KeyUnwrapper); ok {
		if v, wrapped := u.HashUnwrap(); wrapped {
			k = v
		}
	}
	if s, ok := k.(*String); ok {
		return strKey(s.B)
	}
	return k
}

// snapshotKey is the value remembered in Keys for iteration/inspect: a string
// key is stored as a frozen copy so mutating the original does not change it.
func snapshotKey(k Value) Value {
	if s, ok := k.(*String); ok {
		d := s.Dup()
		d.Frozen = true
		return d
	}
	return k
}

// NewHash returns an empty hash.
func NewHash() *Hash { return &Hash{vals: map[any]Value{}} }

// Get returns the value for k and whether it is present.
func (h *Hash) Get(k Value) (Value, bool) { v, ok := h.vals[hashKey(k)]; return v, ok }

// Set inserts or updates k→v, preserving first-insertion order.
func (h *Hash) Set(k, v Value) {
	hk := hashKey(k)
	if _, ok := h.vals[hk]; !ok {
		h.Keys = append(h.Keys, snapshotKey(k))
	}
	h.vals[hk] = v
}

// Len returns the number of entries.
func (h *Hash) Len() int { return len(h.Keys) }

// Delete removes k, returning its value and whether it was present.
func (h *Hash) Delete(k Value) (Value, bool) {
	hk := hashKey(k)
	v, ok := h.vals[hk]
	if !ok {
		return NilV, false
	}
	delete(h.vals, hk)
	for i, key := range h.Keys {
		if hashKey(key) == hk {
			h.Keys = append(h.Keys[:i], h.Keys[i+1:]...)
			break
		}
	}
	return v, true
}

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
		v := h.vals[hashKey(k)]
		// Ruby 4.0 (since 3.4) inspect: symbol keys use the label form
		// `name: value`; all other keys use `key => value` with spaces.
		if sym, ok := k.(Symbol); ok {
			// A plain symbol key uses the bare label `name:`; one that needs
			// quoting uses the quoted label `"name":`.
			if isPlainSymbol(string(sym)) {
				b.WriteString(string(sym) + ": " + v.Inspect())
			} else {
				b.WriteString(NewString(string(sym)).Inspect() + ": " + v.Inspect())
			}
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

// Range is Lo..Hi (Exclusive ? "...". A reference type; immutable in practice.
type Range struct {
	Lo, Hi    Value
	Exclusive bool
}

func (r *Range) sep() string {
	if r.Exclusive {
		return "..."
	}
	return ".."
}
func (r *Range) ToS() string     { return rangeEnd(r.Lo, false) + r.sep() + rangeEnd(r.Hi, false) }
func (r *Range) Inspect() string { return rangeEnd(r.Lo, true) + r.sep() + rangeEnd(r.Hi, true) }

// rangeEnd renders one endpoint, or "" for a nil (beginless/endless) bound.
func rangeEnd(v Value, inspect bool) string {
	if _, ok := v.(Nil); ok {
		return ""
	}
	if inspect {
		return v.Inspect()
	}
	return v.ToS()
}
func (r *Range) Truthy() bool    { return true }

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
// body. It is a real object with its own instance-variable table, so top-level
// @ivars persist (and are shared with top-level method bodies, whose self is
// also main).
type Main struct{ ivars map[string]Value }

// NewMain returns the top-level self with an empty instance-variable table.
func NewMain() *Main { return &Main{ivars: map[string]Value{}} }

func (m *Main) ToS() string     { return "main" }
func (m *Main) Inspect() string { return "main" }
func (m *Main) Truthy() bool    { return true }

// IvarTable exposes main's instance variables to the VM.
func (m *Main) IvarTable() map[string]Value { return m.ivars }
