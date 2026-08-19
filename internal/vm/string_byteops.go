// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// byteOnBoundary reports whether byte offset i (already known to be within
// [0, len]) begins a character in s's encoding. The ends are always boundaries;
// a binary (ASCII-8BIT) string treats every byte as a boundary; otherwise (the
// UTF-8 default) a boundary is any byte that is not a UTF-8 continuation byte.
func byteOnBoundary(s *object.String, i int) bool {
	b := s.Bytes()
	if i == 0 || i == len(b) {
		return true
	}
	if s.IsBinary() {
		return true
	}
	return b[i]&0xC0 != 0x80
}

// stringConv coerces v to a *object.String, raising the MRI TypeError (by class
// name) when it is not already one.
func stringConv(v object.Value) *object.String {
	s, ok := v.(*object.String)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	}
	return s
}

// strByteindex implements String#byteindex(pattern, offset=0): the byte index of
// the first match of a String or Regexp at or after offset, or nil.
func (vm *VM) strByteindex(self *object.String, args []object.Value) object.Value {
	s := self.Str()
	n := len(s)
	off := 0
	if len(args) > 1 {
		off = int(toInt(args[1]))
		if off < 0 {
			off += n
		}
	}
	if off < 0 || off > n {
		return object.NilV
	}
	if !byteOnBoundary(self, off) {
		raise("IndexError", "offset %d does not land on character boundary", off)
	}
	switch needle := args[0].(type) {
	case *Regexp:
		md := needle.re.Match(s[off:])
		if md == nil {
			vm.lastMatch = object.NilV
			return object.NilV
		}
		vm.lastMatch = &MatchData{md: md, subject: s, re: needle, byteOff: off}
		return object.IntValue(int64(off + md.Begin(0)))
	default:
		ns := stringConv(args[0])
		vm.combinedEncName(self, ns) // raises Encoding::CompatibilityError if incompatible
		idx := strings.Index(s[off:], ns.Str())
		if idx < 0 {
			return object.NilV
		}
		return object.IntValue(int64(off + idx))
	}
}

// strByterindex implements String#byterindex(pattern, offset=bytesize): the byte
// index of the last match of a String or Regexp whose start is at or before
// offset, or nil.
func (vm *VM) strByterindex(self *object.String, args []object.Value) object.Value {
	s := self.Str()
	n := len(s)
	off := n
	if len(args) > 1 {
		off = int(toInt(args[1]))
		if off < 0 {
			off += n
		}
	}
	if off < 0 {
		return object.NilV
	}
	if off > n {
		off = n
	}
	if !byteOnBoundary(self, off) {
		raise("IndexError", "offset %d does not land on character boundary", off)
	}
	switch needle := args[0].(type) {
	case *Regexp:
		return vm.byterindexRegexp(s, needle, off)
	default:
		ns := stringConv(args[0])
		vm.combinedEncName(self, ns) // raises Encoding::CompatibilityError if incompatible
		return byterindexString(s, ns.Str(), off, n)
	}
}

// byterindexString returns the byte index of the last occurrence of needle
// starting at or before off, or nil. Any match lying wholly within
// s[:off+len(needle)] necessarily starts at or before off.
func byterindexString(s, needle string, off, n int) object.Value {
	end := off + len(needle)
	if end > n {
		end = n
	}
	idx := strings.LastIndex(s[:end], needle)
	if idx < 0 || idx > off {
		return object.NilV
	}
	return object.IntValue(int64(idx))
}

// byterindexRegexp finds the greatest match start at or before off by scanning
// forward (advancing one character past each match start so overlapping matches
// are seen) and keeping the last start not exceeding off. It records the winning
// match as $~.
func (vm *VM) byterindexRegexp(s string, re *Regexp, off int) object.Value {
	best := -1
	for p := 0; p <= len(s); {
		md := re.re.Match(s[p:])
		if md == nil {
			break
		}
		begin := p + md.Begin(0)
		if begin > off {
			break
		}
		best = begin
		vm.lastMatch = &MatchData{md: md, subject: s, re: re, byteOff: p}
		p = begin + runeLenAt(s, begin)
	}
	if best < 0 {
		vm.lastMatch = object.NilV
		return object.NilV
	}
	return object.IntValue(int64(best))
}

// runeLenAt returns the byte length of the UTF-8 rune beginning at s[i], or 1 at
// the end of s or on an invalid lead byte, so a scan always advances.
func runeLenAt(s string, i int) int {
	if i >= len(s) {
		return 1
	}
	_, sz := utf8.DecodeRuneInString(s[i:])
	return sz
}

// strBytesplice implements String#bytesplice, replacing a byte range of the
// receiver in place and returning the receiver. Arities (MRI 4.0):
//
//	bytesplice(index, length, str)
//	bytesplice(index, length, str, str_index, str_length)
//	bytesplice(range, str)
//	bytesplice(range, str, str_range)
func (vm *VM) strBytesplice(self *object.String, args []object.Value) object.Value {
	vm.checkFrozen(self)
	var dstBeg, dstLen int
	var repl string
	switch len(args) {
	case 2:
		r, ok := args[0].(*object.Range)
		if !ok {
			raise("TypeError", "wrong argument type %s (expected Range)", classNameOf(args[0]))
		}
		dstBeg, dstLen = bytespliceRange(self, r)
		repl = stringConv(args[1]).Str()
	case 3:
		if r, ok := args[0].(*object.Range); ok {
			dstBeg, dstLen = bytespliceRange(self, r)
			src := stringConv(args[1])
			sr, ok := args[2].(*object.Range)
			if !ok {
				raise("TypeError", "wrong argument type %s (expected Range)", classNameOf(args[2]))
			}
			repl = bytespliceSrcRange(src, sr)
		} else {
			dstBeg, dstLen = bytespliceIndexLen(self, args[0], args[1])
			repl = stringConv(args[2]).Str()
		}
	case 5:
		dstBeg, dstLen = bytespliceIndexLen(self, args[0], args[1])
		src := stringConv(args[2])
		repl = bytespliceSrcIndexLen(src, args[3], args[4])
	default:
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2, 3, or 5)", len(args))
	}
	b := self.Bytes()
	nb := make([]byte, 0, len(b)-dstLen+len(repl))
	nb = append(nb, b[:dstBeg]...)
	nb = append(nb, repl...)
	nb = append(nb, b[dstBeg+dstLen:]...)
	self.SetBytes(nb)
	return self
}

// bytespliceIndexLen validates and resolves the (index, length) destination
// selector against self, clamping length to the string end and requiring both
// the start and end to fall on character boundaries.
func bytespliceIndexLen(self *object.String, idxV, lenV object.Value) (int, int) {
	n := self.Len()
	idx := int(toInt(idxV))
	raw := idx
	if idx < 0 {
		idx += n
	}
	if idx < 0 || idx > n {
		raise("IndexError", "index %d out of string", raw)
	}
	length := int(toInt(lenV))
	if length < 0 {
		raise("IndexError", "negative length %d", length)
	}
	if idx+length > n {
		length = n - idx
	}
	checkByteBoundary(self, idx)
	checkByteBoundary(self, idx+length)
	return idx, length
}

// bytespliceRange validates and resolves a Range destination selector against
// self, raising RangeError when the start is out of range, clamping the end to
// the string end, and requiring both ends on character boundaries.
func bytespliceRange(self *object.String, r *object.Range) (int, int) {
	n := self.Len()
	beg, length := rangeToByteSpan(r, n)
	checkByteBoundary(self, beg)
	checkByteBoundary(self, beg+length)
	return beg, length
}

// bytespliceSrcIndexLen extracts the (str_index, str_length) slice of a source
// string, with the same validation rules as the destination selector.
func bytespliceSrcIndexLen(src *object.String, siV, slV object.Value) string {
	beg, length := bytespliceIndexLen(src, siV, slV)
	return src.Str()[beg : beg+length]
}

// bytespliceSrcRange extracts the str_range slice of a source string.
func bytespliceSrcRange(src *object.String, r *object.Range) string {
	beg, length := bytespliceRange(src, r)
	return src.Str()[beg : beg+length]
}

// rangeToByteSpan converts a byte Range over a string of length n into a
// (begin, length) span: a negative bound counts from the end, an out-of-range
// begin raises RangeError, the end is clamped to n, and an inverted interval is
// an empty span (an insertion point).
func rangeToByteSpan(r *object.Range, n int) (int, int) {
	beg := 0
	if !object.IsNil(r.Lo) {
		beg = int(toInt(r.Lo))
		if beg < 0 {
			beg += n
		}
	}
	if beg < 0 || beg > n {
		raise("RangeError", "%s out of range", r.Inspect())
	}
	end := n
	if !object.IsNil(r.Hi) {
		end = int(toInt(r.Hi))
		if end < 0 {
			end += n
		}
		if !r.Exclusive {
			end++
		}
	}
	if end > n {
		end = n
	}
	length := end - beg
	if length < 0 {
		length = 0
	}
	return beg, length
}

// checkByteBoundary raises the MRI IndexError when byte offset i does not begin a
// character in s's encoding.
func checkByteBoundary(s *object.String, i int) {
	if !byteOnBoundary(s, i) {
		raise("IndexError", "offset %d does not land on character boundary", i)
	}
}
