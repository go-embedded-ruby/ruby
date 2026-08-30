package vm

import (
	encbinary "encoding/binary"
	"math"
	"math/big"
	"strconv"
	"strings"
	stdtime "time"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file implements a self-contained, MRI-byte-exact Ruby Marshal (format
// version 4.8) encoder and decoder for the Marshal module (see marshal.go).
// Unlike the standalone go-ruby-marshal engine (still used by pstore /
// sinatra_session for their scalar payloads), this codec lives in the VM so it
// can dispatch the user hooks — #marshal_dump / #marshal_load and the
// class-level _dump / _load protocol — and reconstruct the full object model
// (Object ivars, Struct, Range, Regexp, Time, Rational/Complex, Class/Module
// references) with a single shared symbol table and object-link counter, so
// shared and cyclic structures encode and decode exactly as they do in MRI.

const (
	marshalMajor = 4
	marshalMinor = 8

	// Fixnum marshal range: integers in [fixnumMin, fixnumMax] use the compact
	// 'i' form; everything outside uses the Bignum 'l' form. This is MRI's
	// marshal boundary, independent of the platform word size.
	marshalFixnumMin = -(1 << 30)
	marshalFixnumMax = (1 << 30) - 1
)

// --- encoder ----------------------------------------------------------------

type mDumper struct {
	vm     *VM
	buf    []byte
	syms   map[string]int       // symbol name -> symbol-table index
	objs   map[object.Value]int // composite/object identity -> object-link index
	nextID int
	// limit is the remaining recursion budget (Marshal.dump's third argument).
	// It counts down one level per nested value; a negative limit (the default
	// -1) never reaches zero and so imposes no bound. Reaching zero at the entry
	// of any value raises, matching MRI's "exceed depth limit".
	limit int
}

// marshalDump returns the Ruby Marshal encoding of v, honouring the given
// recursion limit (-1 for unlimited).
func (vm *VM) marshalDump(v object.Value, limit int) []byte {
	d := &mDumper{vm: vm, syms: map[string]int{}, objs: map[object.Value]int{}, limit: limit}
	d.buf = []byte{marshalMajor, marshalMinor}
	d.writeValue(v)
	return d.buf
}

// newID consumes the next object-link index for a linkable value that is not
// tracked by identity (Float, Bignum, and the internal payload strings).
func (d *mDumper) newID() { d.nextID++ }

// link records v under a fresh object id and returns false, or, if v was seen
// before, emits the '@' link to its id and returns true.
func (d *mDumper) link(v object.Value) bool {
	if id, ok := d.objs[v]; ok {
		d.buf = append(d.buf, '@')
		d.writeLong(id)
		return true
	}
	d.objs[v] = d.nextID
	d.nextID++
	return false
}

func (d *mDumper) writeValue(v object.Value) {
	// Enforce Marshal.dump's recursion limit: a zero budget at the entry of any
	// value is an error, and each nested value is written with one less. A
	// negative budget (the -1 default) never hits zero, so it is unbounded.
	if d.limit == 0 {
		raise("ArgumentError", "exceed depth limit")
	}
	d.limit--
	defer func() { d.limit++ }()
	switch x := v.(type) {
	case object.Nil:
		d.buf = append(d.buf, '0')
	case object.Bool:
		if bool(x) {
			d.buf = append(d.buf, 'T')
		} else {
			d.buf = append(d.buf, 'F')
		}
	case object.Symbol:
		d.writeSymbol(string(x))
	case object.Integer:
		d.writeInt(big.NewInt(int64(x)))
	case *object.Bignum:
		// A Bignum is a heap object, so repeated references link like any other
		// composite (unlike a Fixnum-range Integer, which is immediate).
		if d.link(x) {
			return
		}
		d.writeBignumBody(x.I)
	case object.Float:
		d.newID()
		d.buf = append(d.buf, 'f')
		d.writeBytes(marshalFloatString(float64(x)))
	case *object.String:
		d.writeString(x)
	case *object.Array:
		if d.link(x) {
			return
		}
		d.buf = append(d.buf, '[')
		d.writeLong(len(x.Elems))
		for _, e := range x.Elems {
			d.writeValue(e)
		}
	case *object.Hash:
		if d.link(x) {
			return
		}
		d.writeHash(x)
	case *object.Range:
		if d.link(x) {
			return
		}
		d.buf = append(d.buf, 'o')
		d.writeSymbol("Range")
		d.writeLong(3)
		d.writeSymbol("excl")
		d.writeValue(object.Bool(x.Exclusive))
		d.writeSymbol("begin")
		d.writeValue(x.Lo)
		d.writeSymbol("end")
		d.writeValue(x.Hi)
	case *Regexp:
		if d.link(x) {
			return
		}
		d.writeRegexp(x)
	case *object.Complex:
		if d.link(x) {
			return
		}
		d.emitUserMarshal("Complex", object.NewArray(x.Re, x.Im))
	case *object.Rational:
		if d.link(x) {
			return
		}
		num := object.NormInt(new(big.Int).Set(x.R.Num()))
		den := object.NormInt(new(big.Int).Set(x.R.Denom()))
		d.emitUserMarshal("Rational", object.NewArray(num, den))
	case *Time:
		if d.link(x) {
			return
		}
		d.writeTime(x)
	case *RClass:
		if d.link(x) {
			return
		}
		d.writeClassRef(x)
	case *RObject:
		d.writeObject(x)
	default:
		raise("TypeError", "no _dump_data is defined for class %s", d.vm.classOf(v).name)
	}
}

func (d *mDumper) writeInt(b *big.Int) {
	if b.IsInt64() {
		if n := b.Int64(); n >= marshalFixnumMin && n <= marshalFixnumMax {
			d.buf = append(d.buf, 'i')
			d.writeLong(int(n))
			return
		}
	}
	// An Integer outside the marshal Fixnum range dumps in the Bignum 'l' form
	// and consumes an object-link id, but is immediate so it is never linked.
	d.newID()
	d.writeBignumBody(b)
}

// writeBignumBody emits the 'l' Bignum payload (sign, 16-bit-short count,
// little-endian magnitude). It does not allocate an object-link id; the caller
// does that (writeInt via newID, or writeValue via link for a *Bignum object).
func (d *mDumper) writeBignumBody(b *big.Int) {
	d.buf = append(d.buf, 'l')
	if b.Sign() < 0 {
		d.buf = append(d.buf, '-')
	} else {
		d.buf = append(d.buf, '+')
	}
	le := marshalLEAbs(b)
	d.writeLong(len(le) / 2)
	d.buf = append(d.buf, le...)
}

func (d *mDumper) writeString(s *object.String) {
	if d.link(s) {
		return
	}
	enc := s.EncName()
	if enc == "ASCII-8BIT" {
		d.buf = append(d.buf, '"')
		d.writeBytes(s.Str())
		return
	}
	d.buf = append(d.buf, 'I', '"')
	d.writeBytes(s.Str())
	switch enc {
	case "UTF-8":
		d.writeLong(1)
		d.writeSymbol("E")
		d.buf = append(d.buf, 'T')
	case "US-ASCII":
		d.writeLong(1)
		d.writeSymbol("E")
		d.buf = append(d.buf, 'F')
	default:
		d.writeLong(1)
		d.writeSymbol("encoding")
		d.writeValue(object.NewStringBytesEnc([]byte(enc), "ASCII-8BIT"))
	}
}

func (d *mDumper) writeHash(h *object.Hash) {
	if !object.IsNil(h.DefaultProc) {
		raise("TypeError", "can't dump hash with default proc")
	}
	// A Hash put into compare_by_identity mode carries no inline flag in the
	// stream, so MRI wraps it in a 'C' container naming the Hash class; loading
	// that container re-applies compare_by_identity.
	if h.Identity {
		d.buf = append(d.buf, 'C')
		d.writeSymbol("Hash")
	}
	if h.Default != nil {
		d.buf = append(d.buf, '}')
	} else {
		d.buf = append(d.buf, '{')
	}
	d.writeLong(len(h.Keys))
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		d.writeValue(k)
		d.writeValue(v)
	}
	if h.Default != nil {
		d.writeValue(h.Default)
	}
}

func (d *mDumper) writeRegexp(r *Regexp) {
	nonASCII := !marshalIsASCII(r.source)
	opt := 0
	if strings.ContainsRune(r.flags, 'i') {
		opt |= reIgnoreCase
	}
	if strings.ContainsRune(r.flags, 'x') {
		opt |= reExtended
	}
	if strings.ContainsRune(r.flags, 'm') {
		opt |= reMultiline
	}
	if nonASCII {
		opt |= 16 // ARG_ENCODING_FIXED, set for a non-ASCII (UTF-8) source
	}
	d.buf = append(d.buf, 'I', '/')
	d.writeBytes(r.source)
	d.buf = append(d.buf, byte(opt))
	d.writeLong(1)
	d.writeSymbol("E")
	if nonASCII {
		d.buf = append(d.buf, 'T')
	} else {
		d.buf = append(d.buf, 'F')
	}
}

func (d *mDumper) writeClassRef(c *RClass) {
	if c.name == "" {
		if c.isModule {
			raise("TypeError", "can't dump anonymous module %s", c.ToS())
		}
		raise("TypeError", "can't dump anonymous class %s", c.ToS())
	}
	if c.isModule {
		d.buf = append(d.buf, 'm')
	} else {
		d.buf = append(d.buf, 'c')
	}
	d.writeBytes(c.name)
}

// writeTime emits Time's user-defined dump: the 8-byte packed form (MRI's
// time_mdump), I-wrapped with the :zone / :offset ivars.
// timeDumpBytes packs a Time into MRI's 8-byte marshal/_dump form (two
// little-endian uint32s: the date/UTC-flag word and the time/usec word).
func timeDumpBytes(t *Time) []byte {
	tt := t.t.UTC()
	year, mon, mday := tt.Date()
	hour, min, sec := tt.Clock()
	usec := tt.Nanosecond() / 1000
	p := uint32(0x80000000) | uint32((year-1900)<<14) | uint32((int(mon)-1)<<10) | uint32(mday<<5) | uint32(hour)
	if t.t.Location() == stdtime.UTC {
		p |= 0x40000000
	}
	s := uint32(min<<26) | uint32(sec<<20) | uint32(usec)
	var payload [8]byte
	encbinary.LittleEndian.PutUint32(payload[0:4], p)
	encbinary.LittleEndian.PutUint32(payload[4:8], s)
	return payload[:]
}

func (d *mDumper) writeTime(t *Time) {
	utc := t.t.Location() == stdtime.UTC
	payload := timeDumpBytes(t)

	d.buf = append(d.buf, 'I', 'u')
	d.writeSymbol("Time")
	d.writeBytes(string(payload))
	if utc {
		d.writeLong(1)
		d.writeSymbol("zone")
		d.writeValue(object.NewStringBytesEnc([]byte("UTC"), "US-ASCII"))
		return
	}
	_, off := t.t.Zone()
	d.writeLong(2)
	d.writeSymbol("offset")
	d.writeValue(object.Integer(off))
	d.writeSymbol("zone")
	d.buf = append(d.buf, '0')
}

// writeObject dispatches an ordinary instance: the #marshal_dump hook (U), the
// class _dump hook (u), a Struct (S), or the generic ivar object (o).
func (d *mDumper) writeObject(o *RObject) {
	if d.link(o) {
		return
	}
	// Every path below names o.class; an instance of an anonymous class cannot be
	// dumped, whatever hook (marshal_dump / _dump / Struct / builtin subclass /
	// plain object) it would otherwise take.
	if o.class.name == "" {
		raise("TypeError", "can't dump anonymous class %s", o.class.ToS())
	}
	// An object singleton-extended by one or more modules is prefixed with an 'e'
	// container per module (last-extended first, matching MRI's ancestry order).
	if o.singleton != nil {
		inc := o.singleton.includes
		for i := len(inc) - 1; i >= 0; i-- {
			if inc[i].name == "" {
				raise("TypeError", "can't dump anonymous class %s", inc[i].ToS())
			}
			d.buf = append(d.buf, 'e')
			d.writeSymbol(inc[i].name)
		}
	}
	if d.vm.respondsTo(o, "marshal_dump") {
		val := d.vm.send(o, "marshal_dump", nil, nil)
		d.emitUserMarshal(o.class.name, val)
		return
	}
	if d.vm.respondsTo(o, "_dump") {
		s := d.vm.send(o, "_dump", []object.Value{object.Integer(-1)}, nil)
		str, ok := s.(*object.String)
		if !ok {
			raise("TypeError", "_dump() must return String")
		}
		d.buf = append(d.buf, 'u')
		d.writeSymbol(o.class.name)
		d.writeBytes(str.Str())
		return
	}
	if sd := structDefOf(o.class); sd != nil {
		d.buf = append(d.buf, 'S')
		d.writeSymbol(o.class.name)
		d.writeLong(len(sd.names))
		for i, name := range sd.names {
			d.writeSymbol(name)
			d.writeValue(o.structVals[i])
		}
		return
	}
	// An instance of a user subclass of a built-in value type (Array/Hash/
	// String/Range/...) dumps as 'C' — the class name followed by the wrapped
	// built-in value. Any instance variables are carried by an outer 'I' wrapper,
	// as MRI does for objects whose payload type has no inline ivar slot.
	if o.builtin != nil {
		names := o.liveIvarNames()
		if len(names) > 0 {
			d.buf = append(d.buf, 'I')
		}
		d.buf = append(d.buf, 'C')
		d.writeSymbol(o.class.name)
		d.writeValue(o.builtin)
		if len(names) > 0 {
			d.writeLong(len(names))
			for _, name := range names {
				d.writeSymbol(name)
				d.writeValue(o.ivars[name])
			}
		}
		return
	}
	// A plain object (no marshal_dump / _dump / Struct / builtin payload) whose
	// singleton class carries per-object methods (def obj.foo) or its own ivars
	// (class << obj; @v = …) cannot be marshaled. A singleton that only holds
	// extend-ed modules has no own methods or ivars and is emitted via 'e' above.
	if o.singleton != nil && (len(o.singleton.methods) > 0 || len(o.singleton.ivars) > 0) {
		raise("TypeError", "singleton can't be dumped")
	}
	if d.vm.marshalIsException(o.class) {
		d.writeException(o)
		return
	}
	d.buf = append(d.buf, 'o')
	d.writeSymbol(o.class.name)
	names := o.liveIvarNames()
	d.writeLong(len(names))
	for _, name := range names {
		d.writeSymbol(name)
		d.writeValue(o.ivars[name])
	}
}

// writeException emits an Exception (or subclass) instance. MRI leads with the
// :mesg and :bt pseudo-ivars (the message and backtrace, or nil), which rbgo
// keeps in @message and @__backtrace__, and then the object's remaining ivars in
// assignment order.
func (d *mDumper) writeException(o *RObject) {
	var others []string
	for _, n := range o.liveIvarNames() {
		if n == "@message" || n == "@__backtrace__" {
			continue
		}
		others = append(others, n)
	}
	d.buf = append(d.buf, 'o')
	d.writeSymbol(o.class.name)
	d.writeLong(2 + len(others))
	d.writeSymbol("mesg")
	d.writeExceptionField(o.ivars["@message"])
	d.writeSymbol("bt")
	d.writeExceptionField(o.ivars["@__backtrace__"])
	for _, n := range others {
		d.writeSymbol(n)
		d.writeValue(o.ivars[n])
	}
}

// writeExceptionField writes a :mesg / :bt value, emitting nil when the backing
// ivar is unset or nil.
func (d *mDumper) writeExceptionField(v object.Value) {
	if v == nil || object.IsNil(v) {
		d.buf = append(d.buf, '0')
		return
	}
	d.writeValue(v)
}

// liveIvarNames returns the object's instance-variable names in first-assignment
// order, dropping any that have since been removed.
func (o *RObject) liveIvarNames() []string {
	out := make([]string, 0, len(o.ivarOrder))
	for _, n := range o.ivarOrder {
		if _, live := o.ivars[n]; live {
			out = append(out, n)
		}
	}
	return out
}

func (d *mDumper) emitUserMarshal(className string, val object.Value) {
	d.buf = append(d.buf, 'U')
	d.writeSymbol(className)
	d.writeValue(val)
}

func (d *mDumper) writeSymbol(name string) {
	if id, ok := d.syms[name]; ok {
		d.buf = append(d.buf, ';')
		d.writeLong(id)
		return
	}
	// A symbol carrying non-ASCII bytes is, by rbgo's UTF-8 default, a UTF-8
	// symbol; MRI wraps such a symbol in an 'I' container with the :E => true
	// encoding ivar (a pure-ASCII symbol stays a bare ':' with no wrapper). The
	// symbol itself is interned before the E ivar so their table indices match
	// MRI's first-appearance order and a repeated encoded symbol links back.
	if !marshalIsASCII(name) {
		d.buf = append(d.buf, 'I')
		d.syms[name] = len(d.syms)
		d.buf = append(d.buf, ':')
		d.writeBytes(name)
		d.writeLong(1)
		d.writeSymbol("E")
		d.buf = append(d.buf, 'T')
		return
	}
	d.syms[name] = len(d.syms)
	d.buf = append(d.buf, ':')
	d.writeBytes(name)
}

// writeBytes emits a length-prefixed byte string (no type tag).
func (d *mDumper) writeBytes(s string) {
	d.writeLong(len(s))
	d.buf = append(d.buf, s...)
}

// writeLong emits n in Ruby's packed "long" encoding (marshal's w_long).
func (d *mDumper) writeLong(n int) {
	if n == 0 {
		d.buf = append(d.buf, 0)
		return
	}
	if n > 0 && n < 123 {
		d.buf = append(d.buf, byte(n+5))
		return
	}
	if n < 0 && n > -124 {
		d.buf = append(d.buf, byte(n-5))
		return
	}
	var tmp []byte
	for i := 1; ; i++ {
		tmp = append(tmp, byte(n&0xff))
		n >>= 8
		if n == 0 {
			d.buf = append(d.buf, byte(i))
			d.buf = append(d.buf, tmp...)
			return
		}
		if n == -1 {
			d.buf = append(d.buf, byte(256-i))
			d.buf = append(d.buf, tmp...)
			return
		}
	}
}

// --- decoder ----------------------------------------------------------------

type mReader struct {
	vm     *VM
	buf    []byte
	pos    int
	syms   []string
	objs   []object.Value
	proc   *Proc
	freeze bool
}

// marshalLoad decodes a Marshal byte stream into a VM value.
func (vm *VM) marshalLoad(data []byte, proc *Proc, freeze bool) object.Value {
	r := &mReader{vm: vm, buf: data, proc: proc, freeze: freeze}
	major := r.byte()
	minor := r.byte()
	if int(major) != marshalMajor || int(minor) > marshalMinor {
		raise("TypeError", "incompatible marshal file format (can't be read)\n\tformat version %d.%d required; %d.%d given",
			marshalMajor, marshalMinor, major, minor)
	}
	return r.readValue()
}

// byte reads one byte, raising the MRI "marshal data too short" ArgumentError at
// end of input. Every read routes through here (or bytes), so a truncated stream
// is caught at a single point.
func (r *mReader) byte() byte {
	if r.pos >= len(r.buf) {
		raise("ArgumentError", "marshal data too short")
	}
	b := r.buf[r.pos]
	r.pos++
	return b
}

func (r *mReader) bytes(n int) []byte {
	if n < 0 || r.pos+n > len(r.buf) {
		raise("ArgumentError", "marshal data too short")
	}
	b := r.buf[r.pos : r.pos+n]
	r.pos += n
	return b
}

// long decodes Ruby's packed "long" encoding.
func (r *mReader) long() int {
	c := int(int8(r.byte()))
	if c == 0 {
		return 0
	}
	if c > 0 {
		if c > 4 {
			return c - 5
		}
		n := 0
		for i := 0; i < c; i++ {
			n |= int(r.byte()) << (8 * i)
		}
		return n
	}
	if c < -4 {
		return c + 5
	}
	n := -1
	for i := 0; i < -c; i++ {
		n &^= 0xff << (8 * i)
		n |= int(r.byte()) << (8 * i)
	}
	return n
}

// register appends v to the object-link table so a later '@' resolves to it,
// applying the deep-freeze flag; it is called as soon as v exists (before its
// children) so cyclic structures can link back to it.
func (r *mReader) register(v object.Value) object.Value {
	r.objs = append(r.objs, v)
	return v
}

// readValue reads one value and, unless it is a bare symbol or an object link,
// hands it to the load proc. MRI uses the proc's return value in place of the
// loaded object (so a proc can transform each object, and the top-level result
// is the proc's return for the outermost object); freezing, when requested, has
// already been applied to the object the proc receives.
func (r *mReader) readValue() object.Value {
	v, eligible := r.readObject()
	if eligible && r.proc != nil {
		return r.vm.callBlock(r.proc, []object.Value{v})
	}
	return v
}

// readObject reads one value. The second result reports whether the value is
// eligible to be passed to the load proc (true for everything but symbols and
// links).
func (r *mReader) readObject() (object.Value, bool) {
	tag := r.byte()
	switch tag {
	case '0':
		return object.NilV, true
	case 'T':
		return object.Bool(true), true
	case 'F':
		return object.Bool(false), true
	case 'i':
		return object.Integer(r.long()), true
	case 'l':
		return r.readBignum(), true
	case 'f':
		return r.freezeValue(r.readFloat()), true
	case ':', ';':
		r.pos--
		return object.Symbol(r.readSymbol()), false
	case '"':
		return r.freezeValue(r.register(object.NewStringBytesEnc(append([]byte(nil), r.bytes(r.long())...), "ASCII-8BIT"))), true
	case 'I':
		return r.freezeValue(r.readIvarWrapped()), true
	case '[':
		return r.freezeValue(r.readArray()), true
	case '{', '}':
		return r.freezeValue(r.readHash(tag == '}')), true
	case 'o':
		return r.freezeValue(r.readObj()), true
	case 'e':
		return r.freezeValue(r.readExtended()), true
	case 'C':
		return r.freezeValue(r.readSubclass()), true
	case 'S':
		return r.freezeValue(r.readStruct()), true
	case 'c':
		return r.readClassRef(false), true
	case 'm':
		return r.readClassRef(true), true
	case 'M':
		return r.readOldModule(), true
	case 'U':
		return r.freezeValue(r.readUserMarshal()), true
	case 'u':
		return r.freezeValue(r.readUserDef()), true
	case '/':
		return r.freezeValue(r.readRegexp(false)), true
	case '@':
		idx := r.long()
		if idx < 0 || idx >= len(r.objs) {
			raise("ArgumentError", "dump format error (unlinked)")
		}
		return r.objs[idx], false
	default:
		raise("ArgumentError", "dump format error(0x%x)", tag)
		return nil, false // unreachable
	}
}

func (r *mReader) readBignum() object.Value {
	sign := r.byte()
	n := r.long() // count in 16-bit shorts
	raw := r.bytes(n * 2)
	be := make([]byte, len(raw))
	for i, b := range raw {
		be[len(raw)-1-i] = b
	}
	z := new(big.Int).SetBytes(be)
	if sign == '-' {
		z.Neg(z)
	}
	return r.register(object.NormInt(z))
}

func (r *mReader) readFloat() object.Value {
	s := string(r.bytes(r.long()))
	// Older MRI dumps append a NUL followed by extra mantissa bytes for full
	// precision (e.g. "1.3\0\314\315"); the human-readable decimal before the
	// NUL already round-trips, so parse only that prefix.
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	var f float64
	switch s {
	case "inf":
		f = math.Inf(1)
	case "-inf":
		f = math.Inf(-1)
	case "nan":
		f = math.NaN()
	default:
		f, _ = strconv.ParseFloat(s, 64)
	}
	r.register(object.Float(f))
	return object.Float(f)
}

func (r *mReader) readArray() object.Value {
	n := r.long()
	a := object.NewArray()
	r.register(a)
	for i := 0; i < n; i++ {
		a.Elems = append(a.Elems, r.readValue())
	}
	return a
}

func (r *mReader) readHash(withDefault bool) object.Value {
	n := r.long()
	h := object.NewHash()
	r.register(h)
	for i := 0; i < n; i++ {
		k := r.readValue()
		v := r.readValue()
		h.Set(k, v)
	}
	if withDefault {
		h.Default = r.readValue()
	}
	return h
}

// readIvarWrapped reads the 'I' container: a base object followed by its ivars.
// String/Regexp encoding ivars (:E, :encoding) are folded into the encoding;
// every other ivar is set as a real instance variable (Time's cosmetic
// :zone / :offset are read and discarded).
func (r *mReader) readIvarWrapped() object.Value {
	base, _ := r.readObject()
	n := r.long()
	for i := 0; i < n; i++ {
		name := r.readSymbol()
		val := r.readValue()
		r.applyIvar(base, name, val)
	}
	return base
}

func (r *mReader) applyIvar(base object.Value, name string, val object.Value) {
	switch b := base.(type) {
	case *object.String:
		switch name {
		case "E":
			if val.Truthy() {
				b.Enc = "" // UTF-8
			} else {
				b.Enc = "US-ASCII"
			}
		case "encoding":
			if s, ok := val.(*object.String); ok {
				b.Enc = s.Str()
			}
		}
	case *Regexp:
		// :E only refines the source encoding; the compiled Regexp needs nothing.
	default:
		setIvar(base, name, val)
	}
}

func (r *mReader) readObj() object.Value {
	className := r.readSymbol()
	if className == "Range" {
		return r.readRange()
	}
	cls := r.vm.marshalClass(className)
	o := &RObject{class: cls, ivars: map[string]object.Value{}}
	r.register(o)
	isExc := r.vm.marshalIsException(cls)
	n := r.long()
	for i := 0; i < n; i++ {
		name := r.readSymbol()
		val := r.readValue()
		// MRI serialises an Exception's message and backtrace as the special
		// pseudo-ivars :mesg and :bt; rbgo keeps them in @message and
		// @__backtrace__. A nil value means "unset", so it is left off entirely.
		if isExc {
			switch name {
			case "mesg":
				if !object.IsNil(val) {
					setIvar(o, "@message", val)
				}
				continue
			case "bt":
				if !object.IsNil(val) {
					setIvar(o, "@__backtrace__", val)
				}
				continue
			}
		}
		setIvar(o, name, val)
	}
	return o
}

// marshalIsException reports whether cls is Exception or one of its subclasses,
// so the :mesg / :bt pseudo-ivars are mapped onto rbgo's message/backtrace.
func (vm *VM) marshalIsException(cls *RClass) bool {
	for _, a := range vm.ancestors(cls) {
		if a == vm.cException {
			return true
		}
	}
	return false
}

// readExtended reads the 'e' container: a module name followed by the object
// that was singleton-extended with it. The base is re-extended on load. 'e'
// carries no link id of its own — the wrapped object is what gets registered.
func (r *mReader) readExtended() object.Value {
	modName := r.readSymbol()
	mod := r.vm.marshalClass(modName)
	base, _ := r.readObject()
	r.vm.send(base, "extend", []object.Value{mod}, nil)
	return base
}

// readSubclass reads the 'C' container: a user subclass of a built-in value
// type, whose wrapped built-in value follows the class name. The outer object
// is registered before the payload so the link ids match the dump side.
func (r *mReader) readSubclass() object.Value {
	className := r.readSymbol()
	cls := r.vm.marshalClass(className)
	// 'C:Hash' wrapping a Hash payload is MRI's carrier for a compare_by_identity
	// Hash of the plain Hash class (not a user subclass): rebuild the Hash itself
	// and re-apply compare_by_identity, sharing the container's single object id.
	if cls == r.vm.cHash {
		inner := r.readValue()
		if ih, ok := inner.(*object.Hash); ok {
			ih.CompareByIdentity()
		}
		return inner
	}
	o := &RObject{class: cls, ivars: map[string]object.Value{}}
	r.register(o)
	o.builtin = r.readValue()
	return o
}

func (r *mReader) readRange() object.Value {
	rng := &object.Range{}
	r.register(rng)
	n := r.long()
	for i := 0; i < n; i++ {
		name := r.readSymbol()
		val := r.readValue()
		switch name {
		case "excl":
			rng.Exclusive = val.Truthy()
		case "begin":
			rng.Lo = val
		case "end":
			rng.Hi = val
		}
	}
	return rng
}

func (r *mReader) readStruct() object.Value {
	className := r.readSymbol()
	cls := r.vm.marshalClass(className)
	sd := structDefOf(cls)
	if sd == nil {
		raise("TypeError", "%s is not a Struct", className)
	}
	o := &RObject{class: cls, ivars: map[string]object.Value{}, structVals: make([]object.Value, len(sd.names))}
	r.register(o)
	n := r.long()
	for i := 0; i < n; i++ {
		name := r.readSymbol()
		val := r.readValue()
		for idx, m := range sd.names {
			if m == name {
				o.structVals[idx] = val
			}
		}
	}
	return o
}

// readClassRef reads a 'c' (class), 'm' (module), or — via readOldModule — an 'M'
// (old class-or-module) reference. 'c' insists the name resolve to a real Class
// and 'm' to a Module; 'M' accepts either, matching MRI's legacy reader.
func (r *mReader) readClassRef(module bool) object.Value {
	name := string(r.bytes(r.long()))
	cls := r.vm.marshalClass(name)
	if module && !cls.isModule {
		raise("ArgumentError", "%s does not refer to module", name)
	}
	if !module && cls.isModule {
		raise("ArgumentError", "%s does not refer to class", name)
	}
	return r.register(cls)
}

// readOldModule reads the legacy 'M' reference, which names either a class or a
// module without distinguishing them.
func (r *mReader) readOldModule() object.Value {
	name := string(r.bytes(r.long()))
	return r.register(r.vm.marshalClass(name))
}

func (r *mReader) readUserMarshal() object.Value {
	className := r.readSymbol()
	// The value carries the same object identity as its marshal_dump payload's
	// container, so it is registered before the payload is read (matching the
	// dump-side id order) so shared/cyclic references resolve.
	switch className {
	case "Complex":
		c := &object.Complex{}
		r.register(c)
		arr := r.mustArray(r.readValue(), 2)
		c.Re, c.Im = arr.Elems[0], arr.Elems[1]
		return c
	case "Rational":
		rat := &object.Rational{R: new(big.Rat)}
		r.register(rat)
		arr := r.mustArray(r.readValue(), 2)
		rat.R.SetFrac(marshalBigInt(arr.Elems[0]), marshalBigInt(arr.Elems[1]))
		return rat
	}
	cls := r.vm.marshalClass(className)
	o := &RObject{class: cls, ivars: map[string]object.Value{}}
	r.register(o)
	val := r.readValue()
	r.vm.send(o, "marshal_load", []object.Value{val}, nil)
	return o
}

func (r *mReader) readUserDef() object.Value {
	className := r.readSymbol()
	data := r.bytes(r.long())
	if className == "Time" {
		return r.register(marshalLoadTime(data))
	}
	cls := r.vm.marshalClass(className)
	str := object.NewStringBytesEnc(append([]byte(nil), data...), "ASCII-8BIT")
	return r.register(r.vm.send(cls, "_load", []object.Value{str}, nil))
}

func (r *mReader) readRegexp(_ bool) object.Value {
	source := string(r.bytes(r.long()))
	opt := int(r.byte())
	flags := ""
	if opt&reMultiline != 0 {
		flags += "m"
	}
	if opt&reIgnoreCase != 0 {
		flags += "i"
	}
	if opt&reExtended != 0 {
		flags += "x"
	}
	re := r.vm.compileRegexp(source, flags)
	return r.register(re)
}

// readSymbol reads a ':' new symbol (interning it), a ';' back-reference, or an
// 'I'-wrapped encoded symbol. rbgo symbols carry no per-symbol encoding, so the
// wrapper's encoding ivars (:E / :encoding) are read past and discarded; the
// symbol name (UTF-8 bytes) is returned unchanged.
func (r *mReader) readSymbol() string {
	tag := r.byte()
	switch tag {
	case ':':
		name := string(r.bytes(r.long()))
		r.syms = append(r.syms, name)
		return name
	case ';':
		idx := r.long()
		if idx < 0 || idx >= len(r.syms) {
			raise("ArgumentError", "dump format error (bad symbol)")
		}
		return r.syms[idx]
	case 'I':
		name := r.readSymbol()
		n := r.long()
		for i := 0; i < n; i++ {
			r.readSymbol()
			r.readValue()
		}
		return name
	default:
		raise("ArgumentError", "dump format error(0x%x)", tag)
		return "" // unreachable
	}
}

func (r *mReader) mustArray(v object.Value, n int) *object.Array {
	a, ok := v.(*object.Array)
	if !ok || len(a.Elems) != n {
		raise("ArgumentError", "marshal data too short")
	}
	return a
}

// freezeValue deep-freezes v when the freeze: kwarg is active, for the value
// kinds that carry a frozen flag.
func (r *mReader) freezeValue(v object.Value) object.Value {
	if !r.freeze {
		return v
	}
	switch x := v.(type) {
	case *object.String:
		x.Frozen = true
	case *RObject:
		x.frozen = true
	case *Regexp:
		x.frozen = true
	}
	return v
}

// marshalClass resolves a possibly "::"-qualified constant name to a class or
// module, raising MRI's ArgumentError when it is undefined.
func (vm *VM) marshalClass(name string) *RClass {
	segs := strings.Split(strings.TrimPrefix(name, "::"), "::")
	var cur object.Value
	for i, seg := range segs {
		if i == 0 {
			v, ok := vm.cObject.consts[seg]
			if !ok {
				raise("ArgumentError", "undefined class/module %s", name)
			}
			cur = v
			continue
		}
		cls, ok := cur.(*RClass)
		if !ok {
			raise("ArgumentError", "undefined class/module %s", name)
		}
		v, ok := vm.constInAncestors(cls, seg)
		if !ok {
			raise("ArgumentError", "undefined class/module %s", name)
		}
		cur = v
	}
	cls, ok := cur.(*RClass)
	if !ok {
		raise("ArgumentError", "undefined class/module %s", name)
	}
	return cls
}

// marshalLoadTime rebuilds a Time from the 8-byte packed time_mdump form.
func marshalLoadTime(data []byte) *Time {
	if len(data) < 8 {
		raise("ArgumentError", "marshal data too short")
	}
	p := encbinary.LittleEndian.Uint32(data[0:4])
	s := encbinary.LittleEndian.Uint32(data[4:8])
	utc := p&0x40000000 != 0
	p &= 0x3FFFFFFF
	year := int(p>>14) + 1900
	mon := int((p>>10)&0xf) + 1
	mday := int((p >> 5) & 0x1f)
	hour := int(p & 0x1f)
	min := int((s >> 26) & 0x3f)
	sec := int((s >> 20) & 0x3f)
	usec := int(s & 0xFFFFF)
	loc := stdtime.UTC
	_ = utc
	t := stdtime.Date(year, stdtime.Month(mon), mday, hour, min, sec, usec*1000, loc)
	return &Time{t: t}
}

// --- shared float / integer helpers -----------------------------------------

// marshalBigInt extracts the *big.Int backing an Integer or Bignum value.
func marshalBigInt(v object.Value) *big.Int {
	switch x := v.(type) {
	case object.Integer:
		return big.NewInt(int64(x))
	case *object.Bignum:
		return new(big.Int).Set(x.I)
	default:
		raise("TypeError", "no implicit conversion to Integer")
		return nil // unreachable
	}
}

// marshalIsASCII reports whether s is pure 7-bit ASCII.
func marshalIsASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// marshalLEAbs returns |b| as little-endian bytes, padded to an even length.
func marshalLEAbs(b *big.Int) []byte {
	be := new(big.Int).Abs(b).Bytes()
	le := make([]byte, len(be))
	for i, by := range be {
		le[len(be)-1-i] = by
	}
	if len(le)%2 == 1 {
		le = append(le, 0)
	}
	return le
}

// marshalFloatString formats f the way MRI's marshal does: the shortest decimal
// that round-trips, using exponent notation only when the decimal point falls
// before the fourth place to the left or past the last significant digit.
func marshalFloatString(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	}
	if f == 0 {
		if math.Signbit(f) {
			return "-0"
		}
		return "0"
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	ei := strings.IndexByte(s, 'e')
	mant := s[:ei]
	exp, _ := strconv.Atoi(s[ei+1:])
	var digits string
	if dot := strings.IndexByte(mant, '.'); dot >= 0 {
		digits = mant[:dot] + mant[dot+1:]
	} else {
		digits = mant
	}
	decpt := exp + 1
	digs := len(digits)

	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	switch {
	case decpt < -3 || decpt > digs:
		b.WriteByte(digits[0])
		if digs > 1 {
			b.WriteByte('.')
			b.WriteString(digits[1:])
		}
		b.WriteByte('e')
		b.WriteString(strconv.Itoa(decpt - 1))
	case decpt > 0:
		if decpt >= digs {
			b.WriteString(digits)
			b.WriteString(strings.Repeat("0", decpt-digs))
		} else {
			b.WriteString(digits[:decpt])
			b.WriteByte('.')
			b.WriteString(digits[decpt:])
		}
	default:
		b.WriteString("0.")
		b.WriteString(strings.Repeat("0", -decpt))
		b.WriteString(digits)
	}
	return b.String()
}
