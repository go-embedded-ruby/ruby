// Package object holds the runtime value representation.
//
// Phase 0 keeps Value minimal: a small interface plus concrete value types.
// Per plan-rbgo.md §4 (and the critique folded into it), the eventual design
// must add RClass(*VM) for dynamic dispatch and split Fixnum/Bignum so the
// common integer does not carry a nil *big.Int word. None of that exists yet —
// this is the deliberately-thin vertical slice.
package object

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unsafe"
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
// A part may also be a Rational (e.g. the `2.5ri` literal yields
// Complex(0, Rational(5,2)), inspected as "(0+(5/2)*i)").
type Complex struct{ Re, Im Value }

func (c *Complex) ToS() string { return c.Re.ToS() + imagPart(c.Im) }

// Inspect renders the parenthesised form, e.g. "(1+2i)". A Rational component
// is shown in its own parenthesised inspect form and a Rational imaginary part
// is written as "(n/d)*i" (matching MRI: Complex(0, Rational(5,2)) → "(0+(5/2)*i)").
func (c *Complex) Inspect() string {
	re := c.Re.ToS()
	if r, ok := c.Re.(*Rational); ok {
		re = r.Inspect()
	}
	return "(" + re + imagPartInspect(c.Im) + ")"
}
func (c *Complex) Truthy() bool { return true }

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

// imagPartInspect renders the imaginary term for Complex#inspect. A Rational
// imaginary part is parenthesised and multiplied — "+(5/2)*i" / "-(1/2)*i" —
// while other numeric parts match imagPart ("+2i", "-3.0i").
func imagPartInspect(im Value) string {
	r, ok := im.(*Rational)
	if !ok {
		return imagPart(im)
	}
	sign := "+"
	rr := r.R
	if rr.Sign() < 0 {
		sign = "-"
		rr = new(big.Rat).Neg(rr)
	}
	return sign + "(" + rr.Num().String() + "/" + rr.Denom().String() + ")*i"
}

// String is a Ruby string: a mutable, reference-typed byte sequence (always used
// as *String), so aliasing and in-place mutation (<<, []=, replace, the bang
// methods) behave as in Ruby. Frozen marks a string that may not be mutated
// (Hash keys are frozen snapshots; a frozen literal raises on mutation).
//
// Copy-on-write: a String has two representations. An *owned* string holds a
// private, mutable byte slice in b (isView == false). A *view* string instead
// shares an immutable Go string in view (isView == true) and leaves b nil,
// avoiding the substring copy that dominates alloc-heavy paths such as
// String#split. Because Go strings are immutable, a view can never observe a
// mutation of whatever produced it, so views are provably free of source
// aliasing. The first in-place mutation calls ensureOwned, which copies the
// view into a private b and switches the representation; all reads go through
// the Bytes / Str / Len accessors, which work on both representations.
//
// Invariants: exactly one representation is live. When isView is true, view
// holds the content and b is nil; when isView is false, b holds the content and
// view is "".
type String struct {
	b      []byte // owned, mutable bytes (isView == false)
	view   string // shared immutable bytes (isView == true)
	isView bool
	Frozen bool
	// Enc is the encoding name, or "" for the UTF-8 default. Only a non-default
	// (e.g. "ASCII-8BIT") changes behaviour, so existing UTF-8 strings are
	// unaffected.
	Enc string
}

// NewString builds an owned String from a Go string, copying its bytes.
func NewString(s string) *String { return &String{b: []byte(s)} }

// NewStringBytes builds an owned String that takes ownership of b without
// copying it. The caller must not retain or mutate b afterwards.
func NewStringBytes(b []byte) *String { return &String{b: b} }

// NewStringBytesEnc is NewStringBytes with an explicit encoding tag.
func NewStringBytesEnc(b []byte, enc string) *String { return &String{b: b, Enc: enc} }

// NewFrozenStringView builds a frozen copy-on-write view over the immutable Go
// string s. Because it is frozen it never mutates, so the view is permanent and
// serves every read without copying — ideal for interned/AOT string literals.
func NewFrozenStringView(s string) *String { return &String{view: s, isView: true, Frozen: true} }

// TakeFrom makes s share o's representation (owned slice or view), used when o
// is a freshly produced, otherwise-discarded String so the transfer is
// zero-copy and cannot alias any live value. Enc and Frozen are left untouched.
func (s *String) TakeFrom(o *String) { s.b, s.view, s.isView = o.b, o.view, o.isView }

// NewStringView builds a copy-on-write String that shares s's immutable bytes
// without copying them. The first in-place mutation transparently materializes
// a private copy (see ensureOwned). Use this for substrings and other reads
// carved out of an already-immutable Go string.
func NewStringView(s string) *String { return &String{view: s, isView: true} }

// Bytes returns the string's bytes for READ-ONLY use, without copying. Callers
// MUST NOT mutate the returned slice; use MutableBytes for in-place mutation.
// For a view this aliases the underlying immutable Go string, so mutating it
// would corrupt every sibling sharing that source.
func (s *String) Bytes() []byte {
	if s.isView {
		return unsafe.Slice(unsafe.StringData(s.view), len(s.view))
	}
	return s.b
}

// Len returns the string's byte length on either representation.
func (s *String) Len() int {
	if s.isView {
		return len(s.view)
	}
	return len(s.b)
}

// ensureOwned materializes a private, mutable byte slice, converting a view to
// owned. It is idempotent and a no-op on an already-owned string. It does NOT
// check Frozen: mutation methods must raise FrozenError before calling it.
func (s *String) ensureOwned() {
	if s.isView {
		s.b = []byte(s.view)
		s.view = ""
		s.isView = false
	}
}

// MutableBytes returns the owned byte slice for in-place mutation, first
// materializing a private copy if the string is a view. Callers may freely
// mutate the result in place.
func (s *String) MutableBytes() []byte {
	s.ensureOwned()
	return s.b
}

// SetBytes replaces the string's contents with b, taking ownership of it and
// switching to the owned representation.
func (s *String) SetBytes(b []byte) {
	s.b = b
	s.view = ""
	s.isView = false
}

// Str returns the string's contents as a Go string.
func (s *String) Str() string {
	if s.isView {
		return s.view
	}
	return string(s.b)
}

// EncName returns the string's encoding name, defaulting to UTF-8.
func (s *String) EncName() string {
	if s.Enc == "" {
		return "UTF-8"
	}
	return s.Enc
}

// IsBinary reports whether the string is tagged ASCII-8BIT (BINARY), in which
// case it is treated as opaque bytes (length counts bytes, not characters).
func (s *String) IsBinary() bool { return s.Enc == "ASCII-8BIT" }

// Dup returns an unfrozen shallow copy with its own backing array, preserving the
// encoding.
func (s *String) Dup() *String {
	src := s.Bytes()
	b := make([]byte, len(src))
	copy(b, src)
	return &String{b: b, Enc: s.Enc}
}

func (s *String) ToS() string { return s.Str() }
func (s *String) Inspect() string {
	var b strings.Builder
	b.WriteByte('"')
	rs := []rune(s.Str())
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
	// keyBucket maps a stored user-object key (snapshot) to the customBucket it
	// hashes to, so reuseBucket can find an #eql? sibling and collapse it to the
	// same entry. Only populated for keys routed through CustomKeyHook; lazily
	// created on first such insertion.
	keyBucket map[Value]customBucket
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

// CustomKeyHook lets the vm package supply Ruby-level hash/eql? semantics for a
// key that is neither a plain value type nor a built-in subclass — i.e. a user
// object that overrides #hash and #eql?. Given such a key it returns the int64
// the object's #hash method yields and a predicate that reports whether a stored
// key is #eql? to it; ok is false for any key that should keep identity-based
// behaviour (the default Object#hash/#eql?). Set once by the VM at construction;
// nil in tests that never build a VM (then every object key uses identity).
var CustomKeyHook func(k Value) (rubyHash int64, eql func(stored Value) bool, ok bool)

// customBucket is the comparable map key minted for a user object whose #hash and
// #eql? are overridden. seq disambiguates distinct (not #eql?) objects that share
// a #hash value; reuseBucket assigns matching keys the same bucket so #eql? keys
// collapse to one entry exactly as MRI's open-addressed table would.
type customBucket struct {
	hash int64
	seq  int
}

// hashKey normalises a key to its comparable map form. Plain value types and the
// recursively content-addressable collections (Array/Hash) reduce to a content
// key so that #eql? values coincide; a user object with an overridden #hash/#eql?
// is routed through CustomKeyHook and reuseBucket so it follows Ruby semantics
// rather than Go pointer identity.
func (h *Hash) hashKey(k Value) any {
	if u, ok := k.(KeyUnwrapper); ok {
		if v, wrapped := u.HashUnwrap(); wrapped {
			k = v
		}
	}
	switch kk := k.(type) {
	case *String:
		return strKey(kk.Bytes())
	// Immediate value types are their own comparable key: Ruby fixnums, floats,
	// symbols, true/false and nil hash and compare by value (1.eql?(1),
	// :a.eql?(:a)), and none can be subclassed to override #hash, so they never
	// need the CustomKeyHook / #hash-method walk below. Returning them directly is
	// byte-identical to the old fall-through (the hook reported ok=false for them
	// and returned k) but skips a full method-resolution per Get/Set — the hot
	// cost on an Integer- or Symbol-keyed Hash.
	case Integer:
		return kk
	case Float:
		return kk
	case Symbol:
		return kk
	case Bool:
		return kk
	case Nil:
		return kk
	case *Bignum:
		return "\x00big:" + kk.I.String()
	case *Array:
		var b strings.Builder
		b.WriteString("\x00arr:")
		for _, e := range kk.Elems {
			fmt.Fprintf(&b, "%v\x01", h.hashKey(e))
		}
		return b.String()
	case *Hash:
		var b strings.Builder
		b.WriteString("\x00hsh:")
		for _, e := range kk.Keys {
			v, _ := kk.Get(e)
			fmt.Fprintf(&b, "%v\x02%v\x01", h.hashKey(e), h.valKey(v))
		}
		return b.String()
	}
	if CustomKeyHook != nil {
		if rh, eql, ok := CustomKeyHook(k); ok {
			return h.reuseBucket(rh, eql)
		}
	}
	return k
}

// valKey is the content key for a value appearing on the *value* side of a nested
// Hash that is itself used as a Hash key. It reuses hashKey so the serialisation
// is consistent (two #eql? nested hashes produce the same key string).
func (h *Hash) valKey(v Value) any { return h.hashKey(v) }

// reuseBucket returns the customBucket for a user object with Ruby hash rh and
// #eql? predicate eql: if an existing key in this hash is #eql?, its bucket is
// reused (so the new key updates that entry); otherwise a fresh seq is minted at
// the same hash so non-#eql? collisions stay distinct.
func (h *Hash) reuseBucket(rh int64, eql func(Value) bool) customBucket {
	maxSeq := -1
	for _, key := range h.Keys {
		cb, ok := h.keyBucket[key]
		if !ok || cb.hash != rh {
			continue
		}
		if eql(key) {
			return cb
		}
		if cb.seq > maxSeq {
			maxSeq = cb.seq
		}
	}
	return customBucket{hash: rh, seq: maxSeq + 1}
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

// NewHashCap returns an empty hash whose backing map and key slice are pre-sized
// for about n entries, so a bulk builder (e.g. JSON.parse of a known-width
// object) fills it without re-growing. A negative n behaves like NewHash.
func NewHashCap(n int) *Hash {
	if n < 0 {
		n = 0
	}
	return &Hash{vals: make(map[any]Value, n), Keys: make([]Value, 0, n)}
}

// Get returns the value for k and whether it is present.
func (h *Hash) Get(k Value) (Value, bool) { v, ok := h.vals[h.hashKey(k)]; return v, ok }

// Set inserts or updates k→v, preserving first-insertion order.
func (h *Hash) Set(k, v Value) {
	hk := h.hashKey(k)
	if _, ok := h.vals[hk]; !ok {
		snap := snapshotKey(k)
		h.Keys = append(h.Keys, snap)
		if cb, isCustom := hk.(customBucket); isCustom {
			if h.keyBucket == nil {
				h.keyBucket = map[Value]customBucket{}
			}
			h.keyBucket[snap] = cb
		}
	}
	h.vals[hk] = v
}

// Len returns the number of entries.
func (h *Hash) Len() int { return len(h.Keys) }

// Clear removes every entry, leaving an empty hash (the Default/DefaultProc are
// kept, as in MRI).
func (h *Hash) Clear() {
	h.Keys = nil
	h.vals = map[any]Value{}
	h.keyBucket = nil
}

// Delete removes k, returning its value and whether it was present.
func (h *Hash) Delete(k Value) (Value, bool) {
	hk := h.hashKey(k)
	v, ok := h.vals[hk]
	if !ok {
		return NilV, false
	}
	delete(h.vals, hk)
	for i, key := range h.Keys {
		if h.hashKey(key) == hk {
			if h.keyBucket != nil {
				delete(h.keyBucket, key)
			}
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
		v := h.vals[h.hashKey(k)]
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
func (r *Range) Truthy() bool { return true }

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
