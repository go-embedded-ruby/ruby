// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	gozlib "github.com/go-ruby-zlib/zlib"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// zlibDeflater / zlibInflater wrap a library streaming compressor / decompressor
// so it can live in a Ruby object's instance variable (which holds an
// object.Value). They are never user-visible as values; the Zlib::Deflate /
// Zlib::Inflate RObject keeps one in @__stream.
type zlibDeflater struct{ d *gozlib.Deflater }

func (zlibDeflater) ToS() string     { return "#<Zlib::Deflate>" }
func (zlibDeflater) Inspect() string { return "#<Zlib::Deflate>" }
func (zlibDeflater) Truthy() bool    { return true }

type zlibInflater struct{ i *gozlib.Inflater }

func (zlibInflater) ToS() string     { return "#<Zlib::Inflate>" }
func (zlibInflater) Inspect() string { return "#<Zlib::Inflate>" }
func (zlibInflater) Truthy() bool    { return true }

// raiseZlib maps an error returned by the go-ruby-zlib library to the exact MRI
// Ruby exception. Every error the library returns is a *zlib.Error carrying the
// MRI class name (Zlib::StreamError / BufError / DataError / GzipFile::Error)
// and message, so the binding raises that class verbatim; an unexpected error
// type falls back to Zlib::Error.
func raiseZlib(err error) object.Value {
	var ze *gozlib.Error
	if e, ok := err.(*gozlib.Error); ok {
		ze = e
	}
	if ze != nil {
		return raise(ze.Class, "%s", ze.Msg)
	}
	return raise("Zlib::Error", "%s", err.Error())
}

// registerZlib installs the Zlib module (require "zlib"), backed by the pure-Go
// github.com/go-ruby-zlib/zlib library (no cgo). It provides:
//
//   - module functions Zlib.crc32 / adler32 (+ crc32_combine / adler32_combine)
//     and Zlib.deflate / inflate / gzip / gunzip;
//   - the streaming classes Zlib::Deflate (#deflate / #<< / #finish /
//     #total_in / #total_out / #adler / #finished?) with the .deflate one-shot,
//     and Zlib::Inflate (#inflate / #<< / #finish / accessors) with the .inflate
//     one-shot;
//   - the level constants NO_COMPRESSION / BEST_SPEED / BEST_COMPRESSION /
//     DEFAULT_COMPRESSION, the strategy and flush constants, and VERSION;
//   - the error hierarchy Zlib::Error < StandardError with StreamError / BufError
//     / DataError / GzipFile::Error (+ GzipFile) under it.
//
// The checksums are byte-exact with MRI. The compressors round-trip and
// interoperate with MRI, but a deflate / gzip byte stream is implementation-
// defined and need not equal MRI's, so it is validated by round-trip rather
// than raw-byte equality.
func (vm *VM) registerZlib() {
	mod := newClass("Zlib", nil)
	mod.isModule = true
	vm.consts["Zlib"] = mod

	// Error hierarchy. Each class is registered both scoped (on the module, for
	// constant lookup) and flat in vm.consts (so an internal raise(name) finds it).
	defErr := func(name string, super *RClass) *RClass {
		c := newClass("Zlib::"+name, super)
		mod.consts[name] = c
		vm.consts["Zlib::"+name] = c
		return c
	}
	zerr := defErr("Error", vm.consts["StandardError"].(*RClass))
	defErr("StreamError", zerr)
	defErr("BufError", zerr)
	defErr("DataError", zerr)
	gzFile := defErr("GzipFile", zerr)
	// GzipFile::Error is the class the library names; expose it both as a constant
	// nested under GzipFile and flat for raising.
	gzErr := newClass("Zlib::GzipFile::Error", zerr)
	gzFile.consts["Error"] = gzErr
	vm.consts["Zlib::GzipFile::Error"] = gzErr

	// Level / strategy / flush constants and VERSION. ZLIB_VERSION is host-
	// dependent in MRI, so it is intentionally not asserted by tests, but the
	// library's representative value is still surfaced for completeness.
	ints := map[string]int{
		"NO_COMPRESSION":      gozlib.NoCompression,
		"BEST_SPEED":          gozlib.BestSpeed,
		"BEST_COMPRESSION":    gozlib.BestCompression,
		"DEFAULT_COMPRESSION": gozlib.DefaultCompression,
		"DEFAULT_STRATEGY":    gozlib.DefaultStrategy,
		"FILTERED":            gozlib.Filtered,
		"HUFFMAN_ONLY":        gozlib.HuffmanOnly,
		"RLE":                 gozlib.RLE,
		"FIXED":               gozlib.Fixed,
		"NO_FLUSH":            gozlib.NoFlush,
		"SYNC_FLUSH":          gozlib.SyncFlush,
		"FULL_FLUSH":          gozlib.FullFlush,
		"FINISH":              gozlib.Finish,
	}
	for name, v := range ints {
		mod.consts[name] = object.Integer(int64(v))
	}
	mod.consts["VERSION"] = object.NewString(gozlib.Version)
	mod.consts["ZLIB_VERSION"] = object.NewString(gozlib.ZlibVersion)

	// bytesArg returns args[i] as bytes, or empty when absent.
	bytesArg := func(args []object.Value, i int) []byte {
		if i < len(args) {
			return []byte(strArg(args[i]))
		}
		return nil
	}
	// intAt returns args[i] as an integer, or def when that argument is absent.
	intAt := func(args []object.Value, i int, def int64) int64 {
		if i < len(args) {
			return intArg(args[i])
		}
		return def
	}

	modFn := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// Zlib.crc32(data = "", crc = 0) / Zlib.adler32(data = "", adler = 1).
	modFn("crc32", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(gozlib.Crc32(bytesArg(args, 0), uint32(intAt(args, 1, 0)))))
	})
	modFn("adler32", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(gozlib.Adler32(bytesArg(args, 0), uint32(intAt(args, 1, 1)))))
	})
	// Zlib.crc32_combine(crc1, crc2, len2) / adler32_combine(adler1, adler2, len2).
	modFn("crc32_combine", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(gozlib.Crc32Combine(uint32(intArg(args[0])), uint32(intArg(args[1])), intArg(args[2]))))
	})
	modFn("adler32_combine", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(gozlib.Adler32Combine(uint32(intArg(args[0])), uint32(intArg(args[1])), intArg(args[2]))))
	})

	// Zlib.deflate(data, level = DEFAULT_COMPRESSION) — one-shot compress.
	deflateOneShot := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		level := int(intAt(args, 1, int64(gozlib.DefaultCompression)))
		out, err := gozlib.Deflate(bytesArg(args, 0), level)
		if err != nil {
			raiseZlib(err)
		}
		return &object.String{B: out}
	}
	// Zlib.inflate(data) — one-shot decompress.
	inflateOneShot := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := gozlib.Inflate(bytesArg(args, 0))
		if err != nil {
			raiseZlib(err)
		}
		return &object.String{B: out}
	}
	modFn("deflate", deflateOneShot)
	modFn("inflate", inflateOneShot)

	// Zlib.gzip(data, level: DEFAULT_COMPRESSION) / Zlib.gunzip(data).
	modFn("gzip", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		level := int(intAt(args, 1, int64(gozlib.DefaultCompression)))
		out, err := gozlib.GzipCompress(bytesArg(args, 0), level)
		if err != nil {
			raiseZlib(err)
		}
		return &object.String{B: out}
	})
	modFn("gunzip", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := gozlib.GzipDecompress(bytesArg(args, 0))
		if err != nil {
			raiseZlib(err)
		}
		return &object.String{B: out}
	})

	// Zlib::Deflate — streaming compressor + the Zlib::Deflate.deflate one-shot.
	deflate := newClass("Zlib::Deflate", vm.cObject)
	mod.consts["Deflate"] = deflate
	deflate.smethods["deflate"] = &Method{name: "deflate", owner: deflate, native: deflateOneShot}
	deflate.smethods["new"] = &Method{name: "new", owner: deflate,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			o := &RObject{class: deflate, ivars: map[string]object.Value{}}
			vm.send(o, "initialize", args, blk)
			return o
		}}
	// initialize(level = DEFAULT_COMPRESSION): an out-of-range level raises
	// Zlib::StreamError, matching MRI.
	deflate.define("initialize", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		level := int(intAt(args, 0, int64(gozlib.DefaultCompression)))
		d, err := gozlib.NewDeflaterLevel(level)
		if err != nil {
			raiseZlib(err)
		}
		setIvar(self, "@__stream", zlibDeflater{d: d})
		return object.NilV
	})
	selfDeflater := func(self object.Value) *gozlib.Deflater { return getIvar(self, "@__stream").(zlibDeflater).d }
	// takePending drains the bytes #<< produced but did not yet hand back, so the
	// next #deflate / #finish emits a contiguous stream (MRI's #<< buffers, with
	// the bytes surfacing on the next read).
	takePending := func(self object.Value) []byte {
		if p, ok := getIvar(self, "@__pending").(*object.String); ok && len(p.B) > 0 {
			setIvar(self, "@__pending", object.NilV)
			return p.B
		}
		return nil
	}
	// #deflate(data, flush = NO_FLUSH): feed data, returning the bytes produced so
	// far (any bytes buffered by an earlier #<<, then the new output).
	deflate.define("deflate", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		flush := int(intAt(args, 1, int64(gozlib.NoFlush)))
		out, err := selfDeflater(self).Deflate(bytesArg(args, 0), flush)
		if err != nil {
			raiseZlib(err)
		}
		return &object.String{B: append(takePending(self), out...)}
	})
	// #<<(data): feed data and return self; the bytes produced are buffered and
	// surface on the next #deflate / #finish, matching MRI's append idiom.
	deflate.define("<<", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := selfDeflater(self).Deflate(bytesArg(args, 0), gozlib.NoFlush)
		if err != nil {
			raiseZlib(err)
		}
		setIvar(self, "@__pending", &object.String{B: append(takePending(self), out...)})
		return self
	})
	// #finish: flush and close the stream, returning any buffered bytes plus the
	// trailing bytes — i.e. the rest of the stream.
	deflate.define("finish", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		// Deflater.Finish never errors (a 2nd #finish returns "", matching MRI's
		// tolerance), so the result needs no error handling.
		out, _ := selfDeflater(self).Finish()
		return &object.String{B: append(takePending(self), out...)}
	})
	deflate.define("total_in", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(selfDeflater(self).TotalIn())
	})
	deflate.define("total_out", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(selfDeflater(self).TotalOut())
	})
	deflate.define("adler", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(selfDeflater(self).Adler()))
	})
	deflate.define("finished?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(selfDeflater(self).Finished())
	})

	// Zlib::Inflate — streaming decompressor + the Zlib::Inflate.inflate one-shot.
	inflate := newClass("Zlib::Inflate", vm.cObject)
	mod.consts["Inflate"] = inflate
	inflate.smethods["inflate"] = &Method{name: "inflate", owner: inflate, native: inflateOneShot}
	inflate.smethods["new"] = &Method{name: "new", owner: inflate,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			o := &RObject{class: inflate, ivars: map[string]object.Value{}}
			vm.send(o, "initialize", args, blk)
			return o
		}}
	inflate.define("initialize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		setIvar(self, "@__stream", zlibInflater{i: gozlib.NewInflater()})
		return object.NilV
	})
	selfInflater := func(self object.Value) *gozlib.Inflater { return getIvar(self, "@__stream").(zlibInflater).i }
	// takeInflated drains the decoded bytes #<< produced but did not yet hand back,
	// so #finish returns them (MRI's #<< buffers; the bytes surface on a read).
	takeInflated := func(self object.Value) []byte {
		if p, ok := getIvar(self, "@__pending").(*object.String); ok && len(p.B) > 0 {
			setIvar(self, "@__pending", object.NilV)
			return p.B
		}
		return nil
	}
	// #inflate(data): feed a (complete) stream and return the decoded bytes (after
	// any bytes buffered by an earlier #<<).
	inflate.define("inflate", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := selfInflater(self).Inflate(bytesArg(args, 0))
		if err != nil {
			raiseZlib(err)
		}
		return &object.String{B: append(takeInflated(self), out...)}
	})
	// #<<(data): feed compressed data and return self; the decoded bytes are
	// buffered and surface on the next #inflate / #finish.
	inflate.define("<<", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := selfInflater(self).Inflate(bytesArg(args, 0))
		if err != nil {
			raiseZlib(err)
		}
		setIvar(self, "@__pending", &object.String{B: append(takeInflated(self), out...)})
		return self
	})
	inflate.define("finish", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		// Inflater.Finish marks the stream finished and returns no further bytes
		// (a no-op in this one-shot streaming model); its error is always nil, so
		// only the buffered #<< output is surfaced here.
		out, _ := selfInflater(self).Finish()
		return &object.String{B: append(takeInflated(self), out...)}
	})
	inflate.define("total_in", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(selfInflater(self).TotalIn())
	})
	inflate.define("total_out", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(selfInflater(self).TotalOut())
	})
	inflate.define("adler", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(int64(selfInflater(self).Adler()))
	})
	inflate.define("finished?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(selfInflater(self).Finished())
	})
}
