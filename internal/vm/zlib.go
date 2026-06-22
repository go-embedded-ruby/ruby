package vm

import (
	"bytes"
	"compress/zlib"
	"hash/crc32"
	"io"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerZlib installs the Zlib module (require "zlib"): the crc32/adler32
// checksums (exact, matching MRI), and Zlib::Deflate.deflate / Zlib::Inflate
// .inflate built on compress/zlib. Deflated bytes are valid zlib streams that
// round-trip and interoperate, though the compressor's exact output is
// implementation-defined and need not equal MRI's.
func (vm *VM) registerZlib() {
	mod := newClass("Zlib", nil)
	mod.isModule = true
	vm.consts["Zlib"] = mod

	// Zlib::Error and Zlib::DataError, registered scoped + flat (for raise).
	zerr := newClass("Zlib::Error", vm.consts["StandardError"].(*RClass))
	mod.consts["Error"] = zerr
	vm.consts["Zlib::Error"] = zerr
	dataErr := newClass("Zlib::DataError", zerr)
	mod.consts["DataError"] = dataErr
	vm.consts["Zlib::DataError"] = dataErr

	modFn := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }
	modFn("crc32", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var data []byte
		if len(args) > 0 {
			data = []byte(strArg(args[0]))
		}
		init := uint32(0)
		if len(args) > 1 {
			init = uint32(intArg(args[1]))
		}
		return object.Integer(int64(crc32.Update(init, crc32.IEEETable, data)))
	})
	modFn("adler32", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var data []byte
		if len(args) > 0 {
			data = []byte(strArg(args[0]))
		}
		init := uint32(1)
		if len(args) > 1 {
			init = uint32(intArg(args[1]))
		}
		return object.Integer(int64(adler32Update(init, data)))
	})

	deflate := newClass("Zlib::Deflate", vm.cObject)
	mod.consts["Deflate"] = deflate
	deflate.smethods["deflate"] = &Method{name: "deflate", owner: deflate, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		data := []byte(strArg(args[0]))
		level := zlib.DefaultCompression
		if len(args) > 1 {
			level = int(intArg(args[1]))
			if level < zlib.HuffmanOnly || level > zlib.BestCompression {
				raise("Zlib::Error", "stream error")
			}
		}
		var buf bytes.Buffer
		w, _ := zlib.NewWriterLevel(&buf, level) // level already validated
		w.Write(data)
		w.Close()
		return &object.String{B: buf.Bytes()}
	}}

	inflate := newClass("Zlib::Inflate", vm.cObject)
	mod.consts["Inflate"] = inflate
	inflate.smethods["inflate"] = &Method{name: "inflate", owner: inflate, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		r, err := zlib.NewReader(bytes.NewReader([]byte(strArg(args[0]))))
		if err != nil {
			raise("Zlib::DataError", "incorrect header check")
		}
		out, err := io.ReadAll(r)
		if err != nil {
			raise("Zlib::DataError", "invalid stored block lengths")
		}
		return &object.String{B: out}
	}}
}

// adler32Update continues an Adler-32 checksum from an initial value (the
// standard library exposes only the from-scratch form), so Zlib.adler32 can take
// a running checksum as MRI does.
func adler32Update(adler uint32, data []byte) uint32 {
	const mod = 65521
	s1 := adler & 0xffff
	s2 := (adler >> 16) & 0xffff
	for _, b := range data {
		s1 = (s1 + uint32(b)) % mod
		s2 = (s2 + s1) % mod
	}
	return s2<<16 | s1
}
