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
	freed    bool
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
	access := newClass("IO::Buffer::AccessError", vm.consts["RuntimeError"].(*RClass))
	cBuf.consts["AccessError"] = access
	vm.consts["IO::Buffer::AccessError"] = access

	sm := func(name string, fn NativeFn) { cBuf.smethods[name] = &Method{name: name, owner: cBuf, native: fn} }
	dm := func(name string, fn NativeFn) { cBuf.define(name, fn) }

	sm("new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		size := int64(65536)
		if len(args) > 0 {
			size = intArg(args[0])
		}
		flags := int64(0)
		if len(args) > 1 {
			flags = intArg(args[1])
		}
		return &ioBuffer{data: make([]byte, size), readonly: flags&128 != 0}
	})
	sm("for", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		s := vm.strArgConv(args, 0)
		buf := &ioBuffer{data: append([]byte(nil), s...), external: true, readonly: true}
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
	pred("valid?", func(b *ioBuffer) bool { return true })
	pred("external?", func(b *ioBuffer) bool { return !b.freed && b.external })
	pred("internal?", func(b *ioBuffer) bool { return !b.freed && !b.external })
	pred("mapped?", func(b *ioBuffer) bool { return false })
	pred("shared?", func(b *ioBuffer) bool { return false })
	pred("private?", func(b *ioBuffer) bool { return false })
	pred("locked?", func(b *ioBuffer) bool { return false })
	pred("readonly?", func(b *ioBuffer) bool { return b.readonly })

	dm("free", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*ioBuffer)
		b.freed = true
		b.data = nil
		return b
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

	// Bitwise operators combine two equal-length buffers (or a buffer and a shorter
	// one, repeating it) into a fresh internal buffer; ~ complements every byte.
	bitOp := func(name string, op func(a, b byte) byte) {
		dm(name, func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			a := self.(*ioBuffer).live()
			other, ok := args[0].(*ioBuffer)
			if !ok {
				raise("TypeError", "no implicit conversion of %s into IO::Buffer", classNameOf(args[0]))
			}
			ob := other.live()
			if len(ob) == 0 {
				raise("ArgumentError", "Other buffer has zero length!")
			}
			out := make([]byte, len(a))
			for i := range a {
				out[i] = op(a[i], ob[i%len(ob)])
			}
			return &ioBuffer{data: out}
		})
	}
	bitOp("&", func(a, b byte) byte { return a & b })
	bitOp("|", func(a, b byte) byte { return a | b })
	bitOp("^", func(a, b byte) byte { return a ^ b })
	dm("~", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*ioBuffer).live()
		out := make([]byte, len(a))
		for i := range a {
			out[i] = ^a[i]
		}
		return &ioBuffer{data: out}
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
