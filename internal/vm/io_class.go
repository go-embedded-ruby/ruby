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
		fd := int(vm.repeatLong(pos[0]))
		src, ok := vm.fdTable[fd]
		if !ok {
			raise("Errno::EBADF", "Bad file descriptor - fd %d", fd)
		}
		if src.closed {
			raise("IOError", "closed stream")
		}
		res := &IOObj{cls: cIO}
		vm.ioAdoptDescriptor(res, src, pos, opts)
		return res
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
		var ret object.Value
		blockRec := recoverAny(func() { ret = vm.callBlock(blk, []object.Value{o}) })
		closeRec := recoverAny(func() { vm.send(o, "close", nil, nil) })
		if re, isRE := closeRec.(RubyError); isRE && re.Class == "IOError" && re.Message == "closed stream" {
			closeRec = nil // MRI ignores a "closed stream" IOError from the ensure close
		}
		if blockRec != nil {
			panic(blockRec) // the block's exception (or break/return) is primary
		}
		if closeRec != nil {
			panic(closeRec)
		}
		return ret
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
		var data *object.String
		if p, ok := args[0].(*object.String); ok {
			ra := []object.Value{p}
			if hasLen {
				ra = append(ra, object.IntValue(int64(length)))
				if hasOff {
					ra = append(ra, object.IntValue(int64(srcOffset)))
				}
			}
			data = vm.ioReadFile(ra, true).(*object.String)
		} else {
			if hasOff {
				raise("ArgumentError", "cannot specify src_offset for non-IO")
			}
			var ra []object.Value
			if hasLen {
				ra = append(ra, object.IntValue(int64(length)))
			}
			r := vm.send(args[0], "read", ra, nil)
			if s, ok := r.(*object.String); ok {
				data = s
			} else {
				data = object.NewString("")
			}
		}
		if p, ok := args[1].(*object.String); ok {
			vm.ioWriteFile([]object.Value{p, data})
		} else {
			vm.send(args[1], "write", []object.Value{data}, nil)
		}
		return object.IntValue(int64(len(data.Bytes())))
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
		o := openFileIO(cFile, pathArg(vm, pos[0]), "r")
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
		o := openFileIO(cFile, pathArg(vm, pos[0]), "r")
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

// ioAdoptDescriptor makes o wrap the descriptor src under the mode/encoding
// decoded from (pos, opts): the shared buffer/path, the read/write access half
// (an explicit mode incompatible with the descriptor is EINVAL, no mode inherits
// the descriptor's), the append position, and IO.new's optional path: override
// (an explicit nil clears the inherited path). Shared by IO.for_fd/new and
// IO#initialize so the two decode a descriptor identically (io.c io_initialize).
func (vm *VM) ioAdoptDescriptor(o, src *IOObj, pos []object.Value, opts *object.Hash) {
	ms := vm.ioResolveModeEnc(pos, opts)
	o.isStr, o.buf, o.path = true, src.buf, src.path
	o.binmode, o.noAutoclose = ms.binmode, ms.noAutoclose
	o.extEnc, o.intEnc = ms.extEnc, ms.intEnc
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
		extractBinmode(opts, &ms)
	}
	// A binary stream defaults to ASCII-8BIT external encoding unless the mode
	// string already named one (a later :encoding option may still override it).
	if ms.binmode && !ms.hasEnc && ms.extEnc == "" {
		ms.extEnc = "ASCII-8BIT"
	}
	if opts != nil {
		if vm.extractEncodingOption(opts, &ms) && ms.hasEnc {
			raise("ArgumentError", "encoding specified twice")
		}
		if v, ok := opts.Get(object.Symbol("autoclose")); ok {
			ms.noAutoclose = !v.Truthy()
		}
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

// flagsModeSpec fills the read/write/append intent of ms from an integer open-flag
// set (a bitwise OR of File::RDONLY/WRONLY/RDWR/APPEND).
func flagsModeSpec(flags int64, ms *ioModeSpec) {
	switch flags & 0x3 {
	case fO_RDONLY:
		ms.readable = true
	case fO_WRONLY:
		ms.writable = true
	default: // RDWR
		ms.readable, ms.writable = true, true
	}
	if flags&fO_APPEND != 0 {
		ms.writable, ms.appendMode = true, true
	}
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
		ms.writable = true
	case 'a':
		ms.writable, ms.appendMode = true, true
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
		case 'x': // exclusive-create flag; no effect on the fmode intent
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
	if j := strings.IndexByte(enc, ':'); j >= 0 {
		ext, intn = enc[:j], enc[j+1:]
	}
	if ext != "" {
		ms.extEnc = vm.lookupEncodingName(ext).name
	}
	if intn != "" && intn != "-" {
		ms.intEnc = vm.lookupEncodingName(intn).name
		if ms.intEnc == ms.extEnc {
			ms.intEnc = ""
		}
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
	if hasExt {
		ms.extEnc = vm.encNameFromOpt(extV)
	}
	if hasInt {
		switch {
		case object.IsNil(intV), isDashString(intV):
			ms.intEnc = ""
		default:
			ms.intEnc = vm.encNameFromOpt(intV)
		}
		if ms.intEnc != "" && ms.intEnc == ms.extEnc {
			ms.intEnc = ""
		}
	}
	if hasEnc {
		if s, ok := encV.(*object.String); ok {
			name := stripBOMPrefix(s.Str())
			if j := strings.IndexByte(name, ':'); j >= 0 {
				ms.extEnc = vm.lookupEncodingName(name[:j]).name
				ms.intEnc = vm.lookupEncodingName(name[j+1:]).name
				if ms.intEnc == ms.extEnc {
					ms.intEnc = ""
				}
				return true
			}
			ms.extEnc = vm.lookupEncodingName(name).name
			return true
		}
		ms.extEnc = vm.encodingArg(encV).name
		return true
	}
	return hasExt || hasInt
}

// encNameFromOpt resolves an encoding name from an :encoding / :external_encoding
// / :internal_encoding option value, stripping a leading "BOM|" marker: io.c
// rb_io_extract_encoding_option records the byte-order mark as a separate flag
// and resolves the remainder as the encoding name.
func (vm *VM) encNameFromOpt(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return vm.lookupEncodingName(stripBOMPrefix(s.Str())).name
	}
	return vm.encodingArg(v).name
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
