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
	// "#<StringScanner pos/len [\"pre\" ]@ \"post\">" where the pre window is the
	// (up to five) bytes immediately before the position — "..."-prefixed when
	// more than five precede it and omitted entirely at the start — and the post
	// window is the (up to five) bytes from the position, "..."-suffixed when more
	// than five remain. The windows are byte slices, exactly as MRI shows them.
	if s.sc.EOS() {
		return "#<StringScanner fin>"
	}
	str := s.sc.String()
	pos := s.sc.Pos()

	pre := ""
	if pos > 0 {
		window := str[:pos]
		if pos > 5 {
			window = "..." + window[len(window)-5:]
		}
		pre = strscanInspectStr(window) + " "
	}

	post := str[pos:]
	if len(post) > 5 {
		post = post[:5] + "..."
	}

	return fmt.Sprintf("#<StringScanner %d/%d %s@ %s>",
		pos, len(str), pre, strscanInspectStr(post))
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
func ssScannerOf(v object.Value) *strscan.Scanner { return object.Kind[*StringScanner](v).sc }

// ssPattern converts a Ruby scan/skip/match pattern into the regex source string
// the library compiles. A Regexp contributes its source with an inline (?imx)
// flag prefix — exactly the form rbgo's own compileRegexp feeds go-ruby-regexp,
// so the flags carry through. A String is matched literally, as MRI does, by
// escaping its regex metacharacters. Any other value is coerced via to_str the
// way MRI requires a string-like pattern.
func ssPattern(vm *VM, v object.Value) string {
	{
		__sw169 := v
		switch {
		case object.IsKind[*Regexp](__sw169):
			p := object.Kind[*Regexp](__sw169)
			_ = p
			if imx := sortIMX(p.flags); imx != "" {
				return "(?" + imx + ")" + p.source
			}
			return p.source
		case object.IsKind[*object.String](__sw169):
			p := object.Kind[*object.String](__sw169)
			_ = p
			return regexpEscapeLiteral(p.Str())
		default:
			p := __sw169
			_ = p
			if vm.respondsTo(v, "to_str") {
				if str, ok := object.KindOK[*object.String](vm.send(v, "to_str", nil, nil)); ok {
					return regexpEscapeLiteral(str.Str())
				}
			}
			raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
			return ""
		}
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
	return object.IntValue(int64(n))
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
	std, _ := object.KindOK[*RClass](vm.consts["StandardError"])
	ssErr := newClass("StringScanner::Error", std)
	cls.consts["Error"] = ssErr
	vm.consts["StringScanner::Error"] = ssErr

	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			// MRI's StringScanner.new(string[, fixed_anchor:]) requires the string
			// argument (0 args is an ArgumentError) and coerces it through String()
			// — a nil/non-string therefore raises TypeError, not a silent empty
			// scanner. Any trailing argument (the historical dup / fixed_anchor flag)
			// does not affect the scanned content and is ignored.
			if len(args) == 0 || len(args) > 2 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
			}
			return &StringScanner{sc: strscan.New(strArg(args[0]))}
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
		return object.IntValue(int64(n))
	})

	// [] reads a capture of the most recent match: an Integer index via Group, a
	// Symbol/String name via GroupName. An out-of-range integer index returns nil,
	// but an undefined NAME raises IndexError — matching MRI's StringScanner#[].
	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		sc := ssScannerOf(v)
		{
			__sw170 := args[0]
			switch {
			case object.IsInt(__sw170):
				k := object.AsInteger(__sw170)
				_ = k
				text, ok := sc.Group(int(k))
				if !ok {
					return object.NilV
				}
				return object.NewString(text)
			case object.IsKind[object.Symbol](__sw170):
				k := object.Kind[object.Symbol](__sw170)
				_ = k
				return ssNamedGroup(sc, string(k))
			case object.IsKind[*object.String](__sw170):
				k := object.Kind[*object.String](__sw170)
				_ = k
				return ssNamedGroup(sc, k.Str())
			default:
				k := __sw170
				_ = k
				raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(args[0]))
				return object.NilV
			}
		}
	})

	// peek(n) returns up to n bytes from the current position without advancing.
	d("peek", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(ssScannerOf(v).Peek(int(intArg(args[0]))))
	})

	// pos / charpos report the position; pos= moves it, raising RangeError when
	// the target is outside [0, len] (the library reports it as an error).
	posFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(ssScannerOf(v).Pos()))
	}
	d("pos", posFn)
	d("pointer", posFn)
	d("charpos", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(ssScannerOf(v).CharPos()))
	})
	posSet := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		n := int(intArg(args[0]))
		if err := ssScannerOf(v).SetPos(n); err != nil {
			raise("RangeError", "index out of range")
		}
		return object.IntValue(int64(n))
	}
	d("pos=", posSet)
	d("pointer=", posSet)

	// rest / rest_size / eos? / beginning_of_line? describe the unscanned tail.
	d("rest", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(ssScannerOf(v).Rest())
	})
	d("rest_size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(ssScannerOf(v).RestSize()))
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
