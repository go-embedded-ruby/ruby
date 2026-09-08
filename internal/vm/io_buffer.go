// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	binpkg "encoding/binary"
	"math"
	"math/big"

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
}

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

	sm("new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		size := int64(65536)
		if len(args) > 0 {
			size = bufSizeArg(args[0])
		}
		flags := int64(0)
		if len(args) > 1 {
			flags = intArg(args[1])
		}
		return &ioBuffer{data: make([]byte, size), readonly: flags&128 != 0}
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
	sm("string", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		size := intArg(args[0])
		buf := &ioBuffer{data: make([]byte, size), external: true}
		vm.callBlock(blk, []object.Value{buf})
		out := object.NewStringBytesEnc(append([]byte(nil), buf.data...), "ASCII-8BIT")
		buf.freed = true
		buf.data = nil
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
	pred("null?", func(b *ioBuffer) bool { return b.freed })
	pred("empty?", func(b *ioBuffer) bool { return !b.freed && len(b.data) == 0 })
	pred("valid?", func(b *ioBuffer) bool { return b.sliceValid() })
	pred("external?", func(b *ioBuffer) bool { return !b.freed && b.external })
	pred("internal?", func(b *ioBuffer) bool { return !b.freed && !b.external && !b.borrowed })
	pred("mapped?", func(b *ioBuffer) bool { return false })
	pred("shared?", func(b *ioBuffer) bool { return false })
	pred("private?", func(b *ioBuffer) bool { return false })
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
		return &ioBuffer{data: data[off : off+length : off+length], readonly: b.readonly, borrowed: true, parent: b, sliceOff: off, sliceLen: length}
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
		// A live external buffer (.for/.string) cannot be resized; a *freed*
		// (null) one may be, reallocating into a fresh internal buffer.
		if b.external && !b.freed {
			raise("IO::Buffer::AccessError", "Cannot resize external buffer!")
		}
		if size == 0 {
			b.data = nil
			b.freed = true
			return b
		}
		nd := make([]byte, size)
		copy(nd, b.data)
		b.data = nd
		b.freed = false
		b.external = false
		b.borrowed = false
		b.parent = nil
		return b
	})

	dm("free", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
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
