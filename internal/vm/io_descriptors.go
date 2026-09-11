// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"os"
	"strings"

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
	cls.define("readchar", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		o.pipeRefresh()
		if o.pos >= len(o.buf) {
			raise("EOFError", "end of file reached")
		}
		// One character is a full character of the stream's external encoding — a
		// multi-byte EUC-JP/UTF-8 char, not a single byte — which is then transcoded
		// to the internal encoding when one is set (io.c io_getc → read_all path).
		// decodeCharFrom never reports more bytes than remain (an incomplete lead
		// yields 0, taken as a one-byte character), so the slice below is in bounds.
		ext, _ := vm.ioReadEnc(o)
		_, sz, _, _ := vm.decodeCharFrom(o.buf[o.pos:], ext)
		if sz < 1 {
			sz = 1 // an invalid or truncated lead byte is a one-byte character
		}
		charBytes := o.buf[o.pos : o.pos+sz]
		o.pos += sz
		return vm.ioDecodeRead(o, charBytes)
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
	cls.define("sysread", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		o.pipeRefresh()
		n := int(intArg(args[0]))
		if n < 0 {
			raise("ArgumentError", "negative length %d given", n)
		}
		// The optional output buffer is coerced with #to_str (io_setstrbuf →
		// StringValue), as for pread.
		var buf *object.String
		if len(args) > 1 {
			buf = vm.ioBufferArg(args[1])
		}
		if n == 0 { // a zero-length sysread returns "" (or the buffer untouched)
			if buf != nil {
				return buf
			}
			return ioReadResult(nil, nil)
		}
		if o.pos >= len(o.buf) {
			// MRI empties the output buffer (io_set_read_length to 0) before
			// signalling end-of-file.
			if buf != nil {
				buf.SetBytes(nil)
			}
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
	// write_nonblock(string, exception: true): a non-blocking write. To a file it
	// writes the string and returns the byte count exactly like IO#write (io.c
	// io_write_nonblock shares io_binwrite). To a pipe it models a finite kernel
	// buffer: once the unread backlog reaches the pipe capacity a further write
	// would block, so it reports would-block — raising Errno::EAGAIN, or returning
	// :wait_writable when exception: false is given. It is an IO/File method (not
	// StringIO).
	cls.define("write_nonblock", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		pos, opts := splitIOOpts(args)
		if len(pos) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		raiseOnBlock := true
		if opts != nil {
			if v, ok := opts.Get(object.Symbol("exception")); ok {
				raiseOnBlock = v.Truthy()
			}
		}
		if o.pipe != nil && o.isWriteEnd {
			// A typical pipe holds 64 KiB of unread data before a write blocks.
			const pipeCapacity = 65536
			if len(o.pipe.data)-o.pipe.rpos >= pipeCapacity {
				if !raiseOnBlock {
					return object.Symbol("wait_writable")
				}
				raise("IO::EAGAINWaitWritable", "Resource temporarily unavailable - write would block")
			}
		}
		o.nonblock = true // io_write_nonblock leaves the descriptor non-blocking
		return object.IntValue(vm.ioWriteAll(o, pos[:1]))
	})
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
		if o.closed { // MRI: sysseek on a closed stream raises before coercing the offset
			raise("IOError", "closed stream")
		}
		// NUM2OFFT: coerces via #to_int; a Bignum too large for the off_t raises
		// RangeError rather than TypeError.
		amount := vm.ioOfftArg(args[0])
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
	// set_encoding_by_bom (io.c rb_io_set_encoding_by_bom): if the stream begins
	// with a Unicode byte-order mark, consume it and set the external encoding to
	// the one the BOM names, returning that Encoding; otherwise leave the stream
	// untouched and return nil. The stream must be in binary mode with no encoding
	// already set (ArgumentError otherwise), and a non-readable stream simply
	// returns nil.
	cls.define("set_encoding_by_bom", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.closed { // GetOpenFile: a closed stream raises IOError first
			raise("IOError", "closed stream")
		}
		if !o.binmode {
			raise("ArgumentError", "ASCII incompatible encoding needs binmode")
		}
		if o.intEnc != "" {
			raise("ArgumentError", "encoding conversion is set")
		}
		if o.extEnc != "" && o.extEnc != "ASCII-8BIT" {
			raise("ArgumentError", "encoding is set to %s already", o.extEnc)
		}
		if o.rdClosed { // a write-only stream is not readable: no BOM to strip
			return object.NilV
		}
		// rbgo buffers a file's bytes at open; refresh a read-only file-backed
		// stream from disk so a BOM written after the open (as the specs do) is
		// visible, mirroring MRI's lazy read from the descriptor.
		if o.path != "" && !o.writable {
			if b, err := os.ReadFile(o.path); err == nil {
				o.buf = b
			}
		}
		enc := ioStripBOM(o)
		if enc == "" {
			return object.NilV
		}
		o.extEnc = enc
		e, _ := vm.findEncoding(enc) // every BOM name is a registered encoding
		return e
	})
}

// ioStripBOM inspects the bytes at the cursor for a Unicode byte-order mark and,
// on a full match, advances the cursor past it and returns the encoding name the
// BOM designates ("" when none is found). It mirrors io.c io_strip_bom: a
// truncated BOM leaves the cursor where it was, and "\xFF\xFE" followed by two
// NUL bytes is UTF-32LE while "\xFF\xFE" alone is UTF-16LE.
func ioStripBOM(o *IOObj) string {
	n, enc := detectBOM(o.buf[o.pos:])
	o.pos += n
	return enc
}

// detectBOM reports the byte length and encoding name of a leading Unicode
// byte-order mark in p (0, "" when there is none). "\xFF\xFE" followed by two
// NUL bytes is UTF-32LE, while "\xFF\xFE" alone is UTF-16LE (io.c io_strip_bom).
func detectBOM(p []byte) (int, string) {
	switch {
	case len(p) >= 3 && p[0] == 0xEF && p[1] == 0xBB && p[2] == 0xBF:
		return 3, "UTF-8"
	case len(p) >= 2 && p[0] == 0xFF && p[1] == 0xFE:
		if len(p) >= 4 && p[2] == 0x00 && p[3] == 0x00 {
			return 4, "UTF-32LE"
		}
		return 2, "UTF-16LE"
	case len(p) >= 2 && p[0] == 0xFE && p[1] == 0xFF:
		return 2, "UTF-16BE"
	case len(p) >= 4 && p[0] == 0x00 && p[1] == 0x00 && p[2] == 0xFE && p[3] == 0xFF:
		return 4, "UTF-32BE"
	}
	return 0, ""
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

// defIOReopen installs IO#reopen and IO#fcntl — the two descriptor-rebinding
// methods MRI implements on top of dup2()/freopen()/fcntl(), modelled here on the
// buffered IOObj the VM uses in place of real file descriptors.
func defIOReopen(cls *RClass) {
	// IO#reopen(other_io) / #reopen(path, mode = nil, **opts) -> self
	//
	// io.c rb_io_reopen: with exactly one positional argument that converts to an
	// IO (rb_io_check_io, i.e. #to_io), the receiver adopts that stream
	// (io_reopen); otherwise the argument is a path (FilePathValue, i.e. #to_path)
	// and the receiver is re-bound to a freshly opened stream on it. Either way the
	// receiver object itself is mutated — #object_id is unchanged — and self is
	// returned.
	cls.define("reopen", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		pos, opts := splitIOOpts(args)
		if len(pos) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		// rb_scan_args(argc, argv, "11:") reports one argument only when no second
		// positional mode was given; the IO form is tried only then.
		if len(pos) == 1 {
			if other, ok := ioCheckIO(vm, pos[0]); ok {
				return ioReopenIO(vm, o, other)
			}
		}
		return ioReopenPath(vm, o, pos, opts)
	})

	// IO#fcntl(cmd, arg = 0) — io.c rb_io_fcntl. rbgo has no real descriptors, so
	// the answer is derived from the stream's own recorded state rather than from
	// the host: F_GETFL reports the access mode and O_APPEND the stream was opened
	// with, and F_GETFD reports FD_CLOEXEC from the close-on-exec flag. The two
	// setters are accepted and ignored (there is no descriptor to change), and any
	// other command is refused the way MRI refuses an unsupported one.
	cls.define("fcntl", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.closed {
			raise("IOError", "closed stream")
		}
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		switch intArg(args[0]) {
		case fcntlFGetFD:
			if o.closeOnExecOff {
				return object.IntValue(0)
			}
			return object.IntValue(fdCloexec)
		case fcntlFGetFL:
			return object.IntValue(ioOflags(o))
		case fcntlFSetFD, fcntlFSetFL:
			return object.IntValue(0)
		}
		raise("Errno::EINVAL", "Invalid argument - fcntl(2)")
		return object.NilV
	})
}

// fcntl(2) command numbers and the FD_CLOEXEC descriptor flag. As with the
// File::Constants open flags (fO_RDONLY and friends), the canonical POSIX values
// are fixed here so behaviour does not drift with the host's <fcntl.h>.
const (
	fcntlFGetFD = 1
	fcntlFSetFD = 2
	fcntlFGetFL = 3
	fcntlFSetFL = 4
	fdCloexec   = 1
)

// ioOflags rebuilds the open-flag set (the answer fcntl(F_GETFL) gives) from a
// stream's recorded access mode: io.c rb_io_fmode_oflags maps FMODE_READWRITE to
// O_RDWR, a write-only mode to O_WRONLY and FMODE_APPEND to O_APPEND. O_CREAT and
// O_TRUNC are deliberately absent — they act at open(2) time and a real
// fcntl(F_GETFL) never reports them.
func ioOflags(o *IOObj) int64 {
	var fl int64
	switch {
	case !o.rdClosed && !o.wrClosed:
		fl = fO_RDWR
	case o.rdClosed:
		fl = fO_WRONLY
	default:
		fl = fO_RDONLY
	}
	if o.appendMode {
		fl |= fO_APPEND
	}
	return fl
}

// ioCheckIO converts v to an IO the way io.c's rb_io_check_io does
// (rb_check_convert_type_with_id to T_FILE through #to_io): an IO is itself; an
// object with #to_io is converted, and a conversion that does not yield an IO is a
// TypeError; anything else reports false so the caller can treat v as a path.
func ioCheckIO(vm *VM, v object.Value) (*IOObj, bool) {
	if o, ok := v.(*IOObj); ok {
		return o, true
	}
	if !vm.respondsToDynamic(v, "to_io") {
		return nil, false
	}
	r := vm.send(v, "to_io", nil, nil)
	if object.IsNil(r) {
		return nil, false
	}
	o, ok := r.(*IOObj)
	if !ok {
		raise("TypeError", "can't convert %s to IO (%s#to_io gives %s)",
			vm.classOf(v).name, vm.classOf(v).name, vm.classOf(r).name)
	}
	return o, true
}

// ioGetIO converts v to an IO the way io.c's rb_io_get_io does
// (rb_convert_type_with_id to T_FILE through #to_io) — the strict sibling of
// ioCheckIO: an object with no #to_io is a TypeError rather than a "not an IO"
// answer. IO.select takes every element of its argument arrays through this.
func ioGetIO(vm *VM, v object.Value) *IOObj {
	if o, ok := v.(*IOObj); ok {
		return o
	}
	if !vm.respondsToDynamic(v, "to_io") {
		raise("TypeError", "no implicit conversion of %s into IO", vm.classOf(v).name)
	}
	r := vm.send(v, "to_io", nil, nil)
	o, ok := r.(*IOObj)
	if !ok {
		raise("TypeError", "can't convert %s to IO (%s#to_io gives %s)",
			vm.classOf(v).name, vm.classOf(v).name, vm.classOf(r).name)
	}
	return o
}

// ioReopenIO re-binds o onto other's stream — io.c io_reopen, which dup2()s
// other's descriptor over o's and copies the mode, encodings, pid, lineno and
// path across, then sets o's class to other's. Both streams must be open.
//
// A standard stream keeps its identity instead of adopting the target wholesale:
// MRI dup2()s onto descriptor 0/1/2 and leaves the FILE* in place, so STDOUT stays
// STDOUT and only its destination moves. That is also the shape the rest of this
// VM relies on — Kernel#fork snapshots and restores exactly this redirection
// around a forked block (spawn.go runForkBlock), which a wholesale copy could not
// express.
func ioReopenIO(vm *VM, o, other *IOObj) object.Value {
	if o.closed {
		raise("IOError", "closed stream")
	}
	if other.closed {
		raise("IOError", "closed stream")
	}
	if o == other {
		return o
	}
	// Buffered writes reach the old destination before the descriptor moves
	// (io.c calls io_fflush on both streams first).
	ioFlush(o)
	ioFlush(other)
	// close-on-exec is always set afresh on the reopened stream, whatever either
	// side carried (rb_io_reopen ends with rb_fd_fix_cloexec on the new fd).
	o.closeOnExecOff = false
	switch o.label {
	case "STDIN", "STDOUT", "STDERR":
		o.reopened = other
		return o
	}
	o.buf, o.pos, o.lineno = other.buf, other.pos, other.lineno
	o.rdClosed, o.wrClosed, o.writable = other.rdClosed, other.wrClosed, other.writable
	o.appendMode, o.openMode, o.binmode = other.appendMode, other.openMode, other.binmode
	o.extEnc, o.intEnc = other.extEnc, other.intEnc
	o.isStr, o.w, o.pipe, o.isWriteEnd = other.isStr, other.w, other.pipe, other.isWriteEnd
	o.pipeSynced, o.reopened = other.pipeSynced, other.reopened
	// "if (RTEST(orig->pathv)) fptr->pathv = orig->pathv; else … fptr->pathv = Qnil"
	o.path = other.path
	o.cls = other.cls // RBASIC_SET_CLASS(io, rb_obj_class(nfile))
	return o
}

// ioReopenPath re-binds o onto a freshly opened stream on the given path — the
// freopen() half of io.c rb_io_reopen. With no mode argument the stream keeps the
// access mode it was opened with ("oflags = rb_io_fmode_oflags(fptr->mode)"), so
// a writable stream re-creates (and truncates) the new file while a read-only one
// raises Errno::ENOENT for a path that does not exist. A closed stream reopens.
func ioReopenPath(vm *VM, o *IOObj, pos []object.Value, opts *object.Hash) object.Value {
	name := pathArg(vm, pos[0])
	mode := o.openMode
	if mode == "" {
		mode = "r"
	}
	explicit := (len(pos) > 1 && !object.IsNil(pos[1])) || opts != nil
	if explicit {
		if len(pos) > 1 && !object.IsNil(pos[1]) {
			mode = vm.vmodeString(pos[1])
		} else if m, ok := opts.Get(object.Symbol("mode")); ok && !object.IsNil(m) {
			mode = vm.vmodeString(m)
		}
	}
	// The old destination receives whatever is still buffered before the stream
	// moves (io.c io_fflush), and the read buffer is dropped (rbuf.off = len = 0).
	if !o.closed {
		ioFlush(o)
	}
	fresh := openFileIO(o.cls, name, modeBase(mode))
	o.buf, o.pos, o.path = fresh.buf, fresh.pos, fresh.path
	o.rdClosed, o.wrClosed, o.writable = fresh.rdClosed, fresh.wrClosed, fresh.writable
	o.appendMode, o.openMode = fresh.appendMode, fresh.openMode
	o.isStr, o.closed, o.lineno = true, false, 0
	o.w, o.pipe, o.isWriteEnd, o.pipeSynced, o.reopened = nil, nil, false, 0, nil
	o.closeOnExecOff = false
	if explicit {
		ms := vm.ioResolveModeEnc(pos, opts)
		o.extEnc, o.intEnc, o.binmode = ms.extEnc, ms.intEnc, ms.binmode
	}
	return o
}

// The fcntl and io/nonblock standard-library extensions are supplied by the VM
// rather than by a Ruby file, so `require "fcntl"` / `require "io/nonblock"` must
// succeed without finding one. Registering them here (rather than in require.go's
// table) keeps each feature beside the IO surface that implements it.
func init() {
	providedFeatures["fcntl"] = true
	providedFeatures["io/nonblock"] = true
}

// installFcntl creates the Fcntl module — ext/fcntl/fcntl.c, which defines
// nothing but constants. Only the commands IO#fcntl above actually answers are
// defined (with FD_CLOEXEC, the flag F_GETFD reports), together with the open
// flags, whose values mirror File::Constants so `io.fcntl(Fcntl::F_GETFL) &
// File::APPEND` compares like with like.
func (vm *VM) installFcntl() {
	mod := newClass("Fcntl", nil)
	mod.isModule = true
	vm.consts["Fcntl"] = mod
	for name, val := range map[string]int64{
		"F_GETFD": fcntlFGetFD, "F_SETFD": fcntlFSetFD,
		"F_GETFL": fcntlFGetFL, "F_SETFL": fcntlFSetFL,
		"FD_CLOEXEC": fdCloexec,
	} {
		mod.consts[name] = object.IntValue(val)
	}
	for name, val := range fileFlagConsts {
		mod.consts["O_"+name] = object.IntValue(val)
	}
	for name, val := range fileExtraConsts {
		if strings.HasPrefix(name, "LOCK_") {
			continue // flock(2) operations are File::Constants only, not Fcntl
		}
		mod.consts["O_"+name] = object.IntValue(val)
	}
}

// installIONonblock adds IO#nonblock? / #nonblock= — ext/io/nonblock/nonblock.c,
// which reads and writes O_NONBLOCK on the descriptor with fcntl(2). rbgo has no
// descriptors, so the flag lives on the stream: a file starts blocking and a pipe
// end starts non-blocking, which is what MRI 4.0.5 reports on this host
// ([false, true, true] for a File and the two ends of IO.pipe). The accessors are
// installed on the first `require "io/nonblock"`, as MRI installs them.
func installIONonblock(cls *RClass) {
	cls.define("nonblock?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*IOObj).nonblock)
	})
	cls.define("nonblock=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*IOObj).nonblock = args[0].Truthy()
		return args[0]
	})
}
