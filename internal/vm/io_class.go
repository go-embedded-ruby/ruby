// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerIOClassMethods installs the singleton read/write helpers shared by IO
// and File — IO.read/binread/write/binwrite/foreach/readlines and the identical
// File.* forms. It runs from registerIO, after both classes exist, and assigns
// the same natives to both class-method tables so File and IO behave alike.
func (vm *VM) registerIOClassMethods(cIO, cFile *RClass) {
	def := func(name string, fn NativeFn) {
		m := &Method{name: name, owner: cIO, native: fn}
		cIO.smethods[name] = m
		cFile.smethods[name] = m
	}
	// IO.new(fd, mode = "r", **opts) / IO.for_fd wrap an existing descriptor. rbgo
	// has no real fds, so the descriptor is looked up in the synthetic fd table
	// (populated by #fileno); an unknown one raises Errno::EBADF. The result is a
	// fresh IO carrying the mode's read/write intent and encoding.
	forFd := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pos, opts := splitIOOpts(args)
		if len(pos) < 1 || len(pos) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(pos))
		}
		return vm.ioAdoptFd(cIO, int(vm.repeatLong(pos[0])), pos, opts)
	}
	cIO.smethods["for_fd"] = &Method{name: "for_fd", owner: cIO, native: forFd}
	cIO.smethods["new"] = &Method{name: "new", owner: cIO, native: forFd}
	// IO.open(fd, mode = "r", **opts) is IO.new plus a block: it yields the wrapped
	// stream then, in an ensure, closes it — via the Ruby-level #close so an
	// overridden #close runs (io.c rb_io_s_open + io_close). The block's value is
	// returned. A #close that raises propagates, EXCEPT an IOError whose message is
	// "closed stream", which is swallowed (and leaves no last error); when both the
	// block and #close raise, the block's exception is the one that propagates.
	cIO.smethods["open"] = &Method{name: "open", owner: cIO, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		oVal := forFd(vm, self, args, nil)
		o, ok := oVal.(*IOObj)
		if !ok || blk == nil {
			return oVal
		}
		return vm.ioYieldAndClose(o, blk)
	}}
	// IO.sysopen(path, mode = "r", perm = 0666) opens path and returns its raw
	// descriptor (io.c rb_io_s_sysopen → rb_sysopen). rbgo has no OS fds, so the
	// file is opened into a buffered stream, registered in the synthetic fd table
	// (so IO.for_fd(fd) can wrap it), and its synthetic descriptor returned. The
	// path is coerced with #to_path; the permission argument is accepted but has
	// no effect on the in-memory model.
	cIO.smethods["sysopen"] = &Method{name: "sysopen", owner: cIO, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 || len(args) > 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..3)", len(args))
		}
		path := pathArg(vm, args[0])
		mode := "r"
		if len(args) >= 2 && !object.IsNil(args[1]) {
			mode = vm.vmodeString(args[1]) // Integer flag set or fopen-style String
		}
		o := openFileIO(cFile, path, mode)
		vm.nextFd++
		o.fd = vm.nextFd
		vm.fdTable[o.fd] = o
		return object.IntValue(int64(o.fd))
	}}
	def("read", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.ioReadFile(args, false)
	})
	def("binread", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.ioReadFile(args, true)
	})
	def("write", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.ioWriteFile(args)
	})
	def("binwrite", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.ioWriteFile(args)
	})
	// IO.copy_stream(src, dst, copy_length = nil, src_offset = nil): copy bytes from
	// src to dst, each of which may be a path String or an IO. Returns the number
	// of bytes copied. src_offset is honoured for a path but rejected for a
	// (non-fd) IO such as StringIO, matching MRI.
	def("copy_stream", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..4)", len(args))
		}
		length, hasLen := -1, false
		if len(args) >= 3 && !object.IsNil(args[2]) {
			length, hasLen = int(intArg(args[2])), true
		}
		srcOffset, hasOff := -1, false
		if len(args) >= 4 && !object.IsNil(args[3]) {
			srcOffset, hasOff = int(intArg(args[3])), true
		}
		// A zero-length copy transfers nothing and touches neither object — MRI
		// never calls #read on the source nor #write on the destination.
		if hasLen && length == 0 {
			return object.IntValue(0)
		}
		data := vm.copyStreamRead(args[0], length, hasLen, srcOffset, hasOff)
		vm.copyStreamWrite(args[1], data)
		return object.IntValue(int64(len(data)))
	})
	def("foreach", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		pos, opts := splitIOOpts(args)
		if len(pos) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		if blk == nil {
			// No block ⇒ an Enumerator whose #size is nil (the number of lines is
			// unknown without reading the file), re-dispatching IO.foreach with a
			// block when iterated. io.c: rb_io_s_foreach returns enum_for(:foreach).
			return enumForSized(self, "foreach", enumSizeNil, args...)
		}
		o := vm.ioOpenForeach(pos[0], opts)
		sep, limit, chomp := vm.resolveGetsArgs(pos[1:])
		chomp = optChomp(opts, chomp)
		checkResolvedLimit(limit, "foreach")
		for v := vm.ioGetsResolved(o, sep, limit, chomp); v != object.NilV; v = vm.ioGetsResolved(o, sep, limit, chomp) {
			// Each yield updates $. to the current line number, as MRI's gets-based
			// foreach loop does; $_ is not the per-line variable here.
			vm.globals["$."] = object.IntValue(int64(o.lineno))
			vm.callBlock(blk, []object.Value{v})
		}
		// Reading past the last line clears $_ (io.c rb_io_getline at EOF).
		vm.globals["$_"] = object.NilV
		return object.NilV
	})
	def("readlines", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pos, opts := splitIOOpts(args)
		if len(pos) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		o := vm.ioOpenForeach(pos[0], opts)
		sep, limit, chomp := vm.resolveGetsArgs(pos[1:])
		chomp = optChomp(opts, chomp)
		checkResolvedLimit(limit, "readlines")
		var lines []object.Value
		for v := vm.ioGetsResolved(o, sep, limit, chomp); v != object.NilV; v = vm.ioGetsResolved(o, sep, limit, chomp) {
			lines = append(lines, v)
		}
		return object.NewArrayFromSlice(lines)
	})
}

// ioOpenForeach opens the file IO.foreach / IO.readlines will read — io.c
// open_key_args, which does NOT force O_RDONLY when options are given: an
// :open_args Array supplies the mode (superseding the sibling options), otherwise
// the :mode option does, and only a call with no options at all defaults to
// O_RDONLY. Opening with a write mode therefore creates the file and the read
// that follows raises IOError, where forcing "r" raised Errno::ENOENT instead.
// rb_io_getline_0 opens with rb_io_check_char_readable, so the check is made here
// — nothing happens between the open and the first line.
func (vm *VM) ioOpenForeach(path object.Value, opts *object.Hash) *IOObj {
	cFile := vm.consts["File"].(*RClass)
	o := openFileIO(cFile, pathArg(vm, path), modeBase(ioForeachMode(opts)))
	// open_key_args reaches rb_io_open, so the stream is subject to the same
	// opening `rb_io_ext_int_to_encs(NULL, NULL, …)` as File.open: the lines
	// IO.readlines hands back are transcoded to Encoding.default_internal when one
	// is set, not merely tagged with default_external.
	o.extEnc, o.intEnc = vm.ioExtIntToEncs("", "", encUnset)
	ioCheckReadable(o)
	return o
}

// ioForeachMode returns the access mode IO.foreach / IO.readlines open with: the
// mode inside :open_args if that option is present, else the :mode option, else
// "r" (io.c open_key_args ⇒ rb_io_open ⇒ rb_io_extract_modeenc).
func ioForeachMode(opts *object.Hash) string {
	if opts == nil {
		return "r"
	}
	if oa, ok := opts.Get(object.Symbol("open_args")); ok {
		if m := ioOpenArgsMode(oa); m != "" {
			return m
		}
		return "r"
	}
	if m := ioOptMode(opts); m != "" {
		return m
	}
	return "r"
}

// ioAdoptDescriptor makes o wrap the descriptor src under the mode/encoding
// decoded from (pos, opts): the shared buffer/path, the read/write access half
// (an explicit mode incompatible with the descriptor is EINVAL, no mode inherits
// the descriptor's), the append position, and IO.new's optional path: override
// (an explicit nil clears the inherited path). Shared by IO.for_fd/new and
// IO#initialize so the two decode a descriptor identically (io.c io_initialize).
// ioYieldAndClose is the block form of IO.open and File.open — io.c rb_io_s_open
// with io_close in its ensure. The stream is closed through the RUBY-level
// #close, so a subclass that overrides it runs.
//
// A #close that raises propagates, EXCEPT an IOError "closed stream", which is
// what a block that closed the file itself leaves behind. When BOTH the block
// and #close raise, the one from #close is the one that escapes: it is raised
// from an ensure, and an ensure's exception supersedes the one it was unwinding.
// Measured on MRI 4.0.5 for File.open, for IO.open and for a bare
// begin/raise/ensure/raise alike — all three answer with the ensure's.
func (vm *VM) ioYieldAndClose(o *IOObj, blk *Proc) object.Value {
	var ret object.Value
	blockRec := recoverAny(func() { ret = vm.callBlock(blk, []object.Value{o}) })
	closeRec := recoverAny(func() { vm.send(o, "close", nil, nil) })
	if re, isRE := closeRec.(RubyError); isRE && re.Class == "IOError" && re.Message == "closed stream" {
		closeRec = nil
	}
	if closeRec != nil {
		panic(closeRec)
	}
	if blockRec != nil {
		panic(blockRec)
	}
	return ret
}

// ioFdArg is rb_check_to_int on a File.open/File.new first argument: an Integer,
// or an object that converts to one with #to_int, is a descriptor. Everything
// else — a String, a Pathname, anything answering only #to_path — is a path, and
// the bool says so rather than raising, because the caller has a path to try.
func (vm *VM) ioFdArg(v object.Value) (int, bool) {
	if i, ok := v.(object.Integer); ok {
		return int(i), true
	}
	if vm.respondsToDynamic(v, "to_int") {
		return int(vm.repeatLong(v)), true
	}
	return 0, false
}

// ioAdoptFd builds a stream of class cls over the descriptor fd — io_initialize,
// which IO.new, IO.for_fd and the File.new(fd) form all reach. rbgo has no real
// descriptors (see ioFd), so fd is looked up in the synthetic table #fileno
// fills; one that is not there is the EBADF an unknown descriptor gives.
func (vm *VM) ioAdoptFd(cls *RClass, fd int, pos []object.Value, opts *object.Hash) *IOObj {
	src, ok := vm.fdTable[fd]
	if !ok {
		raise("Errno::EBADF", "Bad file descriptor - fd %d", fd)
	}
	if src.closed {
		raise("IOError", "closed stream")
	}
	res := &IOObj{cls: cls}
	vm.ioAdoptDescriptor(res, src, pos, opts)
	// The wrapper answers #fileno with the descriptor it wraps, as dup-free
	// io_initialize does — File.open(f.fileno).fileno == f.fileno.
	res.fd = fd
	return res
}

func (vm *VM) ioAdoptDescriptor(o, src *IOObj, pos []object.Value, opts *object.Hash) {
	ms := vm.ioResolveModeEnc(pos, opts)
	o.isStr, o.buf, o.path = true, src.buf, src.path
	o.binmode, o.noAutoclose = ms.binmode, ms.noAutoclose
	o.extEnc, o.intEnc, o.newline = ms.extEnc, ms.intEnc, ms.newline
	if ms.explicit {
		// An explicit mode must be compatible with the descriptor's current mode
		// (io.c io_reopen / rb_update_max_fd path: EINVAL when e.g. a write-only fd
		// is opened for reading).
		if (ms.readable && src.rdClosed) || (ms.writable && src.wrClosed) {
			raise("Errno::EINVAL", "Invalid argument")
		}
		o.writable, o.appendMode = ms.writable, ms.appendMode
		o.rdClosed, o.wrClosed = !ms.readable, !ms.writable
	} else {
		// No explicit mode: inherit the descriptor's actual access half (MRI
		// derives it from the fd via fcntl(F_GETFL)).
		o.writable, o.appendMode = src.writable, src.appendMode
		o.rdClosed, o.wrClosed = src.rdClosed, src.wrClosed
	}
	if o.appendMode {
		o.pos = len(o.buf)
	}
	// IO.new(fd, path:) records an explicit path for #path/#inspect (io.c
	// rb_io_extract_modeenc stores the :path option in fptr->pathv). An explicit
	// path: — even nil — overrides the path inherited from the descriptor.
	if opts != nil {
		if pv, ok := opts.Get(object.Symbol("path")); ok {
			if object.IsNil(pv) {
				o.path = ""
			} else {
				o.path = pathArg(vm, pv)
			}
		}
	}
}

// splitIOOpts separates a trailing options Hash (IO.read/write keyword arguments)
// from the positional arguments.
func splitIOOpts(args []object.Value) ([]object.Value, *object.Hash) {
	if n := len(args); n > 0 {
		if h, ok := args[n-1].(*object.Hash); ok {
			return args[:n-1], h
		}
	}
	return args, nil
}

// ioReadFile implements IO.read / File.read (forceBinary false) and
// IO.binread / File.binread (forceBinary true): read `name`, honouring an optional
// byte length and offset and a trailing options Hash. A length beyond EOF yields
// nil; a passed length (or binread) tags the result ASCII-8BIT unless an explicit
// :encoding / :external_encoding option overrides it.
func (vm *VM) ioReadFile(args []object.Value, forceBinary bool) object.Value {
	pos, opts := splitIOOpts(args)
	if len(pos) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..3)")
	}
	name := pathArg(vm, pos[0])
	length, hasLen := int64(0), false
	if len(pos) > 1 && pos[1] != object.NilV {
		length, hasLen = intArg(pos[1]), true
		if length < 0 {
			raise("ArgumentError", "negative length %d given", length)
		}
	}
	var offset int64
	if len(pos) > 2 && pos[2] != object.NilV {
		if offset = intArg(pos[2]); offset < 0 {
			// IO.binread reaches a negative offset through rb_io_seek (Errno::EINVAL);
			// IO.read validates it in Ruby and raises ArgumentError.
			if forceBinary {
				raise("Errno::EINVAL", "Invalid argument @ rb_io_seek - %s", name)
			}
			raise("ArgumentError", "negative offset %d given", offset)
		}
	}
	enc, mode := "", ""
	if opts != nil {
		// :open_args, when present, supersedes :mode / :encoding (MRI disregards the
		// sibling options); its own mode string, if any, still gates readability.
		if oa, ok := opts.Get(object.Symbol("open_args")); ok {
			mode = ioOpenArgsMode(oa)
			if h := openArgsHash(oa); h != nil {
				if v, ok := encOpt(h); ok {
					enc = vm.encodingName(v)
				}
			}
		} else {
			mode = ioOptMode(opts)
			enc = vm.ioOptEncoding(opts)
		}
		if mode != "" && !ioModeReadable(mode) {
			raise("IOError", "not opened for reading")
		}
	}
	data, err := os.ReadFile(name)
	if err != nil {
		raiseOpenErr(name, err)
	}
	if hasLen && offset >= int64(len(data)) && length > 0 {
		return object.NilV // a length read starting at or past EOF yields nil
	}
	start := offset
	if start > int64(len(data)) {
		start = int64(len(data))
	}
	out := data[start:]
	// A "BOM|enc" external encoding (mode "rb:BOM|utf-8") strips a leading BOM from
	// the file's start when one is present (io.c: the BOM overrides the named
	// encoding). Only a read from offset 0 can see the BOM.
	if start == 0 && modeHasBOM(mode) {
		if n, bomEnc := detectBOM(out); bomEnc != "" {
			out = out[n:]
		}
	}
	if hasLen && int64(len(out)) > length {
		out = out[:length]
	}
	if enc == "" && (forceBinary || hasLen) {
		enc = "ASCII-8BIT"
	}
	b := append([]byte(nil), out...)
	if enc != "" {
		return object.NewStringBytesEnc(b, enc)
	}
	return object.NewStringBytes(b)
}

// copyStreamRead reads the bytes IO.copy_stream should transfer from its source
// argument: a real IO (File/pipe/StringIO), a path String or #to_path object, or
// any object answering #readpartial/#read. io.c copy_stream:
//   - an IO source with a src_offset is pread at that offset (only a descriptor-
//     backed stream — a File — can; a StringIO raises "cannot specify src_offset
//     for non-IO") without moving its position; without an offset it reads from
//     the current position, advancing it;
//   - a path source is read with the length/offset directly;
//   - a bare object is drained through #readpartial (preferred) or #read(len, buf).
func (vm *VM) copyStreamRead(src object.Value, length int, hasLen bool, srcOffset int, hasOff bool) []byte {
	if o, ok := src.(*IOObj); ok {
		if hasOff {
			// copy_stream_fallback raises the ArgumentError only when the source has
			// NO fptr — a StringIO, or any duck-typed object. A real IO always has
			// one and reaches maygvl_copy_stream_read, which preads: on a pipe,
			// socket or tty that fails with ESPIPE, reported as syserr "pread".
			// The two are not interchangeable — core/io/copy_stream_spec.rb asks a
			// pipe source for an offset and expects Errno::ESPIPE.
			switch {
			case ioIsStringIO(o):
				raise("ArgumentError", "cannot specify src_offset for non-IO")
			case o.path == "":
				raise("Errno::ESPIPE", "Illegal seek - pread")
			}
			ioCheckReadable(o)
			o.pipeRefresh()
			start := srcOffset
			if start > len(o.buf) {
				start = len(o.buf)
			}
			end := len(o.buf)
			if hasLen && start+length < end {
				end = start + length
			}
			return append([]byte(nil), o.buf[start:end]...)
		}
		var ra []object.Value
		if hasLen {
			ra = append(ra, object.IntValue(int64(length)))
		}
		return strBytesOrEmpty(vm.send(o, "read", ra, nil))
	}
	if _, isStr := src.(*object.String); isStr || vm.respondsToDynamic(src, "to_path") {
		ra := []object.Value{object.NewString(pathArg(vm, src))}
		if hasLen || hasOff {
			if hasLen {
				ra = append(ra, object.IntValue(int64(length)))
			} else {
				ra = append(ra, object.NilV)
			}
			if hasOff {
				ra = append(ra, object.IntValue(int64(srcOffset)))
			}
		}
		return vm.ioReadFile(ra, true).(*object.String).Bytes()
	}
	if hasOff {
		raise("ArgumentError", "cannot specify src_offset for non-IO")
	}
	return vm.copyStreamReadObject(src, length, hasLen)
}

// copyStreamReadObject drains a duck-typed source that is neither an IO nor a
// path: MRI calls #readpartial(len, buf) when the object defines it, otherwise
// #read(len, buf), looping until EOF (a nil return, a short read, or the EOFError
// #readpartial raises).
func (vm *VM) copyStreamReadObject(src object.Value, length int, hasLen bool) []byte {
	const chunk = 16384
	usePartial := vm.respondsToDynamic(src, "readpartial")
	want := chunk
	var out []byte
	for {
		if hasLen {
			if remaining := length - len(out); remaining <= 0 {
				break
			} else if remaining < want {
				want = remaining
			}
		}
		buf := object.NewStringBytesEnc(nil, "ASCII-8BIT")
		var r object.Value
		stop := false
		if usePartial {
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						// #readpartial signals end-of-input by raising EOFError.
						if e, ok := rec.(RubyError); ok && e.Class == "EOFError" {
							stop = true
							return
						}
						panic(rec)
					}
				}()
				r = vm.send(src, "readpartial", []object.Value{object.IntValue(int64(want)), buf}, nil)
			}()
		} else {
			r = vm.send(src, "read", []object.Value{object.IntValue(int64(want)), buf}, nil)
		}
		if stop || object.IsNil(r) {
			break
		}
		b := strBytesOrEmpty(r)
		out = append(out, b...)
		if !usePartial && len(b) < want { // #read returns a short/empty read at EOF
			break
		}
		if len(b) == 0 {
			break
		}
	}
	return out
}

// copyStreamWrite writes the copied bytes to IO.copy_stream's destination: a real
// IO (its buffer is flushed so a File destination reaches disk immediately, as
// copy_stream's descriptor-level write does), a path String / #to_path object
// (truncating write), or any object answering #write.
func (vm *VM) copyStreamWrite(dst object.Value, data []byte) {
	s := object.NewStringBytesEnc(append([]byte(nil), data...), "ASCII-8BIT")
	if o, ok := dst.(*IOObj); ok {
		vm.send(o, "write", []object.Value{s}, nil)
		vm.send(o, "flush", nil, nil)
		return
	}
	if _, isStr := dst.(*object.String); isStr || vm.respondsToDynamic(dst, "to_path") {
		vm.ioWriteFile([]object.Value{object.NewString(pathArg(vm, dst)), s})
		return
	}
	vm.send(dst, "write", []object.Value{s}, nil)
}

// strBytesOrEmpty returns v's bytes when it is a String, else an empty slice
// (a nil read at EOF).
func strBytesOrEmpty(v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	return nil
}

// ioWriteFile implements IO.write / File.write / IO.binwrite / File.binwrite:
// write the (to_s-coerced) string to `name`. With no offset the file is truncated
// ("w"); with an offset the file is created if missing and the bytes are written
// at that offset without truncating. A :mode / :open_args option selects the mode
// (a read-only mode raises IOError); :perm sets the create permissions. Returns
// the number of bytes written.
func (vm *VM) ioWriteFile(args []object.Value) object.Value {
	pos, opts := splitIOOpts(args)
	if len(pos) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(pos))
	}
	name := pathArg(vm, pos[0])
	var data []byte
	if s, ok := pos[1].(*object.String); ok {
		data = s.Bytes()
	} else {
		data = []byte(vm.displayStr(pos[1]))
	}
	offset, hasOffset := int64(0), false
	if len(pos) > 2 && pos[2] != object.NilV {
		offset, hasOffset = intArg(pos[2]), true
	}
	mode := ""
	perm := os.FileMode(0o644)
	if opts != nil {
		mode = ioOptMode(opts)
		if oa, ok := opts.Get(object.Symbol("open_args")); ok {
			mode = ioOpenArgsMode(oa)
			if mode == "" {
				raise("IOError", "not opened for writing")
			}
		} else if _, hasEnc := encOpt(opts); hasEnc && strings.Contains(mode, ":") {
			raise("ArgumentError", "encoding specified twice")
		}
		if v, ok := opts.Get(object.Symbol("perm")); ok {
			perm = os.FileMode(intArg(v) & 0o7777)
		}
	}
	if mode != "" && !ioModeWritable(mode) {
		raise("IOError", "not opened for writing")
	}
	n := ioPutBytes(name, data, mode, offset, hasOffset, perm)
	return object.IntValue(int64(n))
}

// ioPutBytes writes data to name per the resolved mode/offset, returning the byte
// count. It composes the final file content in memory and writes it once: "a"
// appends to the existing content; a non-truncating write preserves the existing
// content and overwrites (extending with NUL padding) at the offset; a truncating
// write replaces the whole file. Perm applies only when the file is created.
func ioPutBytes(name string, data []byte, mode string, offset int64, hasOffset bool, perm os.FileMode) int {
	// With no explicit mode, an offset selects in-place (non-truncating) writing;
	// without an offset the default is truncating ("w"). An explicit "w" always
	// truncates, even when an offset is also given.
	trunc := strings.HasPrefix(mode, "w") || (mode == "" && !hasOffset)
	var buf []byte
	switch {
	case strings.HasPrefix(mode, "a"): // append: continue past the existing content
		buf, _ = os.ReadFile(name)
		offset, hasOffset = int64(len(buf)), true
	case !trunc: // in-place: keep the existing bytes, overwrite at the offset
		buf, _ = os.ReadFile(name)
	}
	if hasOffset {
		if end := offset + int64(len(data)); end > int64(len(buf)) {
			grown := make([]byte, end)
			copy(grown, buf)
			buf = grown
		}
		copy(buf[offset:], data)
	} else {
		buf = data
	}
	if err := os.WriteFile(name, buf, perm); err != nil {
		raiseOpenErr(name, err)
	}
	return len(data)
}

// openArgsHash returns the Hash element of a :open_args Array (the options
// sub-hash), or nil when the array has none.
func openArgsHash(oa object.Value) *object.Hash {
	if arr, ok := oa.(*object.Array); ok {
		for _, e := range arr.Elems {
			if h, ok := e.(*object.Hash); ok {
				return h
			}
		}
	}
	return nil
}

// raiseOpenErr maps a failed open of name to the MRI errno: Errno::EISDIR when the
// path is a directory, otherwise Errno::ENOENT.
func raiseOpenErr(name string, _ error) {
	if fi, err := os.Stat(name); err == nil && fi.IsDir() {
		raise("Errno::EISDIR", "Is a directory @ rb_sysopen - %s", name)
	}
	raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", name)
}

// ioOptMode returns the string :mode option, or "".
func ioOptMode(opts *object.Hash) string {
	if v, ok := opts.Get(object.Symbol("mode")); ok {
		if s, ok := v.(*object.String); ok {
			return s.Str()
		}
	}
	return ""
}

// ioOpenArgsMode extracts the access-mode string from a :open_args Array: the
// first String element, or a :mode entry in a trailing/leading Hash element.
// Returns "" when the array specifies no mode (which callers treat as an error).
func ioOpenArgsMode(oa object.Value) string {
	arr, ok := oa.(*object.Array)
	if !ok {
		return ""
	}
	for _, e := range arr.Elems {
		switch v := e.(type) {
		case *object.String:
			return v.Str()
		case *object.Hash:
			if m, ok := v.Get(object.Symbol("mode")); ok {
				if s, ok := m.(*object.String); ok {
					return s.Str()
				}
			}
		}
	}
	return ""
}

// ioModeReadable reports whether an fopen-style mode string permits reading
// (anything but a bare write/append mode without "+").
func ioModeReadable(mode string) bool {
	base := modeBase(mode)
	return strings.HasPrefix(base, "r") || strings.Contains(base, "+")
}

// ioModeWritable reports whether an fopen-style mode string permits writing (any
// mode but a bare "r" without "+").
func ioModeWritable(mode string) bool {
	base := modeBase(mode)
	return !strings.HasPrefix(base, "r") || strings.Contains(base, "+")
}

// modeHasBOM reports whether a mode string's encoding part carries the "BOM|"
// prefix (e.g. "rb:BOM|utf-8"), which asks IO.read/File.read to strip and honour
// a leading byte-order mark.
func modeHasBOM(mode string) bool {
	if i := strings.IndexByte(mode, ':'); i >= 0 {
		return strings.HasPrefix(strings.ToUpper(mode[i+1:]), "BOM|")
	}
	return false
}

// modeBase strips a ":enc" encoding suffix (e.g. "w:UTF-16LE") from a mode string.
func modeBase(mode string) string {
	if i := strings.IndexByte(mode, ':'); i >= 0 {
		return mode[:i]
	}
	return mode
}

// encOpt returns the :encoding or :external_encoding option value, if present.
func encOpt(opts *object.Hash) (object.Value, bool) {
	if v, ok := opts.Get(object.Symbol("encoding")); ok {
		return v, true
	}
	if v, ok := opts.Get(object.Symbol("external_encoding")); ok {
		return v, true
	}
	return nil, false
}

// ioModeSpec is the decoded access mode + encoding of an IO.new/IO.open/IO.for_fd
// request: the read/write/append/binmode intent plus the resolved external and
// internal encoding names ("" ⇒ default/none). It mirrors MRI's
// rb_io_extract_modeenc bookkeeping (io.c).
type ioModeSpec struct {
	readable, writable, appendMode, binmode, textmode, noAutoclose bool
	extEnc, intEnc                                                 string
	hasEnc                                                         bool // the mode STRING carried a ":enc" suffix
	explicit                                                       bool // a mode argument (or :mode option) was given

	// encResolved records that extEnc/intEnc have already been through
	// rb_io_ext_int_to_encs. MRI runs that resolution FIRST, from
	// rb_io_extract_modeenc's opening `rb_io_ext_int_to_encs(NULL, NULL, …)`, and
	// lets a mode suffix or an :encoding option replace the result; rbgo fills the
	// fields only when something names an encoding, so the defaulted resolution is
	// applied at the END instead — but only if nothing else claimed them. Without
	// the flag an `internal_encoding: nil` (which legitimately resolves to the
	// empty pair) would be indistinguishable from "nobody said anything".
	encResolved bool

	// create, trunc and excl are the rest of MRI's fmode: FMODE_CREATE,
	// FMODE_TRUNC and FMODE_EXCL. A wrapper around an existing descriptor has no
	// use for them, but File.open's open(2) does — "w" is O_WRONLY|O_CREAT|O_TRUNC
	// and "wx" adds O_EXCL, which is the whole of what the 'x' flag means.
	create, trunc, excl bool

	// newline is the :newline option's decorator, "" when none was asked for. MRI
	// turns it into an ECONV_*_NEWLINE_DECORATOR bit and then refuses the
	// combination with binmode (validate_enc_binmode, io.c).
	newline string
}

// oflags is rb_io_fmode_oflags (io.c): the open(2) flag set this fmode asks for.
// It is the form File.open needs, and the form the :flags option merges into.
func (ms *ioModeSpec) oflags() int64 {
	var f int64
	switch {
	case ms.readable && ms.writable:
		f = fO_RDWR
	case ms.writable:
		f = fO_WRONLY
	default:
		f = fO_RDONLY
	}
	if ms.appendMode {
		f |= fO_APPEND
	}
	if ms.trunc {
		f |= fO_TRUNC
	}
	if ms.create {
		f |= fO_CREAT
	}
	if ms.excl {
		f |= fO_EXCL
	}
	return f
}

// ioResolveModeEnc decodes the (fd, mode, **opts) arguments of IO.new / IO.open
// into an ioModeSpec, following io.c rb_io_extract_modeenc: a String or Integer
// mode argument, an overriding :mode option (an error if the positional mode is
// also given), :binmode / :textmode with their "specified twice" / "both …"
// conflicts, and the :encoding / :external_encoding / :internal_encoding options
// (an error when the mode string already named an encoding).
func (vm *VM) ioResolveModeEnc(pos []object.Value, opts *object.Hash) ioModeSpec {
	var ms ioModeSpec
	vmodeGiven := false
	if len(pos) >= 2 && !object.IsNil(pos[1]) {
		vm.resolveVmode(pos[1], &ms)
		vmodeGiven = true
	}
	if opts != nil {
		if m, ok := opts.Get(object.Symbol("mode")); ok && !object.IsNil(m) {
			if vmodeGiven {
				raise("ArgumentError", "mode specified twice")
			}
			vm.resolveVmode(m, &ms)
			vmodeGiven = true
		}
	}
	ms.explicit = vmodeGiven
	if !vmodeGiven {
		ms.readable = true // no explicit mode ⇒ the wrapper inherits the fd's mode
	}
	if opts != nil {
		// rb_io_extract_modeenc reads :flags right after the mode and re-derives
		// the fmode from the merged open flags, so `File.open(p, "w", flags:
		// File::EXCL)` is exactly `File.open(p, File::WRONLY|File::CREAT|
		// File::TRUNC|File::EXCL)`. The encoding fields survive because
		// flagsModeSpec does not touch them — MRI resolves the encoding before this
		// point for the same reason.
		if v, ok := opts.Get(object.Symbol("flags")); ok && !object.IsNil(v) {
			flagsModeSpec(ms.oflags()|vm.repeatLong(v), &ms)
			ms.explicit = true
		}
		extractBinmode(opts, &ms)
		extractNewline(opts, &ms)
	}
	// A binary stream defaults to ASCII-8BIT external encoding unless the mode
	// string already named one (a later :encoding option may still override it).
	if ms.binmode && !ms.hasEnc && ms.extEnc == "" {
		ms.extEnc, ms.intEnc = vm.ioExtIntToEncs("ASCII-8BIT", "", encUnset)
		ms.encResolved = true
	}
	if opts != nil {
		if vm.extractEncodingOption(opts, &ms) && ms.hasEnc {
			raise("ArgumentError", "encoding specified twice")
		}
		if v, ok := opts.Get(object.Symbol("autoclose")); ok {
			ms.noAutoclose = !v.Truthy()
		}
	}
	// rb_io_extract_modeenc opens with `rb_io_ext_int_to_encs(NULL, NULL, &enc,
	// &enc2, 0)`: a stream that names no encoding still SNAPSHOTS the defaults in
	// force at open. That is invisible while Encoding.default_internal is nil
	// (the resolution records nothing and the stream keeps tracking
	// default_external), and decisive once it is set — the stream then reports
	// that internal encoding for the rest of its life, whatever the defaults do
	// afterwards.
	if !ms.encResolved {
		ms.extEnc, ms.intEnc = vm.ioExtIntToEncs("", "", encUnset)
	}
	return ms
}

// resolveVmode decodes a single mode argument (the positional mode or a :mode
// option) into ms: an Integer (via #to_int) is an open-flag set; otherwise the
// value is coerced with #to_str and parsed as an fopen-style mode string.
func (vm *VM) resolveVmode(v object.Value, ms *ioModeSpec) {
	if i, ok := v.(object.Integer); ok {
		flagsModeSpec(int64(i), ms)
		return
	}
	if vm.respondsToDynamic(v, "to_int") {
		flagsModeSpec(vm.repeatLong(v), ms)
		return
	}
	s, ok := v.(*object.String)
	if !ok && vm.respondsToDynamic(v, "to_str") {
		s, ok = vm.send(v, "to_str", nil, nil).(*object.String)
	}
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	}
	vm.parseModeString(s.Str(), ms)
}

// flagsModeSpec is rb_io_oflags_fmode (io.c): it REPLACES the access half of ms
// from an integer open-flag set (a bitwise OR of File::RDONLY/WRONLY/RDWR/APPEND/
// TRUNC/CREAT/EXCL), leaving the encoding fields alone — which is what lets the
// :flags option re-derive the fmode after OR-ing into the flags without losing an
// encoding the mode string already named.
//
// O_APPEND does NOT imply writability here, any more than it does in MRI: the
// access bits alone decide, so File::RDONLY|File::APPEND is a READ-ONLY stream
// and writing to it is an IOError.
func flagsModeSpec(flags int64, ms *ioModeSpec) {
	ms.readable, ms.writable = false, false
	switch flags & 0x3 {
	case fO_WRONLY:
		ms.writable = true
	case fO_RDWR:
		ms.readable, ms.writable = true, true
	default: // fO_RDONLY (and the invalid 0x3, which open(2) rejects)
		ms.readable = true
	}
	ms.appendMode = flags&fO_APPEND != 0
	ms.trunc = flags&fO_TRUNC != 0
	ms.create = flags&fO_CREAT != 0
	ms.excl = flags&fO_EXCL != 0
}

// parseModeString fills ms from an fopen-style mode string ("r"/"w"/"a" with an
// optional "+"/"b"/"t" and a trailing ":ext[:int]" encoding), matching MRI's
// rb_io_modestr_fmode + parse_mode_enc. An empty or malformed base raises
// ArgumentError.
func (vm *VM) parseModeString(mode string, ms *ioModeSpec) {
	base := mode
	if i := strings.IndexByte(mode, ':'); i >= 0 {
		base, ms.hasEnc = mode[:i], true
		vm.parseEncPart(mode[i+1:], ms)
	}
	if base == "" {
		raise("ArgumentError", "invalid access mode %s", mode)
	}
	switch base[0] {
	case 'r':
		ms.readable = true
	case 'w':
		// rb_io_modestr_fmode: 'w' is FMODE_WRITABLE|FMODE_TRUNC|FMODE_CREATE and
		// 'a' is FMODE_WRITABLE|FMODE_APPEND|FMODE_CREATE — the create and truncate
		// halves are part of the letter, not something File.open adds later.
		ms.writable, ms.trunc, ms.create = true, true, true
	case 'a':
		ms.writable, ms.appendMode, ms.create = true, true, true
	default:
		raise("ArgumentError", "invalid access mode %s", mode)
	}
	for _, c := range base[1:] {
		switch c {
		case '+':
			ms.readable, ms.writable = true, true
		case 'b':
			if ms.textmode {
				raise("ArgumentError", "invalid access mode %s", mode)
			}
			ms.binmode = true
		case 't':
			if ms.binmode {
				raise("ArgumentError", "invalid access mode %s", mode)
			}
			ms.textmode = true
		case 'x':
			// FMODE_EXCL, and only on a 'w' base: rb_io_modestr_fmode checks
			// modestr[0] itself, so "rx" and "ax" are an invalid access mode rather
			// than an exclusive open.
			if base[0] != 'w' {
				raise("ArgumentError", "invalid access mode %s", mode)
			}
			ms.excl = true
		default:
			raise("ArgumentError", "invalid access mode %s", mode)
		}
	}
}

// parseEncPart resolves the "ext[:int]" encoding suffix of a mode string into
// ms.extEnc / ms.intEnc. An "-" internal encoding (or one equal to the external)
// leaves the internal encoding unset (no transcoding).
func (vm *VM) parseEncPart(enc string, ms *ioModeSpec) {
	ext, intn := enc, ""
	hasInt := false
	if j := strings.IndexByte(enc, ':'); j >= 0 {
		ext, intn, hasInt = enc[:j], enc[j+1:], true
	}
	extName := ""
	if ext != "" {
		extName = vm.lookupEncodingName(ext).name
	}
	// parse_mode_enc: a trailing ":-" — or an internal name equal to the external
	// one — is Qnil, "no transcoding"; anything else names the internal encoding.
	intName, intState := "", encUnset
	if hasInt {
		intState = encNone
		if intn != "" && intn != "-" {
			if n := vm.lookupEncodingName(intn).name; n != extName {
				intName, intState = n, encNamed
			}
		}
	}
	ms.extEnc, ms.intEnc = vm.ioExtIntToEncs(extName, intName, intState)
	ms.encResolved = true
}

// extractNewline applies the :newline option, which names one of MRI's newline
// decorators. rb_econv_prepare_options refuses anything else by name, and
// validate_enc_binmode (io.c) then refuses any decorator at all on a binary
// stream — there is no newline to translate in bytes.
func extractNewline(opts *object.Hash, ms *ioModeSpec) {
	v, ok := opts.Get(object.Symbol("newline"))
	if !ok || object.IsNil(v) {
		return
	}
	sym, isSym := v.(object.Symbol)
	switch {
	case isSym && (sym == "universal" || sym == "crlf" || sym == "cr" || sym == "lf"):
		ms.newline = string(sym)
	default:
		raise("ArgumentError", "unexpected value for newline option: %s", v.ToS())
	}
	if ms.binmode {
		raise("ArgumentError", "newline decorator with binary mode")
	}
}

// extractBinmode applies the :textmode / :binmode options to ms, raising the
// ArgumentError conflicts MRI's extract_binmode does ("textmode specified twice",
// "binmode specified twice", "both textmode and binmode specified").
func extractBinmode(opts *object.Hash, ms *ioModeSpec) {
	if v, ok := opts.Get(object.Symbol("textmode")); ok && !object.IsNil(v) {
		if ms.textmode {
			raise("ArgumentError", "textmode specified twice")
		}
		if ms.binmode {
			raise("ArgumentError", "both textmode and binmode specified")
		}
		if v.Truthy() {
			ms.textmode = true
		}
	}
	if v, ok := opts.Get(object.Symbol("binmode")); ok && !object.IsNil(v) {
		if ms.binmode {
			raise("ArgumentError", "binmode specified twice")
		}
		if ms.textmode {
			raise("ArgumentError", "both textmode and binmode specified")
		}
		if v.Truthy() {
			ms.binmode = true
		}
	}
	// (MRI's extract_binmode has a final "both … specified" guard here; it is
	// unreachable in rbgo because the two branches above already reject every mode
	// string / option combination that could set both flags.)
}

// extractEncodingOption applies the :encoding / :external_encoding /
// :internal_encoding options to ms, following io.c rb_io_extract_encoding_option:
// a present :external_encoding or :internal_encoding makes a sibling :encoding be
// ignored (with a warning when $VERBOSE is set); an internal encoding of nil or
// "-" — or one equal to the external — leaves the internal encoding unset. It
// returns whether any encoding was extracted.
func (vm *VM) extractEncodingOption(opts *object.Hash, ms *ioModeSpec) bool {
	encV, hasEnc := opts.Get(object.Symbol("encoding"))
	if hasEnc && object.IsNil(encV) {
		hasEnc = false
	}
	// rb_io_extract_encoding_option reads :external_encoding with a Qnil test but
	// :internal_encoding with a Qundef one, so `external_encoding: nil` is the
	// same as not passing it at all while `internal_encoding: nil` REFUSES an
	// internal encoding. The asymmetry is deliberate in the C and load-bearing
	// here: only the second suppresses the default_internal snapshot.
	extV, hasExt := opts.Get(object.Symbol("external_encoding"))
	if hasExt && object.IsNil(extV) {
		hasExt = false
	}
	intV, hasInt := opts.Get(object.Symbol("internal_encoding"))
	if (hasExt || hasInt) && hasEnc {
		which := "external"
		if !hasExt {
			which = "internal"
		}
		vm.rbWarn("Ignoring encoding parameter '%s': %s_encoding is used", vm.displayStr(encV), which)
		hasEnc = false
	}
	extName := ""
	if hasExt {
		// A "BOM|" marker is accepted only on the :encoding option (and mode
		// strings), not on :external_encoding / :internal_encoding — MRI resolves
		// these as plain encoding names.
		extName = vm.encodingArg(extV).name
	}
	intName, intState := "", encUnset
	if hasInt {
		intState = encNone
		if !object.IsNil(intV) && !isDashString(intV) {
			if n := vm.encodingArg(intV).name; n != extName {
				intName, intState = n, encNamed
			}
		}
	}
	switch {
	case hasEnc:
		if s, ok := encV.(*object.String); ok {
			// A String :encoding goes through parse_mode_enc, which is the same
			// "ext[:int]" grammar a mode suffix uses.
			vm.parseEncPart(stripBOMPrefix(s.Str()), ms)
			return true
		}
		ms.extEnc, ms.intEnc = vm.ioExtIntToEncs(vm.encodingArg(encV).name, "", encUnset)
		ms.encResolved = true
		return true
	case hasExt, hasInt:
		ms.extEnc, ms.intEnc = vm.ioExtIntToEncs(extName, intName, intState)
		ms.encResolved = true
		return true
	}
	return false
}

// recoverAny runs fn and returns whatever it panics with (a RubyError, or a
// break/return/throw control signal), or nil when it returns normally. It lets a
// caller run an ensure step (IO.open's close) before re-raising the first panic.
func recoverAny(fn func()) (rec any) {
	defer func() { rec = recover() }()
	fn()
	return nil
}

// isDashString reports whether v is the String "-" (the internal_encoding value
// MRI treats as "no transcoding").
func isDashString(v object.Value) bool {
	s, ok := v.(*object.String)
	return ok && s.Str() == "-"
}

// rbWarn emits an MRI rb_warn-style warning line to the current $stderr, but only
// when $VERBOSE is non-nil (false or true) — the gate rb_warn applies. Writing to
// curStderr (rather than Kernel#warn) keeps the warning off stdout while still
// honouring a $stderr reassigned to a StringIO (mspec's `complain` matcher). Used
// for the encoding options that IO.new silently overrides.
func (vm *VM) rbWarn(format string, a ...any) {
	if object.IsNil(vm.globals["$VERBOSE"]) {
		return
	}
	vm.curStderr().writeStr(fmt.Sprintf(format, a...) + "\n")
}

// ioOptEncoding returns the canonical encoding name selected by the :encoding /
// :external_encoding option, or "" when none is given.
func (vm *VM) ioOptEncoding(opts *object.Hash) string {
	if v, ok := encOpt(opts); ok {
		return vm.encodingName(v)
	}
	return ""
}
