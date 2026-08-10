// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
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
	def("foreach", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		pos, _ := splitIOOpts(args)
		if len(pos) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		o := openFileIO(cFile, pathArg(vm, pos[0]), "r")
		sep := pos[1:]
		checkGetsLimit(sep, "foreach")
		var lines []object.Value
		for v := ioGets(o, sep); v != object.NilV; v = ioGets(o, sep) {
			if blk != nil {
				vm.callBlock(blk, []object.Value{v})
			} else {
				lines = append(lines, v)
			}
		}
		if blk == nil { // no block ⇒ an Enumerator; approximate with the line array
			return object.NewArrayFromSlice(lines)
		}
		return object.NilV
	})
	def("readlines", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pos, _ := splitIOOpts(args)
		if len(pos) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		o := openFileIO(cFile, pathArg(vm, pos[0]), "r")
		sep := pos[1:]
		checkGetsLimit(sep, "readlines")
		var lines []object.Value
		for v := ioGets(o, sep); v != object.NilV; v = ioGets(o, sep) {
			lines = append(lines, v)
		}
		return object.NewArrayFromSlice(lines)
	})
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

// ioOptEncoding returns the canonical encoding name selected by the :encoding /
// :external_encoding option, or "" when none is given.
func (vm *VM) ioOptEncoding(opts *object.Hash) string {
	if v, ok := encOpt(opts); ok {
		return vm.encodingName(v)
	}
	return ""
}
