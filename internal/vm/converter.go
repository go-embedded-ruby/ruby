// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// converterObj binds Encoding::Converter — Ruby's stateful transcoding engine —
// into rbgo. It reuses the pure-Go (CGO=0) transcode primitives that back
// String#encode (decodeToUTF8 / encodeFromUTF8 and the golang.org/x/text
// codecs), driving them through a byte-at-a-time conversion loop so the
// #primitive_convert state machine (status symbols, source-buffer consumption,
// #primitive_errinfo, #last_error, #putback) matches MRI for the common single
// conversion step. The residual — MRI's internal output-buffer carryover across
// #primitive_convert calls (the "� spills into the next buffer" behaviour)
// and the multi-hop stateful decorators (ISO-2022-JP escape-state #finish) — is
// documented at the methods that would need it.
type converterObj struct {
	src, dst string // canonical source / destination encoding names

	repl    []byte // replacement bytes, in the destination encoding
	replEnc string // encoding name the replacement String reports

	invalidReplace bool // :invalid => :replace
	undefReplace   bool // :undef   => :replace
	crlf           bool // crlf_newline decorator requested (for #convpath marker)

	// Error / status state, read back by #primitive_errinfo, #last_error and
	// #putback. status defaults to "source_buffer_empty" (no conversion yet).
	status       string
	errSrcEnc    string // step source encoding at the error (nil-reported when "")
	errDstEnc    string // step destination encoding at the error
	errBytes     []byte // the erroneous / incomplete bytes (nil when none)
	errReadAgain []byte // read-again bytes as reported by #primitive_errinfo
	readAgain    []byte // bytes read past the error, re-fed on the next convert / #putback
	lastErrExc   object.Value

	pendingIncomplete []byte // incomplete tail buffered by #convert for #finish
	finished          bool   // #finish has been called (stream closed)
}

func (c *converterObj) ToS() string {
	return fmt.Sprintf("#<Encoding::Converter: %s to %s>", c.src, c.dst)
}
func (c *converterObj) Inspect() string { return c.ToS() }
func (c *converterObj) Truthy() bool    { return true }

// decodeHop returns the (from, to) encodings of the conversion step that decodes
// the source: source → UTF-8, collapsing to source → dest when the source is
// itself UTF-8. It is what #primitive_errinfo reports for a decode-side error.
func (c *converterObj) decodeHop() (string, string) {
	if c.src == "UTF-8" {
		return c.src, c.dst
	}
	return c.src, "UTF-8"
}

// encodeHop returns the (from, to) encodings of the step that encodes into the
// destination: UTF-8 → dest, collapsing to source → dest when the destination is
// UTF-8. It is what #primitive_errinfo reports for an undefined-conversion error.
func (c *converterObj) encodeHop() (string, string) {
	if c.dst == "UTF-8" {
		return c.src, c.dst
	}
	return "UTF-8", c.dst
}

// registerConverter installs the native Encoding::Converter class. Called from
// registerEncoding after the Encoding class and its error hierarchy exist.
func (vm *VM) registerConverter() {
	cls := newClass("Encoding::Converter", vm.cObject)
	vm.cConverter = cls
	vm.cEncoding.consts["Converter"] = cls
	vm.consts["Encoding::Converter"] = cls

	// The bit-mask / decorator constants. rbgo drives conversion through the
	// keyword options rather than these masks, so the values need only be the
	// distinct Integers MRI exposes (code that ORs them into an options Integer
	// still round-trips through Converter.new, which also accepts an options Hash).
	for name, val := range converterConstants {
		cls.consts[name] = object.IntValue(val)
	}

	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.converterNew(args)
		}}
	cls.smethods["search_convpath"] = &Method{name: "search_convpath", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			src, dst, crlf := vm.convpathArgs(args)
			return vm.buildConvpath(src, dst, crlf)
		}}
	cls.smethods["asciicompat_encoding"] = &Method{name: "asciicompat_encoding", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.asciicompatEncoding(args[0])
		}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	d("source_encoding", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.internEncoding(self.(*converterObj).src)
	})
	d("destination_encoding", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.internEncoding(self.(*converterObj).dst)
	})
	d("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*converterObj).ToS())
	})
	d("replacement", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := self.(*converterObj)
		return object.NewStringBytesEnc(append([]byte(nil), c.repl...), c.replEnc)
	})
	d("replacement=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		vm.converterSetReplacement(self.(*converterObj), args[0])
		return args[0]
	})
	d("convpath", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := self.(*converterObj)
		return vm.buildConvpath(c.src, c.dst, c.crlf)
	})
	d("convert", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.converterConvert(self.(*converterObj), args[0])
	})
	d("finish", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.converterFinish(self.(*converterObj))
	})
	d("primitive_convert", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.primitiveConvert(self.(*converterObj), args)
	})
	d("primitive_errinfo", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*converterObj).errinfo()
	})
	d("last_error", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := self.(*converterObj)
		if c.lastErrExc == nil {
			return object.NilV
		}
		return c.lastErrExc
	})
	d("putback", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.converterPutback(self.(*converterObj), args)
	})
}

// converterConstants are the Integer masks / decorator flags MRI exposes on
// Encoding::Converter. The concrete values match MRI so code that inspects or
// combines them sees the same numbers.
var converterConstants = map[string]int64{
	"INVALID_MASK":                0x0f,
	"INVALID_REPLACE":             0x02,
	"UNDEF_MASK":                  0xf0,
	"UNDEF_REPLACE":               0x20,
	"UNDEF_HEX_CHARREF":           0x30,
	"PARTIAL_INPUT":               0x10000,
	"AFTER_OUTPUT":                0x20000,
	"UNIVERSAL_NEWLINE_DECORATOR": 0x100,
	"CRLF_NEWLINE_DECORATOR":      0x200,
	"CR_NEWLINE_DECORATOR":        0x400,
	"XML_TEXT_DECORATOR":          0x800,
	"XML_ATTR_CONTENT_DECORATOR":  0x1000,
	"XML_ATTR_QUOTE_DECORATOR":    0x2000,
}
