// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	binpkg "encoding/binary"
	"math"
	"math/big"
	"os"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// ioBuffer backs IO::Buffer — a fixed-capacity region of bytes with typed and
// bytewise accessors. rbgo models the in-memory forms (an internal buffer from
// IO::Buffer.new, and an external one from .for/.string); the memory-mapped and
// slice/transfer forms are not modelled, so mapped?/shared?/private?/locked? are
// always false. A freed buffer keeps its identity but reports null? and a size of
// zero.
type ioBuffer struct {
	data     []byte
	readonly bool
	external bool // memory owned elsewhere (.for copies a String, .string yields)
	borrowed bool // a slice sharing another buffer's memory — neither external nor internal
	freed    bool
	locked   bool      // inside a #locked block (blocks resize/transfer, reentrant lock)
	parent   *ioBuffer // for a slice, the buffer it was sliced from (nil for a non-slice)
	sliceOff int       // this slice's offset within parent (only meaningful when parent != nil)
	sliceLen int       // this slice's length within parent (only meaningful when parent != nil)

	// mapped, shared and priv are RB_IO_BUFFER_MAPPED / _SHARED / _PRIVATE
	// (include/ruby/io/buffer.h). IO::Buffer.new takes a MAPPED buffer for
	// anything from a page upwards, and IO::Buffer.map takes one over a file —
	// shared with it by default, or a private copy that the file never sees.
	mapped, shared, priv bool

	// null is a buffer with no memory at all, which is what a zero size gives:
	// io_buffer_initialize leaves base NULL, and #null? is true while #free has
	// never been called. It is distinct from freed, which is a buffer that HAD
	// memory.
	null bool
}

// baseNull is io_buffer.c's `buffer->base == NULL`, which #null? reports and
// #resize branches on: a buffer that never had memory (a zero-size
// IO::Buffer.new) or has given it up (#free, #resize(0), #transfer).
//
// A slice takes its base ONCE, when it is cut — `parent->base + offset`, which
// is NULL only if the parent's base was NULL then. Freeing the parent
// afterwards does NOT make the slice null; it makes it DANGLE, which is what
// #valid? reports and what free_spec pins:
//
//	buffer.free
//	slice.null?.should == false
//	slice.valid?.should == false
func (b *ioBuffer) baseNull() bool { return b.freed || b.null }

// sliceValid reports IO::Buffer#valid?. A non-slice is always valid (freeing it
// does not invalidate it — io_buffer.c io_buffer_valid_p). A slice becomes
// invalid when its source has been freed or reallocated so the slice no longer
// falls inside it; a slice of a String-backed (external) buffer stays valid even
// after the buffer is freed because the String keeps the memory alive.
func (b *ioBuffer) sliceValid() bool {
	if b.parent == nil {
		return true
	}
	p := b.parent
	if p.external {
		return true
	}
	if p.freed {
		return false
	}
	return b.sliceOff+b.sliceLen <= len(p.data)
}

func (b *ioBuffer) ToS() string {
	if b.freed {
		return "#<IO::Buffer 0x0000000000000000+0 NULL>"
	}
	return "#<IO::Buffer>"
}
func (b *ioBuffer) Inspect() string { return b.ToS() }
func (b *ioBuffer) Truthy() bool    { return true }

// bufType is one IO::Buffer data type (:U8/:s16/:F64/…): its width in bytes and
// the encode/decode between the raw bytes and a Ruby Integer/Float.
type bufType struct {
	size int
	get  func(b []byte) object.Value
	put  func(b []byte, v object.Value)
}

// bufTypes maps each IO::Buffer type symbol to its accessor. Upper-case names are
// big-endian, lower-case little-endian; U/S are unsigned/signed integers and
// f/F floats.
var bufTypes = map[string]bufType{
	"U8": {1, func(b []byte) object.Value { return object.IntValue(int64(b[0])) }, func(b []byte, v object.Value) { b[0] = byte(bufInt(v)) }},
	"S8": {1, func(b []byte) object.Value { return object.IntValue(int64(int8(b[0]))) }, func(b []byte, v object.Value) { b[0] = byte(bufInt(v)) }},

	"U16": {2, func(b []byte) object.Value { return object.IntValue(int64(binpkg.BigEndian.Uint16(b))) }, func(b []byte, v object.Value) { binpkg.BigEndian.PutUint16(b, uint16(bufInt(v))) }},
	"u16": {2, func(b []byte) object.Value { return object.IntValue(int64(binpkg.LittleEndian.Uint16(b))) }, func(b []byte, v object.Value) { binpkg.LittleEndian.PutUint16(b, uint16(bufInt(v))) }},
	"S16": {2, func(b []byte) object.Value { return object.IntValue(int64(int16(binpkg.BigEndian.Uint16(b)))) }, func(b []byte, v object.Value) { binpkg.BigEndian.PutUint16(b, uint16(bufInt(v))) }},
	"s16": {2, func(b []byte) object.Value { return object.IntValue(int64(int16(binpkg.LittleEndian.Uint16(b)))) }, func(b []byte, v object.Value) { binpkg.LittleEndian.PutUint16(b, uint16(bufInt(v))) }},

	"U32": {4, func(b []byte) object.Value { return object.IntValue(int64(binpkg.BigEndian.Uint32(b))) }, func(b []byte, v object.Value) { binpkg.BigEndian.PutUint32(b, uint32(bufInt(v))) }},
	"u32": {4, func(b []byte) object.Value { return object.IntValue(int64(binpkg.LittleEndian.Uint32(b))) }, func(b []byte, v object.Value) { binpkg.LittleEndian.PutUint32(b, uint32(bufInt(v))) }},
	"S32": {4, func(b []byte) object.Value { return object.IntValue(int64(int32(binpkg.BigEndian.Uint32(b)))) }, func(b []byte, v object.Value) { binpkg.BigEndian.PutUint32(b, uint32(bufInt(v))) }},
	"s32": {4, func(b []byte) object.Value { return object.IntValue(int64(int32(binpkg.LittleEndian.Uint32(b)))) }, func(b []byte, v object.Value) { binpkg.LittleEndian.PutUint32(b, uint32(bufInt(v))) }},

	"U64": {8, func(b []byte) object.Value { return object.NormInt(new(big.Int).SetUint64(binpkg.BigEndian.Uint64(b))) }, func(b []byte, v object.Value) { binpkg.BigEndian.PutUint64(b, bufUint(v)) }},
	"u64": {8, func(b []byte) object.Value {
		return object.NormInt(new(big.Int).SetUint64(binpkg.LittleEndian.Uint64(b)))
	}, func(b []byte, v object.Value) { binpkg.LittleEndian.PutUint64(b, bufUint(v)) }},
	"S64": {8, func(b []byte) object.Value { return object.IntValue(int64(binpkg.BigEndian.Uint64(b))) }, func(b []byte, v object.Value) { binpkg.BigEndian.PutUint64(b, bufUint(v)) }},
	"s64": {8, func(b []byte) object.Value { return object.IntValue(int64(binpkg.LittleEndian.Uint64(b))) }, func(b []byte, v object.Value) { binpkg.LittleEndian.PutUint64(b, bufUint(v)) }},

	"F32": {4, func(b []byte) object.Value {
		return object.Float(float64(math.Float32frombits(binpkg.BigEndian.Uint32(b))))
	}, func(b []byte, v object.Value) { binpkg.BigEndian.PutUint32(b, math.Float32bits(float32(bufFloat(v)))) }},
	"f32": {4, func(b []byte) object.Value {
		return object.Float(float64(math.Float32frombits(binpkg.LittleEndian.Uint32(b))))
	}, func(b []byte, v object.Value) {
		binpkg.LittleEndian.PutUint32(b, math.Float32bits(float32(bufFloat(v))))
	}},
	"F64": {8, func(b []byte) object.Value { return object.Float(math.Float64frombits(binpkg.BigEndian.Uint64(b))) }, func(b []byte, v object.Value) { binpkg.BigEndian.PutUint64(b, math.Float64bits(bufFloat(v))) }},
	"f64": {8, func(b []byte) object.Value { return object.Float(math.Float64frombits(binpkg.LittleEndian.Uint64(b))) }, func(b []byte, v object.Value) { binpkg.LittleEndian.PutUint64(b, math.Float64bits(bufFloat(v))) }},
}

func bufInt(v object.Value) int64 {
	if i, ok := v.(object.Integer); ok {
		return int64(i)
	}
	if b, ok := object.BigOf(v); ok {
		return b.Int64()
	}
	raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(v))
	return 0
}

func bufUint(v object.Value) uint64 { return uint64(bufInt(v)) }

// The IO::Buffer allocation/state flags, as include/ruby/io/buffer.h numbers
// them. They are also the values the IO::Buffer:: constants carry.
const (
	bufFlagEXTERNAL = 1
	bufFlagINTERNAL = 2
	bufFlagMAPPED   = 4
	bufFlagSHARED   = 8
	bufFlagLOCKED   = 32
	bufFlagPRIVATE  = 64
	bufFlagREADONLY = 128
)

// bufDefaultFlags is io_flags_for_size (io_buffer.c): with no flags given,
// IO::Buffer.new allocates internally below a page and maps from a page upwards.
func bufDefaultFlags(size int64) int64 {
	if size >= 16384 {
		return bufFlagMAPPED
	}
	return bufFlagINTERNAL
}

// bufReinit is io_buffer_initialize applied to an EXISTING buffer object, which
// is what rb_io_buffer_resize does on the two paths that cannot resize in
// place: a NULL-base buffer, and (without mremap) a mapped one. The leading
// bytes are preserved; everything about the buffer's KIND is recomputed from
// the new flags, so a mapped buffer shrunk below a page really does become
// internal.
func bufReinit(b *ioBuffer, size, flags int64) *ioBuffer {
	old := b.data
	*b = ioBuffer{}
	if size == 0 {
		b.null = true
		return b
	}
	b.mapped = flags&bufFlagMAPPED != 0
	b.data = make([]byte, size)
	copy(b.data, old)
	return b
}

// bufFlagsArg is io_buffer_extract_flags: a negative flag set is an ArgumentError
// before anything else, a non-Integer is "not an Integer" as every other
// IO::Buffer number is, and unknown bits are deliberately ignored.
func bufFlagsArg(v object.Value) int64 {
	n, ok := v.(object.Integer)
	if !ok {
		raise("TypeError", "not an Integer")
	}
	if int64(n) < 0 {
		raise("ArgumentError", "Flags can't be negative!")
	}
	mask := int64(bufFlagEXTERNAL | bufFlagINTERNAL | bufFlagMAPPED | bufFlagSHARED |
		bufFlagLOCKED | bufFlagPRIVATE | bufFlagREADONLY)
	return int64(n) & mask
}

// bufOffsetArg is io_buffer_extract_offset. Unlike a size it goes through
// NUM2OFFT, so a non-Integer is rb_num2long's "no implicit conversion from X"
// rather than "not an Integer".
func bufOffsetArg(v object.Value) int64 {
	n, ok := v.(object.Integer)
	if !ok {
		if object.IsNil(v) {
			raise("TypeError", "no implicit conversion from nil")
		}
		raise("TypeError", "no implicit conversion from %s", strings.ToLower(classNameOf(v)))
	}
	if int64(n) < 0 {
		raise("ArgumentError", "Offset can't be negative!")
	}
	return int64(n)
}

// bufSizeArg validates an IO::Buffer size (.new / #resize): a non-Integer raises
// TypeError "not an Integer" and a negative value raises ArgumentError "Size
// can't be negative!" — the exact checks and messages from io_buffer.c
// (io_buffer_initialize / io_buffer_resize, via RB_INTEGER_TYPE_P and the
// negative-size guard).
func bufSizeArg(v object.Value) int64 {
	n, ok := v.(object.Integer)
	if !ok {
		raise("TypeError", "not an Integer")
	}
	if int64(n) < 0 {
		raise("ArgumentError", "Size can't be negative!")
	}
	return int64(n)
}

// bufferArgTypeName names v the way io_buffer.c's "wrong argument type %s
// (expected IO::Buffer)" does: nil/true/false spelled in lower case, otherwise
// the class name. classNameOf already yields "nil" for nil.
func bufferArgTypeName(v object.Value) string {
	if b, ok := v.(object.Bool); ok {
		if bool(b) {
			return "true"
		}
		return "false"
	}
	return classNameOf(v)
}

func bufFloat(v object.Value) float64 {
	switch n := v.(type) {
	case object.Float:
		return float64(n)
	case object.Integer:
		return float64(n)
	}
	raise("TypeError", "no implicit conversion to Float from %s", classNameOf(v))
	return 0
}

// bufferTypeName resolves a type argument (a Symbol) to its accessor, raising the
// ArgumentError MRI raises for an unknown one.
func bufferTypeName(v object.Value) (string, bufType) {
	name, ok := v.(object.Symbol)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Symbol", classNameOf(v))
	}
	t, ok := bufTypes[string(name)]
	if !ok {
		raise("ArgumentError", "Unknown type: %s", string(name))
	}
	return string(name), t
}

// live returns the buffer's bytes, raising if it has been freed (the IO::Buffer
// operations that touch memory require a live buffer).
func (b *ioBuffer) live() []byte {
	if b.parent != nil && !b.sliceValid() {
		raise("IO::Buffer::InvalidatedError", "Buffer has been invalidated!")
	}
	if b.freed {
		raise("IO::Buffer::AccessError", "The buffer is not allocated!")
	}
	return b.data
}

// checkWritable raises the AccessError MRI raises for a write to a read-only
// buffer.
func (b *ioBuffer) checkWritable() {
	if b.readonly {
		raise("IO::Buffer::AccessError", "Buffer is not writable!")
	}
}

// registerIOBuffer installs IO::Buffer with its flag/size constants, the
// AccessError exception, the .new/.for/.string constructors, the state
// predicates, and the byte- and value-level accessors. It runs after registerIO
// so IO exists to scope the class under.
func (vm *VM) registerIOBuffer() {
	cIO := vm.consts["IO"].(*RClass)
	cBuf := newClass("IO::Buffer", vm.cObject)
	vm.cIOBuffer = cBuf
	cIO.consts["Buffer"] = cBuf
	// Sizes and creation flags (MRI values).
	cBuf.consts["DEFAULT_SIZE"] = object.IntValue(65536)
	cBuf.consts["PAGE_SIZE"] = object.IntValue(16384)
	for name, val := range map[string]int64{
		"EXTERNAL": 1, "INTERNAL": 2, "MAPPED": 4, "SHARED": 8, "LOCKED": 32,
		"PRIVATE": 64, "READONLY": 128,
	} {
		cBuf.consts[name] = object.IntValue(val)
	}
	rtErr := vm.consts["RuntimeError"].(*RClass)
	access := newClass("IO::Buffer::AccessError", rtErr)
	cBuf.consts["AccessError"] = access
	vm.consts["IO::Buffer::AccessError"] = access
	// LockedError/InvalidatedError/AllocationError are all RuntimeError
	// subclasses in io_buffer.c (rb_eIOBufferLockedError etc.).
	for _, ex := range []string{"LockedError", "InvalidatedError", "AllocationError"} {
		c := newClass("IO::Buffer::"+ex, rtErr)
		cBuf.consts[ex] = c
		vm.consts["IO::Buffer::"+ex] = c
	}

	sm := func(name string, fn NativeFn) { cBuf.smethods[name] = &Method{name: name, owner: cBuf, native: fn} }
	dm := func(name string, fn NativeFn) { cBuf.define(name, fn) }

	// IO::Buffer.new(size = DEFAULT_SIZE, flags = io_flags_for_size(size)) —
	// rb_io_buffer_initialize + io_buffer_initialize (io_buffer.c). Given no flags
	// MRI chooses them by size: anything below a page is allocated internally, and
	// a page or more is MAPPED. Given flags, one of INTERNAL or MAPPED has to be
	// among them or there is no way to get the memory, which is the
	// AllocationError. A zero size allocates nothing at all and the flags are not
	// even consulted — the buffer is null.
	sm("new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0..2)", len(args))
		}
		size := int64(65536)
		if len(args) > 0 {
			size = bufSizeArg(args[0])
		}
		flags := bufDefaultFlags(size)
		if len(args) > 1 {
			flags = bufFlagsArg(args[1])
		}
		b := &ioBuffer{}
		if size == 0 {
			// io_buffer_initialize never looks at the flags on this path: with no
			// memory to get there is nothing for them to describe, so even READONLY
			// is dropped.
			b.null = true
			return b
		}
		// INTERNAL and MAPPED are the two ways to obtain the memory and one of them
		// must be present; every other bit is simply recorded, so INTERNAL|SHARED is
		// an internally allocated buffer that also reports #shared?.
		switch {
		case flags&bufFlagINTERNAL != 0:
			// allocated internally: internal? is the absence of the other three
		case flags&bufFlagMAPPED != 0:
			b.mapped = true
		default:
			raise("IO::Buffer::AllocationError", "Could not allocate buffer!")
		}
		b.readonly = flags&bufFlagREADONLY != 0
		b.shared = flags&bufFlagSHARED != 0
		b.priv = flags&bufFlagPRIVATE != 0
		b.external = flags&bufFlagEXTERNAL != 0
		b.data = make([]byte, size)
		return b
	})
	// IO::Buffer.map(io, size = nil, offset = 0, flags = 0) — io_buffer_map +
	// io_buffer_map_file (io_buffer.c). The mapping is MAP_SHARED unless PRIVATE
	// was asked for, and PROT_READ|PROT_WRITE unless READONLY was: a shared
	// writable mapping of a stream that was not opened for writing is the EACCES
	// mmap(2) gives, which is the "SystemCallError unless read-only" the spec
	// names.
	//
	// This VM's file streams are their bytes in memory (see IOObj), so a shared
	// mapping IS that byte slice — writes through the buffer are the stream's
	// writes, which is the sharing mmap provides — while a private mapping takes a
	// copy the file can never see.
	sm("map", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 || len(args) > 4 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..4)", len(args))
		}
		o := ioGetIO(vm, args[0])
		if o.closed {
			raise("IOError", "closed stream")
		}
		ioFlush(o)
		fileSize := int64(len(o.buf))
		if st, err := os.Stat(o.path); err == nil {
			fileSize = st.Size()
		}
		var size int64
		if len(args) >= 2 && !object.IsNil(args[1]) {
			if size = bufSizeArg(args[1]); size == 0 {
				raise("ArgumentError", "Size can't be zero!")
			}
		} else {
			if fileSize <= 0 {
				raise("ArgumentError", "Invalid negative or zero file size!")
			}
			size = fileSize
		}
		var offset int64
		if len(args) >= 3 {
			offset = bufOffsetArg(args[2])
		}
		if size > fileSize {
			raise("ArgumentError", "Size can't be larger than file size!")
		}
		if offset+size > fileSize {
			raise("ArgumentError", "Offset too large!")
		}
		flags := int64(0)
		if len(args) >= 4 {
			flags = bufFlagsArg(args[3])
		}
		b := &ioBuffer{mapped: true, readonly: flags&bufFlagREADONLY != 0}
		switch {
		case flags&bufFlagPRIVATE != 0:
			b.priv = true
			b.data = append([]byte(nil), o.buf[offset:offset+size]...)
		default:
			if !b.readonly && !ioFmodeWritable(o) {
				raise("Errno::EACCES", "Permission denied - io_buffer_map_file:mmap")
			}
			b.external, b.shared = true, true
			b.data = o.buf[offset : offset+size : offset+size]
		}
		return b
	})
	sm("for", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		// io_buffer.c io_buffer_for: without a block the buffer is always a
		// read-only copy; with a block over a *mutable* String the buffer aliases
		// the String's bytes (writes propagate) and is writable, while a frozen
		// String still yields a read-only buffer.
		buf := &ioBuffer{external: true}
		if s, ok := args[0].(*object.String); ok && blk != nil && !s.Frozen {
			buf.data = s.MutableBytes()
		} else {
			buf.data = append([]byte(nil), vm.strArgConv(args, 0)...)
			buf.readonly = true
		}
		if blk == nil {
			return buf
		}
		defer func() { buf.freed = true; buf.data = nil }()
		return vm.callBlock(blk, []object.Value{buf})
	})
	// IO::Buffer.string(length) — io_buffer.c rb_io_buffer_type_string. The
	// string is built FIRST, with `rb_str_new(NULL, RB_NUM2LONG(length))`, so its
	// two argument errors come before the block is ever looked for: RB_NUM2LONG
	// refuses a bignum with RangeError, and rb_str_new refuses a negative length
	// with ArgumentError. Only then does the yield raise LocalJumpError — and
	// with "no block given", not rb_yield's usual "no block given (yield)",
	// because the block is called through rb_ensure.
	sm("string", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if _, isBig := args[0].(*object.Bignum); isBig {
			raise("RangeError", "bignum too big to convert into 'long'")
		}
		size := intArg(args[0])
		if size < 0 {
			raise("ArgumentError", "negative string size (or size too big)")
		}
		if blk == nil {
			raise("LocalJumpError", "no block given")
		}
		// The STRING is the object that survives: rb_io_buffer_type_string builds
		// it first and the buffer only aliases its bytes, so a #free inside the
		// block detaches the buffer and leaves everything already written in place.
		// Copying out of the buffer afterwards instead loses it — `.string(4) { |b|
		// b.set_string("meat"); b.free }` came back "" rather than "meat".
		out := object.NewStringBytesEnc(make([]byte, size), "ASCII-8BIT")
		buf := &ioBuffer{data: out.MutableBytes(), external: true}
		// io_buffer_for_yield_instance_ensure frees the instance however the block
		// leaves — including by raising.
		defer func() { buf.freed = true; buf.data = nil }()
		vm.callBlock(blk, []object.Value{buf})
		return out
	})

	dm("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
		if b.freed {
			return object.IntValue(0)
		}
		return object.IntValue(int64(len(b.data)))
	})
	pred := func(name string, fn func(b *ioBuffer) bool) {
		dm(name, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Bool(fn(self.(*ioBuffer)))
		})
	}
	// rb_io_buffer_null_p is `base == NULL` and rb_io_buffer_empty_p is
	// `size == 0`. They are DIFFERENT questions that happen to answer alike for
	// a buffer that has given up its memory — #free, #resize(0) and #transfer all
	// leave base NULL and size 0 — so neither may exclude the other's case.
	pred("null?", func(b *ioBuffer) bool { return b.baseNull() })
	pred("empty?", func(b *ioBuffer) bool { return b.freed || len(b.data) == 0 })
	pred("valid?", func(b *ioBuffer) bool { return b.sliceValid() })
	pred("external?", func(b *ioBuffer) bool { return !b.freed && b.external })
	// The four allocation flags are exclusive in practice: a buffer is internal,
	// external, mapped-and-shared or mapped-and-private, and io_buffer.c keeps them
	// as separate bits rather than deriving one from the absence of the others — a
	// private mapping is neither internal nor external, which is why internal? has
	// to exclude the mapped forms explicitly.
	pred("internal?", func(b *ioBuffer) bool {
		return !b.freed && !b.null && !b.external && !b.borrowed && !b.mapped
	})
	pred("mapped?", func(b *ioBuffer) bool { return !b.freed && !b.null && b.mapped })
	pred("shared?", func(b *ioBuffer) bool { return !b.freed && b.shared })
	pred("private?", func(b *ioBuffer) bool { return !b.freed && b.priv })
	pred("locked?", func(b *ioBuffer) bool { return b.locked })
	pred("readonly?", func(b *ioBuffer) bool { return b.readonly })

	// slice(offset = 0, length = size - offset) returns a buffer that shares this
	// buffer's memory over the given range, so writes through the slice are visible
	// in the original. Like MRI's slice it reports as internal, inherits the
	// read-only flag, and can itself be resized; an out-of-range range raises
	// ArgumentError.
	dm("slice", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
		data := b.live()
		off := 0
		if len(args) > 0 {
			off = int(intArg(args[0]))
		}
		length := len(data) - off
		if len(args) > 1 {
			length = int(intArg(args[1]))
		}
		if off < 0 || length < 0 || off+length > len(data) {
			raise("ArgumentError", "Specified offset+length is bigger than the buffer size!")
		}
		return &ioBuffer{
			data: data[off : off+length : off+length], readonly: b.readonly,
			borrowed: true, parent: b, sliceOff: off, sliceLen: length,
			// base = parent->base + offset, taken now: NULL plus zero is NULL.
			null: b.baseNull(),
		}
	})
	// resize(size) reallocates the buffer to the new size, preserving the leading
	// bytes (a larger buffer is zero-filled). An external buffer (from .for /
	// .string, or a slice) cannot be resized.
	dm("resize", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
		if b.locked {
			raise("IO::Buffer::LockedError", "Cannot resize locked buffer!")
		}
		size := bufSizeArg(args[0])
		// rb_io_buffer_resize takes the NULL-base case first, before the external
		// check: a buffer with no memory is re-initialized from scratch, which is
		// why `IO::Buffer.for("x").free.resize(10)` is allowed and why
		// `IO::Buffer.new(0).resize(PAGE_SIZE)` comes back MAPPED rather than
		// internal — io_flags_for_size decides, exactly as it does for
		// IO::Buffer.new.
		if b.baseNull() {
			return bufReinit(b, size, bufDefaultFlags(size))
		}
		if b.external {
			raise("IO::Buffer::AccessError", "Cannot resize external buffer!")
		}
		if b.mapped {
			// mremap is a Linux extension; without it io_buffer_resize_copy runs,
			// allocating a fresh buffer whose flags come from the NEW size. rbgo has
			// no mremap on any platform (there is no mapping to remap — see
			// ioBuffer), so this is the only branch, and the shim pins the platform
			// to darwin, where MRI takes it too.
			return bufReinit(b, size, bufDefaultFlags(size))
		}
		// RB_IO_BUFFER_INTERNAL: realloc in place, keeping the kind. A resize to
		// zero is io_buffer_free.
		if size == 0 {
			b.data = nil
			b.freed = true
			return b
		}
		nd := make([]byte, size)
		copy(nd, b.data)
		b.data = nd
		b.borrowed = false
		b.parent = nil
		return b
	})

	// free (io_buffer.c rb_io_buffer_free) releases the memory and leaves a NULL
	// buffer. It refuses while the buffer is locked — the block form of #locked
	// is the only way in, and rb_io_buffer_free_locked (which unlocks first) is
	// reserved for the C API's own teardown.
	dm("free", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
		if b.locked {
			raise("IO::Buffer::LockedError", "Buffer is locked!")
		}
		b.freed = true
		b.data = nil
		return b
	})

	// transfer (io_buffer.c io_buffer_transfer) moves this buffer's memory and its
	// kind (external/read-only/slice) to a fresh buffer and nullifies the original,
	// so #null? on the source becomes true. It is refused while the buffer is
	// locked.
	dm("transfer", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
		if b.locked {
			raise("IO::Buffer::LockedError", "Cannot transfer ownership of locked buffer!")
		}
		nb := &ioBuffer{
			data:     b.data,
			readonly: b.readonly,
			external: b.external,
			borrowed: b.borrowed,
			parent:   b.parent,
			sliceOff: b.sliceOff,
			sliceLen: b.sliceLen,
		}
		b.freed = true
		b.data = nil
		return nb
	})

	// locked (io_buffer.c io_buffer_locked) marks the buffer locked for the
	// duration of the block, so #locked? is true inside and structural changes
	// (#resize/#transfer) raise LockedError. Re-entering raises "Buffer already
	// locked!". The lock does not propagate to or from slices.
	dm("locked", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		b := self.(*ioBuffer)
		if blk == nil {
			// io_buffer.c io_buffer_locked checks rb_block_given_p explicitly and
			// raises "no block given" (no "(yield)" suffix, unlike a bare yield).
			raise("LocalJumpError", "no block given")
		}
		if b.locked {
			raise("IO::Buffer::LockedError", "Buffer already locked!")
		}
		b.locked = true
		defer func() { b.locked = false }()
		return vm.callBlock(blk, []object.Value{b})
	})

	// get_string(offset = 0, length = size - offset, encoding = BINARY).
	dm("get_string", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
		data := b.live()
		off := intArgOr(args, 0)
		length := int64(len(data)) - off
		if len(args) > 1 && !object.IsNil(args[1]) {
			length = intArg(args[1])
		}
		enc := "ASCII-8BIT"
		if len(args) > 2 && !object.IsNil(args[2]) {
			enc = vm.encodingName(args[2])
		}
		if off < 0 || length < 0 || off+length > int64(len(data)) {
			raise("ArgumentError", "Offset/length out of bounds!")
		}
		return object.NewStringBytesEnc(append([]byte(nil), data[off:off+length]...), enc)
	})
	// set_string(string, offset = 0, length = string.bytesize, source_offset = 0).
	dm("set_string", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
		b.checkWritable()
		data := b.live()
		src := vm.strArgConv(args, 0)
		off := intArgOr(args[1:], 0)
		srcOff := int64(0)
		if len(args) > 3 {
			srcOff = intArg(args[3])
		}
		length := int64(len(src)) - srcOff
		if len(args) > 2 && !object.IsNil(args[2]) {
			length = intArg(args[2])
		}
		if off < 0 || length < 0 || srcOff < 0 || srcOff+length > int64(len(src)) || off+length > int64(len(data)) {
			raise("ArgumentError", "Offset/length out of bounds!")
		}
		copy(data[off:off+length], src[srcOff:srcOff+length])
		return object.IntValue(length)
	})

	dm("get_value", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
		data := b.live()
		_, t := bufferTypeName(args[0])
		off := intArg(args[1])
		if off < 0 || off+int64(t.size) > int64(len(data)) {
			raise("ArgumentError", "Offset/length out of bounds!")
		}
		return t.get(data[off:])
	})
	dm("set_value", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
		b.checkWritable()
		data := b.live()
		_, t := bufferTypeName(args[0])
		off := intArg(args[1])
		if off < 0 || off+int64(t.size) > int64(len(data)) {
			raise("ArgumentError", "Offset/length out of bounds!")
		}
		t.put(data[off:], args[2])
		return object.IntValue(off + int64(t.size))
	})

	dm("clear", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
		b.checkWritable()
		data := b.live()
		fill := byte(0)
		if len(args) > 0 {
			fill = byte(intArg(args[0]))
		}
		off := int64(0)
		if len(args) > 1 {
			off = intArg(args[1])
		}
		end := int64(len(data))
		if len(args) > 2 {
			end = off + intArg(args[2])
		}
		for i := off; i < end && i < int64(len(data)); i++ {
			data[i] = fill
		}
		return b
	})

	dm("bit_count", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		n := 0
		for _, by := range self.(*ioBuffer).live() {
			for by != 0 {
				n += int(by & 1)
				by >>= 1
			}
		}
		return object.IntValue(int64(n))
	})

	// maskBytes reads the argument buffer of a binary bitwise operator, raising the
	// io_buffer.c messages: TypeError "wrong argument type X (expected IO::Buffer)"
	// for a non-buffer, ArgumentError for a zero-length mask.
	maskBytes := func(v object.Value) []byte {
		other, ok := v.(*ioBuffer)
		if !ok {
			raise("TypeError", "wrong argument type %s (expected IO::Buffer)", bufferArgTypeName(v))
		}
		ob := other.live()
		if len(ob) == 0 {
			raise("ArgumentError", "Other buffer has zero length!")
		}
		return ob
	}
	// Bitwise operators combine two buffers (a shorter mask is repeated across the
	// source): &/|/^ return a fresh internal buffer, and!/or!/xor! mutate the
	// receiver in place and return it. ~ / not! complement every byte likewise.
	bitOp := func(name string, op func(a, b byte) byte) {
		dm(name, func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			a := self.(*ioBuffer).live()
			ob := maskBytes(args[0])
			out := make([]byte, len(a))
			for i := range a {
				out[i] = op(a[i], ob[i%len(ob)])
			}
			return &ioBuffer{data: out}
		})
	}
	bitOpBang := func(name string, op func(a, b byte) byte) {
		dm(name, func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			b := self.(*ioBuffer)
			b.checkWritable()
			a := b.live()
			ob := maskBytes(args[0])
			for i := range a {
				a[i] = op(a[i], ob[i%len(ob)])
			}
			return b
		})
	}
	bitOp("&", func(a, b byte) byte { return a & b })
	bitOp("|", func(a, b byte) byte { return a | b })
	bitOp("^", func(a, b byte) byte { return a ^ b })
	bitOpBang("and!", func(a, b byte) byte { return a & b })
	bitOpBang("or!", func(a, b byte) byte { return a | b })
	bitOpBang("xor!", func(a, b byte) byte { return a ^ b })
	dm("~", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*ioBuffer).live()
		out := make([]byte, len(a))
		for i := range a {
			out[i] = ^a[i]
		}
		return &ioBuffer{data: out}
	})
	dm("not!", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
		b.checkWritable()
		a := b.live()
		for i := range a {
			a[i] = ^a[i]
		}
		return b
	})

	// == (and inspect) are handled by the shared valueEqual / Inspect paths.
	dm("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ioBuffer).ToS())
	})
}

// strArgConv reads the i-th argument as a String's bytes, converting via #to_str
// when necessary and raising TypeError otherwise.
func (vm *VM) strArgConv(args []object.Value, i int) []byte {
	v := args[i]
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return s.Bytes()
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	return nil
}
