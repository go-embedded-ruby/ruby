// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// defIOReadExtra installs the byte/char-oriented read protocol and the
// descriptor-state methods shared by IO, File and StringIO: getbyte/readbyte,
// ungetbyte/ungetc, readchar, each_byte, sysread/syswrite, #lineno and the
// half-close methods (close_read/close_write and their predicates). They operate
// on the same in-memory buffer + cursor the rest of the read protocol uses.
func defIOReadExtra(cls *RClass) {
	cls.define("getbyte", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		o.pipeRefresh()
		if o.pos >= len(o.buf) {
			return object.NilV
		}
		b := o.buf[o.pos]
		o.pos++
		return object.IntValue(int64(b))
	})
	cls.define("readbyte", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		o.pipeRefresh()
		if o.pos >= len(o.buf) {
			raise("EOFError", "end of file reached")
		}
		b := o.buf[o.pos]
		o.pos++
		return object.IntValue(int64(b))
	})
	cls.define("readchar", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		o.pipeRefresh()
		if o.pos >= len(o.buf) {
			raise("EOFError", "end of file reached")
		}
		r, sz := utf8.DecodeRune(o.buf[o.pos:])
		o.pos += sz
		return object.NewString(string(r))
	})
	cls.define("ungetbyte", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		if object.IsNil(args[0]) { // a nil argument is a no-op, as in MRI
			return object.NilV
		}
		if bi, ok := object.BigOf(args[0]); ok {
			// rb_io_ungetbyte: an Integer/Bignum is reduced modulo 256 to a single
			// byte (rb_int_modulo(b, 256) & 0xFF), so it never raises RangeError.
			m := new(big.Int).Mod(bi, big.NewInt(256))
			ioUnget(o, []byte{byte(m.Int64())})
			return object.NilV
		}
		// Any other value is coerced with #to_str (StringValue), raising
		// "no implicit conversion of <x> into String" when it cannot be.
		ioUnget(o, vm.strToStr(args[0]))
		return object.NilV
	})
	cls.define("ungetc", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		// rb_io_ungetc: an Integer is the codepoint (encoded to bytes); anything else
		// is coerced with StringValue (#to_str), so nil / a non-String object raises
		// "no implicit conversion of <x> into String". StringIO#ungetc (strio_ungetc)
		// diverges only for nil, which it treats as a no-op.
		switch {
		case object.IsNil(args[0]) && ioIsStringIO(o):
			// StringIO#ungetc(nil): no-op.
		default:
			if a, ok := args[0].(object.Integer); ok {
				ioUnget(o, []byte(string(rune(a))))
			} else {
				ioUnget(o, vm.strToStr(args[0]))
			}
		}
		return object.NilV
	})
	cls.define("each_byte", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		o := self.(*IOObj)
		if blk == nil { // no block ⇒ an Enumerator (buildable even on a closed stream)
			return enumForSized(self, "each_byte", enumSizeNil)
		}
		ioCheckReadable(o) // iterating a closed/unreadable stream raises, as in MRI
		o.pipeRefresh()
		for o.pos < len(o.buf) {
			b := o.buf[o.pos]
			o.pos++
			vm.callBlock(blk, []object.Value{object.IntValue(int64(b))})
		}
		return self
	})
	// #each is a true alias of #each_line (defined in defStringIORead, which runs
	// before defIOReadExtra): they must share the method entry so
	// IO.instance_method(:each) == IO.instance_method(:each_line), as in MRI, and
	// #each inherits each_line's separator/limit/$/ handling.
	cls.methods["each"] = cls.methods["each_line"]
	cls.define("sysread", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		o.pipeRefresh()
		n := int(intArg(args[0]))
		if n < 0 {
			raise("ArgumentError", "negative length %d given", n)
		}
		var buf *object.String
		if len(args) > 1 {
			if b, ok := args[1].(*object.String); ok {
				buf = b
			}
		}
		if n == 0 {
			return ioReadResult(nil, buf) // a zero-length sysread is "" even at EOF
		}
		if o.pos >= len(o.buf) {
			raise("EOFError", "end of file reached")
		}
		end := min(o.pos+n, len(o.buf))
		data := o.buf[o.pos:end]
		o.pos = end
		return ioReadResult(data, buf)
	})
	cls.define("syswrite", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		return object.IntValue(int64(o.writeStr(args[0].ToS())))
	})
	cls.define("lineno", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if !ioIsStringIO(o) { // rb_io_check_char_readable: a closed/write-only real IO raises
			ioCheckReadable(o)
		}
		return object.IntValue(int64(o.lineno))
	})
	cls.define("lineno=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// rb_io_set_lineno: the stream must be char-readable (a closed/write-only
		// real IO raises), then NUM2INT coerces via #to_int (a Float truncates).
		o := self.(*IOObj)
		if !ioIsStringIO(o) {
			ioCheckReadable(o)
		}
		o.lineno = vm.ioCIntArg(args[0])
		return args[0]
	})
	cls.define("close_read", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.rdModeOff { // a StringIO opened write-only has no read half to close (MRI)
			raise("IOError", "not opened for reading")
		}
		o.rdClosed = true
		if o.wrClosed { // both halves shut ⇒ the stream is fully closed
			o.closed = true
		}
		return object.NilV
	})
	cls.define("close_write", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.wrModeOff { // a StringIO opened read-only has no write half to close (MRI)
			raise("IOError", "not opened for writing")
		}
		ioFlush(o)
		o.wrClosed = true
		if o.rdClosed {
			o.closed = true
		}
		return object.NilV
	})
	cls.define("closed_read?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		return object.Bool(o.closed || o.rdClosed)
	})
	cls.define("closed_write?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		return object.Bool(o.closed || o.wrClosed)
	})
}

// defIOSeekable installs the positioned + descriptor methods that MRI defines on
// IO (and hence File) but not on StringIO: pread/pwrite, sysseek and the
// binary-mode / autoclose / fdatasync accessors. pread/pwrite address the buffer
// by absolute offset without disturbing the cursor.
func defIOSeekable(cls *RClass) {
	cls.define("pread", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		// rb_io_pread: len (NUM2SIZET) and offset (NUM2OFFT) are coerced with
		// #to_int, then io_setstrbuf coerces the optional buffer with #to_str —
		// all before the maxlen==0 early return, so a non-String buffer still
		// raises and a zero-length read leaves the buffer's bytes untouched.
		n := int(vm.toIntCoerce(args[0]))
		off := int(vm.toIntCoerce(args[1]))
		var buf *object.String
		if len(args) > 2 {
			buf = vm.ioBufferArg(args[2])
		}
		if n < 0 {
			raise("ArgumentError", "negative string size (or size too big)")
		}
		if n == 0 { // MRI returns the (coerced) buffer unshrunk, or a fresh ""
			if buf != nil {
				return buf
			}
			return ioReadResult(nil, nil)
		}
		ioCheckReadable(o)
		o.pipeRefresh()
		if off < 0 {
			raise("Errno::EINVAL", "Invalid argument - pread")
		}
		if off >= len(o.buf) {
			raise("EOFError", "end of file reached")
		}
		data := o.buf[off:min(off+n, len(o.buf))]
		return ioReadResult(data, buf)
	})
	cls.define("pwrite", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		// rb_io_pwrite: a non-String object is coerced with #to_s
		// (rb_obj_as_string) and the offset with #to_int (NUM2OFFT) before the
		// stream is checked, so a missing #to_s surfaces as NoMethodError and a
		// non-Integer offset as the "into Integer" TypeError.
		data := []byte(vm.objAsString(args[0]))
		off := int(vm.toIntCoerce(args[1]))
		ioCheckOpen(o)
		if off < 0 {
			raise("Errno::EINVAL", "Invalid argument - pwrite")
		}
		if end := off + len(data); end > len(o.buf) {
			o.buf = append(o.buf, make([]byte, end-len(o.buf))...)
		}
		copy(o.buf[off:], data)
		o.writable = true // a written File flushes its buffer back on flush/close
		return object.IntValue(int64(len(data)))
	})
	cls.define("sysseek", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		amount := int(vm.toIntCoerce(args[0])) // NUM2OFFT: coerces via #to_int
		whence := 0
		if len(args) > 1 {
			whence = vm.seekWhence(args[1]) // accepts :SET/:CUR/:END symbols
		}
		switch whence {
		case 1: // SEEK_CUR
			o.pos += amount
		case 2: // SEEK_END
			o.pos = len(o.buf) + amount
		default: // SEEK_SET
			o.pos = amount
		}
		return object.IntValue(int64(o.pos))
	})
	cls.define("binmode?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.closed {
			raise("IOError", "closed stream")
		}
		return object.Bool(o.binmode)
	})
	cls.define("autoclose?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.closed { // MRI: #autoclose? cannot be queried on a closed IO
			raise("IOError", "closed stream")
		}
		return object.Bool(!o.noAutoclose)
	})
	cls.define("autoclose=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.closed { // MRI: #autoclose= cannot be set on a closed IO
			raise("IOError", "closed stream")
		}
		o.noAutoclose = !args[0].Truthy()
		return args[0]
	})
	cls.define("fdatasync", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(0)
	})
}

// ioUnget inserts p immediately before the cursor (leaving the cursor on the
// re-inserted bytes) so a following read returns them first — the pushback model
// shared by ungetbyte and ungetc.
func ioUnget(o *IOObj, p []byte) {
	if len(p) == 0 {
		return
	}
	out := make([]byte, 0, len(o.buf)+len(p))
	out = append(out, o.buf[:o.pos]...)
	out = append(out, p...)
	out = append(out, o.buf[o.pos:]...)
	o.buf = out
}

// ioReadResult returns data as a fresh String, or fills the caller's output
// buffer String and returns it — the shared return convention of the length-taking
// read methods (sysread/pread).
func ioReadResult(data []byte, buf *object.String) object.Value {
	if buf != nil {
		if buf.Frozen {
			raise("FrozenError", "can't modify frozen String: %s", buf.Inspect())
		}
		buf.SetBytes(append([]byte(nil), data...))
		return buf
	}
	return object.NewStringBytes(append([]byte(nil), data...))
}
