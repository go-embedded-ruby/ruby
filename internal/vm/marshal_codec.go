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
	if d.linkRef(v) {
		return true
	}
	d.remember(v)
	return false
}

// linkRef emits the '@' back-reference when v is already in the objects table
// and reports whether it did. Unlike link it never allocates an id, so a caller
// that must register v at a later point (MRI's w_remember position) can.
func (d *mDumper) linkRef(v object.Value) bool {
	if id, ok := d.objs[v]; ok {
		d.buf = append(d.buf, '@')
		d.writeLong(id)
		return true
	}
	return false
}

// remember assigns v the next object-link id, as MRI's w_remember does. Where
// that call sits decides the id every later object gets: the #_dump ('u')
// container registers its object only after writing its instance variables, so
// those variables index BEFORE the object itself.
//
// There is deliberately no already-present guard: every caller reaches this only
// after linkRef reported v absent, and nothing registers v in between, so such a
// guard would be unreachable.
func (d *mDumper) remember(v object.Value) {
	d.objs[v] = d.nextID
	d.nextID++
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
		d.writeArray(x)
	case *object.Hash:
		if d.link(x) {
			return
		}
		d.writeHash(x)
	case *object.Range:
		if d.link(x) {
			return
		}
		d.writeExtended(x)
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
		// Time dumps through the 'u' (USERDEF) container, whose w_remember sits at
		// the END of that branch in MRI's w_object — so the Time takes a HIGHER
		// object-link id than its own instance variables and its :zone string.
		if d.linkRef(x) {
			return
		}
		d.writeTime(x)
		d.remember(x)
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

// marshalEncIvar is the encoding instance variable Marshal records for an
// encoding-capable value: either the short :E form (true for UTF-8, false for
// US-ASCII) or the long :encoding form naming the encoding.
type marshalEncIvar struct {
	sym  string // "E" (short form) or "encoding" (long form)
	tf   byte   // 'T' or 'F' for the short form; 0 when name is used
	name string // encoding name, for the long form only
}

// marshalEncodingIvar mirrors MRI's encoding_name (marshal.c): US-ASCII maps to
// the short :E => false, UTF-8 to the short :E => true, and any other encoding
// to :encoding => "<name>". ASCII-8BIT is encoding index 0, for which
// encoding_name returns Qnil, so a binary value carries no encoding ivar at all
// (and, having no other ivar, no 'I' wrapper either) — hence the nil result.
func marshalEncodingIvar(enc string) *marshalEncIvar {
	switch enc {
	case "ASCII-8BIT", "BINARY":
		return nil
	case "", "UTF-8":
		return &marshalEncIvar{sym: "E", tf: 'T'}
	case "US-ASCII":
		return &marshalEncIvar{sym: "E", tf: 'F'}
	}
	return &marshalEncIvar{sym: "encoding", name: enc}
}

// writeEncodingIvar emits one encoding ivar pair (name then value), as MRI's
// w_encoding does. It writes only the pair; the caller has already written the
// ivar count that covers it.
func (d *mDumper) writeEncodingIvar(e *marshalEncIvar) {
	d.writeSymbol(e.sym)
	if e.tf != 0 {
		d.buf = append(d.buf, e.tf)
		return
	}
	d.writeValue(object.NewStringBytesEnc([]byte(e.name), "ASCII-8BIT"))
}

// marshalIvars returns v's instance variables in first-assignment order and the
// table to read their values from, or nil when it has none to dump.
//
// It answers for the kinds with no ivar field of their own — String, Array,
// Hash, Regexp — whose variables live in the VM-wide generic table, MRI's
// generic_iv_tbl_. That table is what #674 added; before it, a write to one of
// those kinds was discarded in silence (#672), so Marshal had nothing to
// serialise and emitting nothing was indistinguishable from correct. It is
// correct no longer: MRI's has_ivars reaches every one of these kinds through
// its `default: generic:` label and counts what rb_ivar_foreach reports.
//
// The order pointer is non-nil for every kind routed here (genericIvars records
// the assignment order), so a nil one means there is nothing to report.
func marshalIvars(v object.Value) ([]string, map[string]object.Value) {
	st := ivarStoreOf(v, false)
	if len(st.tbl) == 0 || st.order == nil {
		return nil, nil
	}
	names := make([]string, 0, len(*st.order))
	for _, n := range *st.order {
		if _, live := st.tbl[n]; live {
			names = append(names, n)
		}
	}
	return names, st.tbl
}

// writeIvarWrapped emits one value that may carry instance variables, an
// encoding, or both, in MRI's w_object order: the 'I' (TYPE_IVAR) byte, then
// w_uclass's 'e'/'C' prefixes, then the payload, then ONE ivar list covering
// both the encoding ivar and the object's own variables.
//
// TYPE_IVAR WRAPS the object; it is not a field of it. The count and the pairs
// come after the payload, and the encoding ivar comes first within them
// (w_ivar subtracts what w_encoding wrote from the count it already emitted).
// The 'I' byte takes no object-link id of its own: on the dump side w_remember
// runs before it, and on the load side r_object0 registers the wrapped object,
// so the wrapper is transparent to every back-reference.
func (d *mDumper) writeIvarWrapped(v object.Value, enc *marshalEncIvar, payload func()) {
	names, tbl := marshalIvars(v)
	n := len(names)
	if enc != nil {
		n++
	}
	if n > 0 {
		d.buf = append(d.buf, 'I')
	}
	d.writeExtended(v)
	payload()
	if n > 0 {
		d.writeLong(n)
		if enc != nil {
			d.writeEncodingIvar(enc)
		}
		d.writeIvarPairs(names, tbl)
	}
}

// writeExtended emits the 'e' (TYPE_EXTENDED) prefix naming each module
// singleton-extended into v, mirroring MRI's w_extended (marshal.c) called with
// check=TRUE: the singleton class is stepped over and every module between it
// and the real class is named, most-recently-extended first. A singleton that
// carries methods or instance variables of its own is not reconstructible from
// the stream, so MRI refuses it (SINGLETON_DUMP_UNABLE_P).
//
// MRI passes check=FALSE for the #marshal_dump ('U') and #_dump ('u')
// containers, where w_extended then emits nothing at all — those callers must
// not call this.
func (d *mDumper) writeExtended(v object.Value) {
	sc := d.vm.objSingleton(v)
	if sc == nil {
		return
	}
	if len(sc.methods) > 0 || len(sc.ivars) > 0 {
		raise("TypeError", "singleton can't be dumped")
	}
	inc := sc.includes
	for i := len(inc) - 1; i >= 0; i-- {
		if inc[i].name == "" {
			raise("TypeError", "can't dump anonymous class %s", inc[i].ToS())
		}
		d.buf = append(d.buf, 'e')
		d.writeSymbol(inc[i].name)
	}
}

func (d *mDumper) writeString(s *object.String) {
	if d.link(s) {
		return
	}
	// A binary string with no instance variables has no encoding ivar either, so
	// it needs no wrapper at all; one with either takes the 'I' container.
	d.writeIvarWrapped(s, marshalEncodingIvar(s.EncName()), func() { d.writeStringPayload(s) })
}

// writeArray emits an Array: the 'I' wrapper when it carries instance
// variables (an Array is not encoding-capable, so it never has an encoding
// ivar), then the 'e' prefixes, then the elements, then the ivar list.
func (d *mDumper) writeArray(a *object.Array) {
	d.writeIvarWrapped(a, nil, func() { d.writeArrayPayload(a) })
}

// writeArrayPayload emits just the TYPE_ARRAY tag and elements — the Array
// counterpart of writeStringPayload, so a 'C' container can own the wrapper.
func (d *mDumper) writeArrayPayload(a *object.Array) {
	d.buf = append(d.buf, '[')
	d.writeLong(len(a.Elems))
	for _, e := range a.Elems {
		d.writeValue(e)
	}
}

// writeStringPayload emits just the TYPE_STRING tag and bytes, with no 'I'
// wrapper, extend prefix or ivar list. The caller owns those, so a built-in
// subclass's 'C' container can fold the payload's encoding ivar into its own.
func (d *mDumper) writeStringPayload(s *object.String) {
	d.buf = append(d.buf, '"')
	d.writeBytes(s.Str())
}

func (d *mDumper) writeHash(h *object.Hash) {
	if !object.IsNil(h.DefaultProc) {
		raise("TypeError", "can't dump hash with default proc")
	}
	// A Hash is not encoding-capable, so the wrapper is the ivar list alone. It
	// opens BEFORE the compare_by_identity 'C' container below, because MRI
	// writes TYPE_IVAR before w_uclass and before that container.
	d.writeIvarWrapped(h, nil, func() { d.writeHashPayload(h) })
}

// writeHashPayload emits the Hash body: the compare_by_identity 'C' container
// when it applies, then the entries and the default value.
func (d *mDumper) writeHashPayload(h *object.Hash) {
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

// encodingASCIICompatible reports whether an encoding name denotes an
// ASCII-compatible encoding — one whose ASCII bytes stand for ASCII characters.
// The wide Unicode transformation formats (UTF-16/UTF-32, in any endianness) are
// the exceptions Marshal cares about: a Regexp over such a source keeps that
// encoding even when every byte happens to be ASCII.
func encodingASCIICompatible(name string) bool {
	u := strings.ToUpper(name)
	return !strings.HasPrefix(u, "UTF-16") && !strings.HasPrefix(u, "UTF-32")
}

// regexpMarshalFixed reports whether the Regexp is dumped with the
// ARG_ENCODING_FIXED (0x10) option bit set. MRI sets it when FIXEDENCODING was
// requested or the source is otherwise tied to a concrete encoding: a byte
// outside ASCII, or an ASCII-incompatible source encoding (UTF-16/UTF-32) even
// with ASCII-only bytes. A NOENCODING (/n) Regexp is not fixed.
func regexpMarshalFixed(r *Regexp) bool {
	if r.fixedEnc {
		return true
	}
	if r.noEnc {
		return false
	}
	if r.srcEnc != "" && !encodingASCIICompatible(r.srcEnc) {
		return true
	}
	return !marshalIsASCII(r.source)
}

// regexpMarshalEnc returns the encoding name Marshal records for the Regexp,
// which is the encoding of the compiled Regexp (#encoding). When the Regexp is
// fixed to a concrete source encoding, that encoding is recorded even for an
// ASCII-only source (so a Windows-1251 or UTF-16 Regexp keeps its encoding);
// otherwise #encoding's own rule applies (US-ASCII for an ASCII-only source, the
// source encoding for a binary or wide one). An "ASCII-8BIT" result is dumped
// bare, with no encoding ivar.
func regexpMarshalEnc(r *Regexp) string {
	if regexpMarshalFixed(r) && r.srcEnc != "" {
		return r.srcEnc
	}
	return r.encodingName()
}

func (d *mDumper) writeRegexp(r *Regexp) {
	opt := regexpMarshalOpts(r)
	// As for a String: the 'I' wrapper, then w_uclass's 'e'/'C' prefixes, then the
	// payload, then the ivar list. A binary (ASCII-8BIT) Regexp carries no
	// encoding ivar, so one with no variables of its own is emitted bare.
	d.writeIvarWrapped(r, marshalEncodingIvar(regexpMarshalEnc(r)), func() {
		d.writeRegexpPayload(r, opt)
	})
}

// writeRegexpPayload emits just the TYPE_REGEXP tag, source and option byte —
// the Regexp counterpart of writeStringPayload.
func (d *mDumper) writeRegexpPayload(r *Regexp, opt int) {
	d.buf = append(d.buf, '/')
	d.writeBytes(r.source)
	d.buf = append(d.buf, byte(opt))
}

// regexpMarshalOpts returns the Regexp option byte Marshal records: the
// inline flags plus the ARG_ENCODING_FIXED / ARG_ENCODING_NONE bits.
func regexpMarshalOpts(r *Regexp) int {
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
	if regexpMarshalFixed(r) {
		opt |= reFixedEncoding
	}
	if r.noEnc {
		opt |= reNoEncoding
	}
	return opt
}

// marshalPayloadEnc returns the encoding ivar of a built-in payload value, or
// nil when that kind of value carries none.
func marshalPayloadEnc(v object.Value) *marshalEncIvar {
	switch x := v.(type) {
	case *object.String:
		return marshalEncodingIvar(x.EncName())
	case *Regexp:
		return marshalEncodingIvar(regexpMarshalEnc(x))
	}
	return nil
}

// writeBuiltinPayload emits the bare payload of a built-in subclass's 'C'
// container. String and Regexp take the payload writers, so their encoding ivar
// folds into the container's single ivar list instead of opening a nested 'I'
// wrapper; every other built-in has no encoding ivar and writes normally. The
// payload is the same Ruby object as its wrapper, so it must not take an
// object-link id of its own.
func (d *mDumper) writeBuiltinPayload(v object.Value) {
	switch x := v.(type) {
	case *object.String:
		d.writeStringPayload(x)
	case *Regexp:
		d.writeRegexpPayload(x, regexpMarshalOpts(x))
	default:
		d.writeValue(v)
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

// writeTime emits Time's 'u' (USERDEF) container. MRI's time_mdump builds the
// payload String, copies the Time's own instance variables onto it
// (rb_copy_generic_ivar), then appends :offset — only when the Time is not UTC —
// and :zone, which is written ALWAYS, nil included. Marshal then writes that
// String's variables as the container's, so the order here is the order there.
func (d *mDumper) writeTime(t *Time) {
	utc := t.t.Location() == stdtime.UTC
	names := marshalLiveIvars(&t.methodValueState)
	n := len(names) + 1 // :zone is always written
	if !utc {
		n++ // :offset
	}
	d.buf = append(d.buf, 'I', 'u')
	d.writeSymbol("Time")
	d.writeBytes(string(timeDumpBytes(t)))
	d.writeLong(n)
	d.writeIvarPairs(names, t.ivars)
	if !utc {
		_, off := t.t.Zone()
		d.writeSymbol("offset")
		d.writeValue(object.Integer(off))
	}
	d.writeSymbol("zone")
	d.writeValue(d.timeZoneIvar(t))
}

// timeZoneIvar returns the value of a Time's :zone marshal variable: the zone's
// name, or nil for a Time on a bare numeric offset, which has none. The two
// sources differ in encoding, and time_mdump treats them differently — a
// Timezone object's #name is stored exactly as that method returned it, while a
// zone the Time carries as a location contributes the tz database's
// abbreviation, which MRI builds US-ASCII.
func (d *mDumper) timeZoneIvar(t *Time) object.Value {
	if z := t.zoneObj; z != nil {
		// A Timezone object contributes its #name (maybe_tzobj_p in time_mdump),
		// and MRI stores the String that method returned AS IS — so its encoding is
		// whatever Ruby gave it. Re-tagging an ASCII-only name US-ASCII here made a
		// tzobj Time dump ":\tzoneI\"\bXYZ\x06:\x06EF" where MRI writes ":\x06ET".
		n := d.vm.send(z, "name", nil, nil)
		if s, ok := n.(*object.String); ok {
			return s
		}
		return object.NilV
	}
	// A zone the Time carries only as a location: the abbreviation Go reports,
	// which is the tz database's and always ASCII. MRI builds these US-ASCII, so
	// "UTC" / "AST" / "CEST" dump with :E => false. A bare numeric offset has no
	// name and dumps nil.
	name, _ := t.t.Zone()
	if name == "" {
		return object.NilV
	}
	return object.NewStringBytesEnc([]byte(name), "US-ASCII")
}

// writeObject dispatches an ordinary instance: the #marshal_dump hook (U), the
// class _dump hook (u), a Struct (S), or the generic ivar object (o).
func (d *mDumper) writeObject(o *RObject) {
	if d.linkRef(o) {
		return
	}
	// Every path below names o.class; an instance of an anonymous class cannot be
	// dumped, whatever hook (marshal_dump / _dump / Struct / builtin subclass /
	// plain object) it would otherwise take.
	if o.class.name == "" {
		raise("TypeError", "can't dump anonymous class %s", o.class.ToS())
	}
	// The #marshal_dump and #_dump containers are the two MRI writes with
	// w_class(..., check=FALSE): w_extended then emits nothing, so a module
	// extended into the object is NOT recorded and a singleton with its own
	// methods is not refused. Neither calls writeExtended for that reason.
	if d.vm.respondsTo(o, "marshal_dump") {
		// USRMARSHAL registers the object BEFORE calling the hook, so a reference
		// back to it from inside the dumped value links rather than recursing.
		d.remember(o)
		val := d.vm.send(o, "marshal_dump", nil, nil)
		d.emitUserMarshal(o.class.name, val)
		return
	}
	if d.vm.respondsTo(o, "_dump") {
		d.writeUserDef(o)
		return
	}
	// From here on MRI passes check=TRUE, so each branch calls writeExtended:
	// extended modules are recorded and a singleton carrying its own methods or
	// ivars is refused. The call sits inside the branches because the 'I' ivar
	// wrapper, which only some of them take, is written before the 'e' prefixes.
	d.remember(o)
	if sd := structDefOf(o.class); sd != nil {
		d.writeStructLike(o, sd.names)
		return
	}
	// A Data (Ruby 3.2+ immutable value object, minted by Data.define) marshals
	// with the same 'S' container as a Struct — the class name followed by each
	// member name/value pair — because MRI stores a Data's members the same way it
	// stores a Struct's. The real class name is used (never a #name override).
	if dd := dataDefOf(o.class); dd != nil {
		d.writeStructLike(o, dd.names)
		return
	}
	// An instance of a user subclass of a built-in value type (Array/Hash/
	// String/Range/...) dumps as 'C' — the class name followed by the wrapped
	// built-in value. Any instance variables are carried by an outer 'I' wrapper,
	// as MRI does for objects whose payload type has no inline ivar slot.
	if o.builtin != nil {
		// The payload is the same Ruby object as its wrapper, so its encoding ivar
		// belongs to the container's ONE ivar list — not to a nested 'I' wrapper of
		// its own — and, per MRI's w_ivar, it is written before the object's own
		// variables. The 'e' extend prefix was already emitted above, and the 'I'
		// comes before it (w_object writes TYPE_IVAR, then w_uclass).
		names := o.liveIvarNames()
		enc := marshalPayloadEnc(o.builtin)
		n := len(names)
		if enc != nil {
			n++
		}
		if n > 0 {
			d.buf = append(d.buf, 'I')
		}
		d.writeExtended(o)
		d.buf = append(d.buf, 'C')
		d.writeSymbol(o.class.name)
		d.writeBuiltinPayload(o.builtin)
		if n > 0 {
			d.writeLong(n)
			if enc != nil {
				d.writeEncodingIvar(enc)
			}
			d.writeIvarPairs(names, o.ivars)
		}
		return
	}
	// There is deliberately no 'd' (TYPE_DATA) dump branch here. MRI reaches it
	// from `case T_DATA`, not from responding to #_dump_data: a pure-Ruby class
	// that defines #_dump_data is still a T_OBJECT and dumps as 'o'. Keying the
	// branch on the method instead turned `Marshal.dump(W.new)` into "\x04\bd:\x06W…"
	// where MRI writes "\x04\bo:\x06W\x00". rbgo has no user-reachable T_DATA, so
	// the 'd' container is load-only (readUserData), for streams MRI wrote.
	//
	// A plain object is MRI's T_OBJECT: its instance variables live in the 'o'
	// body itself (has_ivars counts them "elsewhere"), so there is no 'I' wrapper.
	d.writeExtended(o)
	if d.vm.marshalIsException(o.class) {
		d.writeException(o)
		return
	}
	d.buf = append(d.buf, 'o')
	d.writeSymbol(o.class.name)
	names := o.liveIvarNames()
	d.writeLong(len(names))
	d.writeIvarPairs(names, o.ivars)
}

// writeIvarPairs writes each named instance variable as a symbol/value pair.
// The count belongs to the caller, which may be covering an encoding ivar too.
func (d *mDumper) writeIvarPairs(names []string, ivars map[string]object.Value) {
	for _, name := range names {
		d.writeSymbol(name)
		d.writeValue(ivars[name])
	}
}

// marshalLiveIvars returns a boxed value's instance-variable names in
// first-assignment order, dropping any since removed — the methodValueState
// counterpart of RObject.liveIvarNames.
func marshalLiveIvars(s *methodValueState) []string {
	out := make([]string, 0, len(s.ivarOrder))
	for _, n := range s.ivarOrder {
		if _, live := s.ivars[n]; live {
			out = append(out, n)
		}
	}
	return out
}

// writeStructLike emits the 'S' container shared by Struct and Data: the class
// name and each member name/value pair. Unlike a plain object, MRI treats a
// Struct as a payload type whose instance variables cannot live in the body, so
// any it carries go in an outer 'I' wrapper (has_ivars's generic branch in
// marshal.c reaches T_STRUCT, while T_OBJECT is "counted elsewhere").
func (d *mDumper) writeStructLike(o *RObject, members []string) {
	names := o.liveIvarNames()
	if len(names) > 0 {
		d.buf = append(d.buf, 'I')
	}
	d.writeExtended(o)
	d.buf = append(d.buf, 'S')
	d.writeSymbol(o.class.name)
	d.writeLong(len(members))
	for i, m := range members {
		d.writeSymbol(m)
		d.writeValue(o.structVals[i])
	}
	if len(names) > 0 {
		d.writeLong(len(names))
		d.writeIvarPairs(names, o.ivars)
	}
}

// writeUserDef emits the #_dump ('u', TYPE_USERDEF) container. MRI (w_object in
// marshal.c) computes the ivar set from the object AND from the String #_dump
// returned, and the String's set WINS when it is non-empty — so a _dump payload
// in a non-UTF-8 encoding carries its :encoding ivar here even though the object
// itself has none. The object is registered in the objects table only AFTER its
// instance variables are written, so those variables take the lower link ids.
func (d *mDumper) writeUserDef(o *RObject) {
	s := d.vm.send(o, "_dump", []object.Value{object.Integer(-1)}, nil)
	str, ok := s.(*object.String)
	if !ok {
		raise("TypeError", "_dump() must return String")
	}
	// The ivar set comes from the payload String alone. has_ivars is called on the
	// object first, but an ordinary instance is T_OBJECT, which that function
	// skips as "counted elsewhere" — and for a USERDEF container there is no
	// elsewhere, so the object's own variables are never written. Only the
	// String's contribute: its encoding, and any variable #_dump set on it.
	//
	// writeIvarWrapped is not used here: this container writes no 'e' prefix
	// (MRI calls w_class with check=FALSE, so w_extended emits nothing), and the
	// object must be remembered AFTER its variables, which is the reverse of
	// every other container.
	enc := marshalEncodingIvar(str.EncName())
	names, tbl := marshalIvars(str)
	n := len(names)
	if enc != nil {
		n++
	}
	if n > 0 {
		d.buf = append(d.buf, 'I')
	}
	d.buf = append(d.buf, 'u')
	d.writeSymbol(o.class.name)
	d.writeBytes(str.Str())
	if n > 0 {
		d.writeLong(n)
		if enc != nil {
			d.writeEncodingIvar(enc)
		}
		d.writeIvarPairs(names, tbl)
	}
	d.remember(o)
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
		return r.freezeValue(r.readUserDef(false)), true
	case 'd':
		return r.freezeValue(r.readUserData()), true
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
// every other ivar is set as a real instance variable, and Time's :zone /
// :offset become its zone.
func (r *mReader) readIvarWrapped() object.Value {
	// MRI's r_object0 passes an `ivp` flag down into the TYPE_USERDEF branch, so
	// that branch reads its OWN instance variables and only then calls r_entry.
	// The object therefore takes a HIGHER object-link id than its variables —
	// mirroring where w_remember sits on the dump side. Every other container
	// registers itself first, so only 'u' needs the hand-over.
	if r.pos < len(r.buf) && r.buf[r.pos] == 'u' {
		r.pos++
		return r.readUserDef(true)
	}
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
		// Only :E and :encoding are the encoding; MRI's r_ivar sends everything
		// sym2encidx does not recognise to rb_ivar_set. Without the default below
		// each of those was dropped on the floor, so a String round-tripped
		// through rbgo lost every variable MRI had written for it.
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
		default:
			setIvar(base, name, val)
		}
	case *Regexp:
		// The encoding ivar records the Regexp's source encoding, which #encoding
		// reports (and a re-dump reproduces); the compiled matcher is unaffected.
		switch name {
		case "E":
			if val.Truthy() {
				b.srcEnc = "UTF-8"
			} else {
				b.srcEnc = "US-ASCII"
			}
		case "encoding":
			if s, ok := val.(*object.String); ok {
				b.srcEnc = s.Str()
			}
		default:
			setIvar(base, name, val)
		}
	case *RObject:
		// A built-in subclass loaded from a 'C' container is ONE Ruby object: the
		// wrapper and the payload it holds. Its encoding ivar therefore describes
		// the payload, not the wrapper. Storing :E / :encoding as a real instance
		// variable made a re-dump emit the encoding twice — once from the payload
		// and once as a stray ivar literally named "E".
		if b.builtin != nil && (name == "E" || name == "encoding") {
			r.applyIvar(b.builtin, name, val)
			return
		}
		setIvar(base, name, val)
	case *Time:
		// MRI's time_mload deletes :offset and :zone from the payload String
		// (rb_attr_delete) and turns them into the Time's zone, copying only what is
		// left back as real instance variables. The packed payload always holds the
		// UTC wall clock (time_mdump calls gmtimew), so re-displaying the same
		// instant in the recorded zone is what restores #utc_offset and #zone.
		switch name {
		// :offset always precedes :zone in the stream, so at this point the Time
		// carries no zone name yet and the offset alone is set — which is exactly
		// MRI's time_fixoff, after which a Time on a bare numeric offset reports
		// #zone as nil.
		case "offset":
			if off, ok := val.(object.Integer); ok {
				b.setFixedZone("", int(off))
			}
		case "zone":
			if s, ok := val.(*object.String); ok {
				_, off := b.t.Zone()
				b.setFixedZone(s.Str(), off)
			}
		default:
			setIvar(base, name, val)
		}
	default:
		setIvar(base, name, val)
	}
}

// marshalNonObjectBases names the core classes whose instances are not MRI
// T_OBJECTs. MRI's TYPE_OBJECT branch allocates an instance (obj_alloc_by_klass)
// and raises "dump format error" when what comes back is not a T_OBJECT, which
// is what stops an 'o' stream naming File, IO or Array from building one. rbgo
// has no allocation that can report a built-in type, so the class itself is
// asked instead — by ancestry, so a subclass is caught too. A user class of the
// same short name is not: a namespaced one is "My::File", and a top-level
// redefinition of File IS the class this rejects.
var marshalNonObjectBases = map[string]bool{
	"String": true, "Array": true, "Hash": true, "Regexp": true, "Time": true,
	"IO": true, "File": true, "Dir": true, "Struct": true, "Proc": true,
	"Method": true, "UnboundMethod": true, "Symbol": true, "Integer": true,
	"Float": true, "NilClass": true, "TrueClass": true, "FalseClass": true,
	"Module": true, "Class": true, "MatchData": true, "Thread": true,
	"Mutex": true, "Binding": true,
}

// marshalPlainObjectClass reports whether an 'o' container may rebuild an
// instance of cls — that is, whether its instances are plain objects.
func (vm *VM) marshalPlainObjectClass(cls *RClass) bool {
	for _, a := range vm.ancestors(cls) {
		if marshalNonObjectBases[a.name] {
			return false
		}
	}
	return true
}

func (r *mReader) readObj() object.Value {
	className := r.readSymbol()
	if className == "Range" {
		return r.readRange()
	}
	cls := r.vm.marshalClass(className)
	if !r.vm.marshalPlainObjectClass(cls) {
		raise("ArgumentError", "dump format error")
	}
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
	// The 'S' container carries both Struct and Data (Ruby 3.2+ immutable value)
	// instances, whose members are stored the same way; memberDefNames resolves
	// either layout. A Data loads as a frozen instance, matching MRI's immutability
	// (independent of the freeze: kwarg).
	names, ok := memberDefNames(cls)
	if !ok {
		raise("TypeError", "%s is not a Struct", className)
	}
	isData := dataDefOf(cls) != nil
	o := &RObject{class: cls, ivars: map[string]object.Value{}, structVals: make([]object.Value, len(names))}
	r.register(o)
	n := r.long()
	for i := 0; i < n; i++ {
		name := r.readSymbol()
		val := r.readValue()
		for idx, m := range names {
			if m == name {
				o.structVals[idx] = val
			}
		}
	}
	if isData {
		o.frozen = true
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

// readUserDef reads the 'u' (TYPE_USERDEF) container. hasIvars says the caller
// was an 'I' wrapper that handed its ivar list over: MRI reads those variables
// here, before r_entry, so the object takes a higher link id than they do.
//
// MRI applies them to the PAYLOAD STRING and then calls _load with it, which is
// how :E / :encoding decide the encoding _load sees. Time is the exception in
// rbgo: it is rebuilt directly rather than through Time._load, so the variables
// time_mload would have read off that String are applied to the Time itself.
func (r *mReader) readUserDef(hasIvars bool) object.Value {
	className := r.readSymbol()
	data := r.bytes(r.long())
	type ivar struct {
		name string
		val  object.Value
	}
	var ivars []ivar
	if hasIvars {
		n := r.long()
		for i := 0; i < n; i++ {
			name := r.readSymbol()
			ivars = append(ivars, ivar{name, r.readValue()})
		}
	}
	if className == "Time" {
		t := marshalLoadTime(data)
		for _, iv := range ivars {
			r.applyIvar(t, iv.name, iv.val)
		}
		return r.register(t)
	}
	str := object.NewStringBytesEnc(append([]byte(nil), data...), "ASCII-8BIT")
	for _, iv := range ivars {
		r.applyIvar(str, iv.name, iv.val)
	}
	cls := r.vm.marshalClass(className)
	return r.register(r.vm.send(cls, "_load", []object.Value{str}, nil))
}

// readUserData reads the 'd' (TYPE_DATA) container that carries an object
// wrapping a C pointer: a class name, then the value its #_load_data rebuilds
// the object from. Following MRI's r_object0, the instance is registered in the
// objects table BEFORE the payload is read, so a reference back to it links; the
// #_load_data check comes before the payload too, so a class missing that method
// raises TypeError without consuming it.
func (r *mReader) readUserData() object.Value {
	name := r.readSymbol()
	cls := r.vm.marshalClass(name)
	o := &RObject{class: cls, ivars: map[string]object.Value{}}
	// MRI separates the two failures by the ALLOCATED object's type: not a T_DATA
	// is "dump format error" (ArgumentError), a T_DATA without #_load_data is a
	// TypeError. rbgo has no T_DATA to test, so the discriminator here is whether
	// the class takes part in the _dump_data / _load_data protocol at all — which
	// is the property that makes a class 'd'-dumpable in the first place. A class
	// that does neither is a regular object and gets the ArgumentError.
	if !r.vm.respondsTo(o, "_dump_data") && !r.vm.respondsTo(o, "_load_data") {
		raise("ArgumentError", "dump format error")
	}
	r.register(o)
	if !r.vm.respondsTo(o, "_load_data") {
		raise("TypeError", "class %s needs to have instance method '_load_data'", name)
	}
	r.vm.send(o, "_load_data", []object.Value{r.readValue()}, nil)
	return o
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
	if rx, ok := re.(*Regexp); ok {
		// A non-ASCII source is already treated as fixed-encoding by its content,
		// exactly as a source literal is, so recording the FIXED bit for it would
		// make a loaded /café/ unequal to the literal /café/ (whose fixedEnc field
		// is false). Only an ASCII-only source needs the bit carried explicitly.
		rx.fixedEnc = opt&reFixedEncoding != 0 && marshalIsASCII(source)
		rx.noEnc = opt&reNoEncoding != 0
	}
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
