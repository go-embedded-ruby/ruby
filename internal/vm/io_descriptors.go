// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
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
	cls.define("ungetbyte", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		switch a := args[0].(type) {
		case object.Integer:
			ioUnget(o, []byte{byte(a)})
		case *object.String:
			ioUnget(o, a.Bytes())
		default:
			if args[0] != object.NilV { // a nil argument is a no-op, as in MRI
				raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(args[0]))
			}
		}
		return object.NilV
	})
	cls.define("ungetc", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		switch a := args[0].(type) {
		case object.Integer:
			ioUnget(o, []byte(string(rune(a))))
		case *object.String:
			ioUnget(o, a.Bytes())
		default:
			if args[0] != object.NilV {
				raise("TypeError", "no implicit conversion of %s into String", classNameOf(args[0]))
			}
		}
		return object.NilV
	})
	cls.define("each_byte", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		o.pipeRefresh()
		for o.pos < len(o.buf) {
			b := o.buf[o.pos]
			o.pos++
			vm.callBlock(blk, []object.Value{object.IntValue(int64(b))})
		}
		return self
	})
	each := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		for {
			v := ioGets(o, args)
			if v == object.NilV {
				break
			}
			vm.callBlock(blk, []object.Value{v})
		}
		return self
	}
	cls.define("each", each)
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
		return object.IntValue(int64(self.(*IOObj).lineno))
	})
	cls.define("lineno=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*IOObj).lineno = int(intArg(args[0]))
		return args[0]
	})
	cls.define("close_read", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		o.rdClosed = true
		if o.wrClosed { // both halves shut ⇒ the stream is fully closed
			o.closed = true
		}
		return object.NilV
	})
	cls.define("close_write", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
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
	cls.define("pread", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		o.pipeRefresh()
		n := int(intArg(args[0]))
		if n < 0 {
			raise("ArgumentError", "negative string size (or size too big)")
		}
		off := int(intArg(args[1]))
		if off < 0 {
			raise("Errno::EINVAL", "Invalid argument - pread")
		}
		var buf *object.String
		if len(args) > 2 {
			if b, ok := args[2].(*object.String); ok {
				buf = b
			}
		}
		if n == 0 {
			return ioReadResult(nil, buf)
		}
		if off >= len(o.buf) {
			raise("EOFError", "end of file reached")
		}
		data := o.buf[off:min(off+n, len(o.buf))]
		return ioReadResult(data, buf)
	})
	cls.define("pwrite", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		off := int(intArg(args[1]))
		if off < 0 {
			raise("Errno::EINVAL", "Invalid argument - pwrite")
		}
		data := []byte(args[0].ToS())
		if end := off + len(data); end > len(o.buf) {
			o.buf = append(o.buf, make([]byte, end-len(o.buf))...)
		}
		copy(o.buf[off:], data)
		o.writable = true // a written File flushes its buffer back on flush/close
		return object.IntValue(int64(len(data)))
	})
	cls.define("sysseek", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		amount := int(intArg(args[0]))
		switch whenceArg(args) {
		case 1: // SEEK_CUR
			o.pos += amount
		case 2: // SEEK_END
			o.pos = len(o.buf) + amount
		default: // SEEK_SET
			o.pos = amount
		}
		return object.IntValue(int64(o.pos))
	})
	cls.define("binmode?", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(false)
	})
	cls.define("autoclose?", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(true)
	})
	cls.define("autoclose=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return args[0]
	})
	cls.define("fdatasync", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(0)
	})
}

// whenceArg returns the SEEK_* whence of a seek-style argument list (default 0).
func whenceArg(args []object.Value) int {
	if len(args) > 1 {
		return int(intArg(args[1]))
	}
	return 0
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
