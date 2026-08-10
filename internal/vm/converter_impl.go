// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// stepStatus is the outcome of decoding one source character.
type stepStatus int

const (
	stepOK stepStatus = iota
	stepInvalid
	stepIncomplete
)

// binStr wraps raw bytes as an ASCII-8BIT Ruby String — the form
// #primitive_errinfo / #last_error error fields and #putback all report.
func binStr(b []byte) *object.String {
	return object.NewStringBytesEnc(append([]byte(nil), b...), "ASCII-8BIT")
}

// --- Encoding::Converter.new -------------------------------------------------

// converterNew builds a converter from Converter.new(src, dst, opts). src/dst are
// coerced through Encoding.find (a String, Encoding, or #to_str); identical
// encodings raise ConverterNotFoundError. opts is an options Hash (or Integer of
// ORed flags) carrying :replace / :invalid / :undef / crlf_newline.
func (vm *VM) converterNew(args []object.Value) object.Value {
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
	}
	c := &converterObj{
		src:    vm.encodingArg(args[0]).name,
		dst:    vm.encodingArg(args[1]).name,
		status: "source_buffer_empty",
	}
	if c.src == c.dst {
		raise("Encoding::ConverterNotFoundError", "code converter not found (%s to %s)", c.src, c.dst)
	}
	c.repl, c.replEnc = defaultReplacementBytes(c.dst)

	if len(args) >= 3 {
		vm.applyConverterOpts(c, args[2])
	}
	return c
}

// applyConverterOpts reads Converter.new's third argument: an options Hash (its
// keys drive the flags), or an Integer whose INVALID_REPLACE / UNDEF_REPLACE /
// CRLF_NEWLINE_DECORATOR bits set the same flags.
func (vm *VM) applyConverterOpts(c *converterObj, opt object.Value) {
	if i, ok := opt.(object.Integer); ok {
		flags := int64(i)
		c.invalidReplace = flags&converterConstants["INVALID_REPLACE"] != 0
		c.undefReplace = flags&converterConstants["UNDEF_REPLACE"] != 0
		c.crlf = flags&converterConstants["CRLF_NEWLINE_DECORATOR"] != 0
		return
	}
	h := vm.toHash(opt)
	if v, ok := h.Get(object.Symbol("invalid")); ok {
		c.invalidReplace = v == object.Symbol("replace")
	}
	if v, ok := h.Get(object.Symbol("undef")); ok {
		c.undefReplace = v == object.Symbol("replace")
	}
	if v, ok := h.Get(object.Symbol("crlf_newline")); ok {
		c.crlf = truthyValue(v)
	}
	if v, ok := h.Get(object.Symbol("replace")); ok {
		c.setReplacementValue(vm, v)
	}
}

// toHash coerces the options argument to a Hash, calling #to_hash when needed —
// matching Converter.new's handling of a non-Hash, non-Integer options object.
func (vm *VM) toHash(v object.Value) *object.Hash {
	if h, ok := v.(*object.Hash); ok {
		return h
	}
	if vm.respondsToDynamic(v, "to_hash") {
		if h, ok := vm.send(v, "to_hash", nil, nil).(*object.Hash); ok {
			return h
		}
	}
	raise("TypeError", "no implicit conversion of %s into Hash", classNameOf(v))
	return nil
}

// defaultReplacementBytes is the replacement MRI installs when none is given:
// U+FFFD (UTF-8) for a UTF-8 destination, "?" (US-ASCII) otherwise.
func defaultReplacementBytes(dst string) ([]byte, string) {
	if dst == "UTF-8" {
		return []byte("�"), "UTF-8"
	}
	return []byte("?"), "US-ASCII"
}

// setReplacementValue applies a :replace option value: nil restores the default,
// otherwise the value is coerced through #to_str and stored in the destination
// encoding. A non-String (true/false/Integer, or a #to_str returning non-String)
// raises TypeError.
func (c *converterObj) setReplacementValue(vm *VM, v object.Value) {
	if v == object.NilV {
		c.repl, c.replEnc = defaultReplacementBytes(c.dst)
		return
	}
	s := vm.strToStr(v)
	c.repl = append([]byte(nil), s...)
	c.replEnc = c.dst
}

// strToStr coerces v to the bytes of a String, via #to_str when needed, raising
// TypeError otherwise.
func (vm *VM) strToStr(v object.Value) []byte {
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

// converterSetReplacement backs Converter#replacement=. The argument must be a
// String; MRI transcodes it into the destination encoding, so an unrepresentable
// replacement raises UndefinedConversionError and leaves the old one in place.
func (vm *VM) converterSetReplacement(c *converterObj, v object.Value) {
	s, ok := v.(*object.String)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	}
	repl := vm.transcodeReplacement(c, s)
	c.repl = repl
	c.replEnc = c.dst
}

// transcodeReplacement converts the assigned replacement String into the
// destination encoding, raising UndefinedConversionError if a character is not
// representable there.
func (vm *VM) transcodeReplacement(c *converterObj, s *object.String) []byte {
	inter := vm.decodeToUTF8(s.Bytes(), s.EncName(), transcodeOpts{}, "")
	var out []byte
	for _, r := range inter {
		eb, ok := vm.encodeCharTo(r, c.dst)
		if !ok {
			raise("Encoding::UndefinedConversionError", "U+%04X from UTF-8 to %s", r, c.dst)
		}
		out = append(out, eb...)
	}
	return out
}

// --- convpath / search_convpath / asciicompat_encoding -----------------------

// pathList is the chain of encodings a src→dst conversion traverses: src → UTF-8
// → dst, with consecutive duplicates removed (so a UTF-8 endpoint collapses the
// pivot). It is the basis of #convpath and the transcoding-path error messages.
func pathList(src, dst string) []string {
	raw := []string{src, "UTF-8", dst}
	out := raw[:1]
	for _, e := range raw[1:] {
		if e != out[len(out)-1] {
			out = append(out, e)
		}
	}
	return out
}

// buildConvpath returns the Array #convpath / .search_convpath produce: one
// [from, to] Encoding pair per hop, plus a trailing "crlf_newline" String when
// that decorator is active. A hop with no available codec raises
// ConverterNotFoundError.
func (vm *VM) buildConvpath(src, dst string, crlf bool) object.Value {
	path := pathList(src, dst)
	var out []object.Value
	for i := 0; i+1 < len(path); i++ {
		if !vm.hopConvertible(path[i], path[i+1]) {
			raise("Encoding::ConverterNotFoundError", "code converter not found (%s to %s)", src, dst)
		}
		out = append(out, object.NewArrayFromSlice([]object.Value{
			vm.internEncoding(path[i]), vm.internEncoding(path[i+1]),
		}))
	}
	if crlf {
		out = append(out, object.NewString("crlf_newline"))
	}
	return object.NewArrayFromSlice(out)
}

// hopConvertible reports whether rbgo has a codec for a single from→to hop (one
// side is always UTF-8): the core encodings handled directly, or an x/text codec
// for the non-UTF-8 side.
func (vm *VM) hopConvertible(from, to string) bool {
	other := from
	if from == "UTF-8" {
		other = to
	}
	return encodingSupported(other)
}

// encodingSupported reports whether rbgo can transcode the named encoding to/from
// UTF-8 (a core encoding or one with an x/text codec).
func encodingSupported(name string) bool {
	switch name {
	case "UTF-8", "US-ASCII", "ASCII-8BIT", "ISO-8859-1",
		"UTF-16LE", "UTF-16BE", "UTF-32LE", "UTF-32BE":
		return true
	}
	_, ok := xtextEncodings[name]
	return ok
}

// convpathArgs parses the (src, dst, opts) arguments shared by
// .search_convpath, returning canonical names and whether crlf_newline is set.
func (vm *VM) convpathArgs(args []object.Value) (string, string, bool) {
	src := vm.encodingArg(args[0]).name
	dst := vm.encodingArg(args[1]).name
	crlf := false
	if h, ok := trailingHash(args); ok {
		if v, ok := h.Get(object.Symbol("crlf_newline")); ok {
			crlf = truthyValue(v)
		}
	}
	return src, dst, crlf
}

// asciicompatMap gives, for each ASCII-incompatible encoding rbgo knows,
// the ASCII-compatible encoding MRI's Converter.asciicompat_encoding returns.
var asciicompatMap = map[string]string{
	"UTF-16": "UTF-8", "UTF-16BE": "UTF-8", "UTF-16LE": "UTF-8",
	"UTF-32": "UTF-8", "UTF-32BE": "UTF-8", "UTF-32LE": "UTF-8",
	"ISO-2022-JP": "stateless-ISO-2022-JP", "ISO-2022-JP-2": "stateless-ISO-2022-JP",
	"ISO-2022-JP-KDDI": "stateless-ISO-2022-JP-KDDI",
}

// asciicompatEncoding implements Converter.asciicompat_encoding: nil for an
// already-ASCII-compatible (or unknown, or "internal") encoding, else the
// corresponding ASCII-compatible Encoding.
func (vm *VM) asciicompatEncoding(v object.Value) object.Value {
	var name string
	if e, ok := v.(*encodingObj); ok {
		name = e.name
	} else {
		name = string(vm.strToStr(v))
	}
	if lower(name) == "internal" { // Encoding.default_internal is nil in rbgo
		return object.NilV
	}
	e, ok := vm.findEncoding(name)
	if !ok {
		return object.NilV
	}
	if e.asciiCompat {
		return object.NilV
	}
	if compat, ok := asciicompatMap[e.name]; ok {
		return vm.internEncoding(compat)
	}
	return vm.internEncoding("UTF-8")
}

// --- convert / finish --------------------------------------------------------

// converterConvert implements Converter#convert: transcode the whole argument,
// raising the matching Encoding error (and recording it for #last_error /
// #primitive_errinfo) on an invalid or undefined sequence. A trailing incomplete
// sequence is buffered for #finish rather than raised, as in MRI.
func (vm *VM) converterConvert(c *converterObj, v object.Value) object.Value {
	if c.finished {
		raise("ArgumentError", "convert after finish")
	}
	in := append(append([]byte(nil), c.readAgain...), vm.strToStr(v)...)
	c.readAgain = nil
	out, _, status, errBytes, readAgain := vm.stepLoop(c, in, -1)
	switch status {
	case "invalid_byte_sequence":
		vm.setConvError(c, "invalid_byte_sequence", errBytes, readAgain, false)
		c.readAgain = append([]byte(nil), readAgain...)
		raise("Encoding::InvalidByteSequenceError", "%s", c.invalidMessage(errBytes, readAgain))
	case "undefined_conversion":
		vm.setConvError(c, "undefined_conversion", errBytes, nil, true)
		raise("Encoding::UndefinedConversionError", "%s", c.undefMessage(errBytes))
	case "incomplete_input":
		// Hold the incomplete tail; #finish reports it. #convert itself succeeds.
		c.pendingIncomplete = errBytes
		c.status = "source_buffer_empty"
		c.lastErrExc = nil
	default:
		c.status = "source_buffer_empty"
		c.lastErrExc = nil
	}
	return object.NewStringBytesEnc(out, c.dst)
}

// converterFinish is invoked by #finish (defined inline) — kept here so the
// incomplete-tail raise path lives beside #convert. It returns the tail of the
// conversion; rbgo's stateless codecs emit nothing further, except that a
// buffered incomplete sequence raises InvalidByteSequenceError as MRI does.
func (vm *VM) converterFinish(c *converterObj) object.Value {
	if len(c.pendingIncomplete) > 0 {
		errBytes := c.pendingIncomplete
		c.pendingIncomplete = nil
		vm.setConvError(c, "incomplete_input", errBytes, nil, false)
		raise("Encoding::InvalidByteSequenceError", "incomplete %s on %s",
			binStr(errBytes).Inspect(), c.src)
	}
	c.finished = true
	return object.NewStringBytesEnc(nil, c.dst)
}

// --- primitive_convert -------------------------------------------------------

// primitiveConvert implements Converter#primitive_convert(src, dst, off, size,
// opts): it drives one conversion pass, writing into dst at off (capped at size
// bytes), consuming the converted bytes from src, and returning the status
// symbol. It does not model MRI's internal output-buffer carryover across calls
// (the documented residual): each call converts what fits in one pass.
func (vm *VM) primitiveConvert(c *converterObj, args []object.Value) object.Value {
	pos := args
	opts, _ := trailingHash(pos)
	if opts != nil {
		pos = pos[:len(pos)-1]
	}
	// source buffer: nil or a String.
	var in []byte
	if pos[0] != object.NilV {
		in = append(in, vm.strToStr(pos[0])...)
	}
	in = append(append([]byte(nil), c.readAgain...), in...)
	c.readAgain = nil

	dst, ok := pos[1].(*object.String)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(pos[1]))
	}
	vm.checkFrozen(dst)

	offset := len(dst.Bytes())
	if len(pos) > 2 && pos[2] != object.NilV {
		offset = int(vm.toIntCoerce(pos[2]))
	}
	if offset > len(dst.Bytes()) {
		raise("ArgumentError", "too big destination byte offset")
	}
	size := -1
	if len(pos) > 3 && pos[3] != object.NilV {
		size = int(vm.toIntCoerce(pos[3]))
	}
	partial := false
	if opts != nil {
		if v, ok := opts.Get(object.Symbol("partial_input")); ok {
			partial = truthyValue(v)
		}
	}

	out, consumed, status, errBytes, readAgain := vm.stepLoop(c, in, size)
	if partial && status == "incomplete_input" {
		status = "source_buffer_empty"
	} else if !partial && status == "source_buffer_empty" {
		status = "finished"
	}

	// Write the produced bytes at offset (truncating anything past it), tagging the
	// destination with the destination encoding.
	base := dst.Bytes()
	if offset < len(base) {
		base = base[:offset]
	}
	dst.SetBytes(append(append([]byte(nil), base...), out...))
	dst.Enc = c.dst

	// Consume the converted bytes from the source String (when one was given).
	if s, ok := pos[0].(*object.String); ok {
		s.SetBytes(append([]byte(nil), in[consumed:]...))
	}

	switch status {
	case "invalid_byte_sequence":
		vm.setConvError(c, status, errBytes, readAgain, false)
		c.readAgain = append([]byte(nil), readAgain...)
	case "undefined_conversion":
		vm.setConvError(c, status, errBytes, nil, true)
	case "incomplete_input":
		vm.setConvError(c, status, errBytes, nil, false)
	default: // finished / source_buffer_empty / destination_buffer_full
		c.status = status
		c.errBytes, c.errReadAgain, c.errSrcEnc, c.errDstEnc = nil, nil, "", ""
		c.lastErrExc = nil
	}
	return object.Symbol(status)
}

// stepLoop is the shared conversion engine for #convert and #primitive_convert.
// It decodes src character by character in the source encoding, encodes each into
// the destination encoding, and stops at the first error or when appending the
// next character would exceed size (a byte cap; negative means unlimited). It
// returns the produced bytes, the number of source bytes consumed, a status
// string, and the erroneous / read-again bytes for the error statuses.
func (vm *VM) stepLoop(c *converterObj, in []byte, size int) (out []byte, consumed int, status string, errBytes, readAgain []byte) {
	pos := 0
	for pos < len(in) {
		r, n, rl, st := vm.decodeCharFrom(in[pos:], c.src)
		switch st {
		case stepIncomplete:
			if c.invalidReplace {
				out = appendCapped(out, c.repl, size)
				return out, len(in), "finished", nil, nil
			}
			return out, len(in), "incomplete_input", in[pos:], nil
		case stepInvalid:
			if c.invalidReplace {
				out = appendCapped(out, c.repl, size)
				pos += n + rl
				continue
			}
			return out, pos + n + rl, "invalid_byte_sequence", in[pos : pos+n], in[pos+n : pos+n+rl]
		}
		eb, okEnc := vm.encodeCharTo(r, c.dst)
		if !okEnc {
			if c.undefReplace {
				eb = c.repl
			} else {
				return out, pos + n, "undefined_conversion", []byte(string(r)), nil
			}
		}
		if size >= 0 && len(out)+len(eb) > size {
			return out, pos, "destination_buffer_full", nil, nil
		}
		out = append(out, eb...)
		pos += n
	}
	return out, len(in), "source_buffer_empty", nil, nil
}

// appendCapped appends add to out unless doing so would exceed a non-negative
// size cap, in which case out is returned unchanged (the replacement is dropped
// only at the very edge of the buffer — sufficient for the replace fast paths).
func appendCapped(out, add []byte, size int) []byte {
	if size >= 0 && len(out)+len(add) > size {
		return out
	}
	return append(out, add...)
}

// --- error state / errinfo / last_error / putback ----------------------------

// setConvError records the state #primitive_errinfo, #last_error and #putback
// read back after an error. undef selects the encode-side hop (UTF-8 → dest) for
// the reported encodings; otherwise the decode-side hop (source → UTF-8) is used.
func (vm *VM) setConvError(c *converterObj, status string, errBytes, readAgain []byte, undef bool) {
	c.status = status
	c.errBytes = append([]byte(nil), errBytes...)
	c.errReadAgain = append([]byte(nil), readAgain...)
	if undef {
		c.errSrcEnc, c.errDstEnc = c.encodeHop()
	} else {
		c.errSrcEnc, c.errDstEnc = c.decodeHop()
	}
	switch status {
	case "invalid_byte_sequence":
		c.lastErrExc = vm.buildException("Encoding::InvalidByteSequenceError", c.invalidMessage(errBytes, readAgain))
	case "incomplete_input":
		c.lastErrExc = vm.buildException("Encoding::InvalidByteSequenceError",
			fmt.Sprintf("incomplete %s on %s", binStr(errBytes).Inspect(), c.src))
	case "undefined_conversion":
		c.lastErrExc = vm.buildException("Encoding::UndefinedConversionError", c.undefMessage(errBytes))
	}
}

// errinfo builds the #primitive_errinfo tuple [status, src_enc, dst_enc,
// error_bytes, read_again_bytes]. The four detail slots are nil unless the last
// status was an error.
func (c *converterObj) errinfo() object.Value {
	sym := object.Symbol(c.status)
	switch c.status {
	case "invalid_byte_sequence", "undefined_conversion", "incomplete_input":
		return object.NewArrayFromSlice([]object.Value{
			sym, object.NewString(c.errSrcEnc), object.NewString(c.errDstEnc),
			binStr(c.errBytes), binStr(c.errReadAgain),
		})
	}
	return object.NewArrayFromSlice([]object.Value{sym, object.NilV, object.NilV, object.NilV, object.NilV})
}

// undefMessage is the UndefinedConversionError message MRI raises: a direct
// "U+XXXX from A to B" for a single hop, or "U+XXXX to DST in conversion from
// A to … to DST" when the character is undefined after the UTF-8 pivot.
func (c *converterObj) undefMessage(utf8Bytes []byte) string {
	r, _ := utf8.DecodeRune(utf8Bytes)
	path := pathList(c.src, c.dst)
	if len(path) == 2 {
		return fmt.Sprintf("U+%04X from %s to %s", r, path[0], path[1])
	}
	return fmt.Sprintf("U+%04X to %s in conversion from %s", r, c.dst, strings.Join(path, " to "))
}

// invalidMessage is the InvalidByteSequenceError message: the erroneous bytes,
// the read-again bytes (when any), and the source encoding they were read in.
func (c *converterObj) invalidMessage(errBytes, readAgain []byte) string {
	if len(readAgain) > 0 {
		return fmt.Sprintf("%s followed by %s on %s",
			binStr(errBytes).Inspect(), binStr(readAgain).Inspect(), c.src)
	}
	return fmt.Sprintf("%s on %s", binStr(errBytes).Inspect(), c.src)
}

// converterPutback implements Converter#putback: it returns (and clears) the
// bytes read past an :invalid_byte_sequence error, tagged in the source encoding,
// so the caller can resume conversion after the error.
func (vm *VM) converterPutback(c *converterObj, args []object.Value) object.Value {
	n := len(c.readAgain)
	if len(args) > 0 && args[0] != object.NilV {
		if k := int(vm.toIntCoerce(args[0])); k < n {
			n = k
		}
	}
	b := c.readAgain[len(c.readAgain)-n:]
	out := object.NewStringBytesEnc(append([]byte(nil), b...), c.src)
	c.readAgain = c.readAgain[:len(c.readAgain)-n]
	c.errReadAgain = c.readAgain
	return out
}

// toIntCoerce coerces v to an int64, calling #to_int when v is not already an
// Integer, and raising TypeError otherwise.
func (vm *VM) toIntCoerce(v object.Value) int64 {
	if i, ok := v.(object.Integer); ok {
		return int64(i)
	}
	if vm.respondsToDynamic(v, "to_int") {
		if i, ok := vm.send(v, "to_int", nil, nil).(object.Integer); ok {
			return int64(i)
		}
	}
	raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(v))
	return 0
}
