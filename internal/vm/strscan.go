// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"

	"github.com/go-embedded-ruby/ruby/internal/object"
	strscan "github.com/go-ruby-strscan/strscan"
)

// StringScanner binds github.com/go-ruby-strscan/strscan — a pure-Go (cgo-free)
// reimplementation of Ruby's strscan standard library (require "strscan") — into
// rbgo, replacing the former pure-Ruby prelude implementation. The cursor logic
// (anchored / forward matching, the recorded match, pre/post text, position
// arithmetic) lives entirely in the library, ported byte for byte from that
// prelude and validated against MRI 4.0.5; this file is only the thin wrapper
// that turns Ruby method calls into Go method calls and the library's
// (string,bool) / error results back into Ruby nil / exceptions. The scanner
// matches with the same go-ruby-regexp engine rbgo's own Regexp uses, so the
// regex semantics (UTF-8, named groups, flags) are identical to before. The
// surface is exactly what Puppet's Pops lexer drives (scan/scan_until/skip/peek/
// pos/pos=/eos?/matched/match?/string/[]) plus the rest of MRI's API.
type StringScanner struct{ sc *strscan.Scanner }

func (s *StringScanner) ToS() string {
	// MRI's StringScanner#inspect: "#<StringScanner fin>" at end-of-string, else
	// "#<StringScanner pos/len [\"matched\" ]@ \"peek...\">" with the matched text
	// shown only when there is a current match and the peek window elided with
	// "..." when more than five bytes remain.
	if s.sc.EOS() {
		return "#<StringScanner fin>"
	}
	matched := ""
	if m, ok := s.sc.Matched(); ok {
		matched = strscanInspectStr(m) + " "
	}
	rest := s.sc.Rest()
	peek := rest
	suffix := ""
	if len(rest) > 5 {
		peek = rest[:5]
		suffix = "..."
	}
	return fmt.Sprintf("#<StringScanner %d/%d %s@ %s>",
		s.sc.Pos(), len(s.sc.String()), matched,
		strscanInspectStr(peek+suffix))
}

func (s *StringScanner) Inspect() string { return s.ToS() }
func (s *StringScanner) Truthy() bool    { return true }

// strscanInspectStr renders a string the way MRI's inspect quotes the scanner's
// matched / peek windows: double-quoted with the common escapes. It is the
// String#inspect form restricted to the byte set the scanner ever shows.
func strscanInspectStr(s string) string {
	return object.NewString(s).Inspect()
}

// ssScannerOf returns the receiver's wrapped library scanner.
func ssScannerOf(v object.Value) *strscan.Scanner { return v.(*StringScanner).sc }

// ssPattern converts a Ruby scan/skip/match pattern into the regex source string
// the library compiles. A Regexp contributes its source with an inline (?imx)
// flag prefix — exactly the form rbgo's own compileRegexp feeds go-ruby-regexp,
// so the flags carry through. A String is matched literally, as MRI does, by
// escaping its regex metacharacters. Any other value is coerced via to_str the
// way MRI requires a string-like pattern.
func ssPattern(vm *VM, v object.Value) string {
	switch p := v.(type) {
	case *Regexp:
		if imx := sortIMX(p.flags); imx != "" {
			return "(?" + imx + ")" + p.source
		}
		return p.source
	case *object.String:
		return regexpEscapeLiteral(p.Str())
	default:
		s := vm.send(v, "to_str", nil, nil)
		if str, ok := s.(*object.String); ok {
			return regexpEscapeLiteral(str.Str())
		}
		raise("TypeError", "wrong argument type %s (expected Regexp)", classNameOf(v))
		return ""
	}
}

// ssStr wraps a (string, ok) library result as a Ruby String, or nil when ok is
// false — the shape scan / scan_until / check / check_until / getch / matched /
// pre_match / post_match all share.
func ssStr(text string, ok bool) object.Value {
	if !ok {
		return object.NilV
	}
	return object.NewString(text)
}

// ssLen wraps a (length, ok) library result as a Ruby Integer length, or nil
// when ok is false — the shape skip / skip_until / match? share.
func ssLen(n int, ok bool) object.Value {
	if !ok {
		return object.NilV
	}
	return object.Integer(n)
}

// registerStringScanner installs the native StringScanner class backed by the
// go-ruby-strscan library. StringScanner.new wraps a fresh library Scanner; each
// instance method forwards to the corresponding Go method, mapping the library's
// (.,false) misses to Ruby nil and its errors to the matching Ruby exception
// (StringScanner::Error for #unscan, RangeError for #pos=). StringScanner::Error
// is created here (< StandardError) so the unscan failure raises the class MRI
// raises.
func (vm *VM) registerStringScanner() {
	cls := newClass("StringScanner", vm.cObject)
	vm.cStringScanner = cls
	vm.consts["StringScanner"] = cls

	// StringScanner::Error < StandardError, the exception #unscan raises with
	// nothing to undo (and the public exception type MRI exposes). Registered
	// scoped (StringScanner::Error) and flat (so raise can name it).
	std, _ := vm.consts["StandardError"].(*RClass)
	ssErr := newClass("StringScanner::Error", std)
	cls.consts["Error"] = ssErr
	vm.consts["StringScanner::Error"] = ssErr

	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			str := ""
			if len(args) > 0 {
				if _, isNil := args[0].(object.Nil); !isNil {
					str = strArg(args[0])
				}
			}
			return &StringScanner{sc: strscan.New(str)}
		}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// scan / scan_until / check / check_until / getch return the matched text or
	// nil; matched / pre_match / post_match read back the recorded match.
	d("scan", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return ssStr(ssScannerOf(v).Scan(ssPattern(vm, args[0])))
	})
	d("scan_until", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return ssStr(ssScannerOf(v).ScanUntil(ssPattern(vm, args[0])))
	})
	d("check", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return ssStr(ssScannerOf(v).Check(ssPattern(vm, args[0])))
	})
	d("check_until", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return ssStr(ssScannerOf(v).CheckUntil(ssPattern(vm, args[0])))
	})
	d("getch", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return ssStr(ssScannerOf(v).Getch())
	})
	d("matched", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return ssStr(ssScannerOf(v).Matched())
	})
	matchedP := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		_, ok := ssScannerOf(v).Matched()
		return object.Bool(ok)
	}
	d("matched?", matchedP)
	d("pre_match", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return ssStr(ssScannerOf(v).PreMatch())
	})
	d("post_match", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return ssStr(ssScannerOf(v).PostMatch())
	})

	// skip / skip_until / match? return the byte length of the match or nil.
	d("skip", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return ssLen(ssScannerOf(v).Skip(ssPattern(vm, args[0])))
	})
	d("skip_until", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return ssLen(ssScannerOf(v).SkipUntil(ssPattern(vm, args[0])))
	})
	d("match?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return ssLen(ssScannerOf(v).Match(ssPattern(vm, args[0])))
	})
	d("matched_size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		n := ssScannerOf(v).MatchedSize()
		if n < 0 {
			return object.NilV
		}
		return object.Integer(n)
	})

	// [] reads a capture of the most recent match: an Integer index via Group, a
	// Symbol/String name via GroupName. An out-of-range integer index returns nil,
	// but an undefined NAME raises IndexError — matching MRI's StringScanner#[].
	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		sc := ssScannerOf(v)
		switch k := args[0].(type) {
		case object.Integer:
			text, ok := sc.Group(int(k))
			if !ok {
				return object.NilV
			}
			return object.NewString(text)
		case object.Symbol:
			return ssNamedGroup(sc, string(k))
		case *object.String:
			return ssNamedGroup(sc, k.Str())
		default:
			raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(args[0]))
			return object.NilV
		}
	})

	// peek(n) returns up to n bytes from the current position without advancing.
	d("peek", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(ssScannerOf(v).Peek(int(intArg(args[0]))))
	})

	// pos / charpos report the position; pos= moves it, raising RangeError when
	// the target is outside [0, len] (the library reports it as an error).
	posFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(ssScannerOf(v).Pos())
	}
	d("pos", posFn)
	d("pointer", posFn)
	d("charpos", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(ssScannerOf(v).CharPos())
	})
	posSet := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		n := int(intArg(args[0]))
		if err := ssScannerOf(v).SetPos(n); err != nil {
			raise("RangeError", "index out of range")
		}
		return object.Integer(n)
	}
	d("pos=", posSet)
	d("pointer=", posSet)

	// rest / rest_size / eos? / beginning_of_line? describe the unscanned tail.
	d("rest", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(ssScannerOf(v).Rest())
	})
	d("rest_size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(ssScannerOf(v).RestSize())
	})
	eosFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(ssScannerOf(v).EOS())
	}
	d("eos?", eosFn)
	d("empty?", eosFn)
	bolFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(ssScannerOf(v).Beginning())
	}
	d("beginning_of_line?", bolFn)
	d("bol?", bolFn)

	// string / string= read and replace the scanned text; << / concat append to
	// it. terminate / reset move the position and return self for chaining.
	d("string", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(ssScannerOf(v).String())
	})
	d("string=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ssScannerOf(v).SetString(strArg(args[0]))
		return args[0]
	})
	concatFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ssScannerOf(v).Concat(strArg(args[0]))
		return v
	}
	d("<<", concatFn)
	d("concat", concatFn)
	d("terminate", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		ssScannerOf(v).Terminate()
		return v
	})
	clearFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		ssScannerOf(v).Reset()
		return v
	}
	d("reset", clearFn)
	d("clear", clearFn)

	// unscan undoes the last advancing match, raising StringScanner::Error when
	// there is nothing to undo.
	d("unscan", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := ssScannerOf(v).Unscan(); err != nil {
			raise("StringScanner::Error", "%s", err.Error())
		}
		return v
	})
}

// ssNamedGroup reads a named capture for StringScanner#[]: it returns the group
// text when present, but raises IndexError when no group has that name — MRI's
// StringScanner#[] distinguishes an undefined NAME (IndexError) from an
// out-of-range integer index (nil). The library answers both with (.,false), so
// the name case is disambiguated here.
func ssNamedGroup(sc *strscan.Scanner, name string) object.Value {
	if text, ok := sc.GroupName(name); ok {
		return object.NewString(text)
	}
	raise("IndexError", "undefined group name reference: %s", name)
	return object.NilV
}
