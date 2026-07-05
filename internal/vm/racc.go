// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	racc "github.com/go-ruby-racc/racc"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerRacc installs the Racc module (require "racc/parser"): the
// Racc::Parser mixin a racc-generated parser `include`s, and Racc::ParseError <
// StandardError. A generated parser embeds its parse tables in a `Racc_arg`
// constant, its reduce actions as `_reduce_N` methods and its lexer as
// `next_token`, then drives the automaton with do_parse / yyparse. Those two
// entry points build a racc.Tables from `Racc_arg` and run the
// github.com/go-ruby-racc/racc engine, calling back through three seams that are
// the parser's own Ruby code — next_token (the lexer), _reduce_N (the reduce
// dispatch) and on_error (the error handler). The pure-compute LALR(1) engine
// lives in the library; this file is the class wiring, the Ruby⇄Go table/value
// conversions' driver, and the seam adapters (see racc_bind.go for the
// conversions themselves).
//
// Bound: the table-driven parse loop (shift/reduce/accept/error + recovery) via
// do_parse and yyparse(recv, mid), the three seams, the default on_error /
// token_to_str / next_token, and full Racc_arg table decoding. Deferred (noted
// in the report): the in-action control methods yyerror / yyaccept / yyerrok
// (they require threading the live engine handle into a Ruby reduce action) and
// MRI's internal _racc_do_parse_rb / _racc_yyparse_rb plumbing — a generated
// parser reaches the engine through do_parse / yyparse, which are bound.
func (vm *VM) registerRacc() {
	mod := newClass("Racc", nil)
	mod.isModule = true
	vm.consts["Racc"] = mod

	// Racc::ParseError < StandardError — what the default on_error raises and a
	// generated parser rescues, matching the gem's `ParseError < StandardError`.
	std := vm.consts["StandardError"].(*RClass)
	perr := newClass("Racc::ParseError", std)
	mod.consts["ParseError"] = perr
	vm.consts["Racc::ParseError"] = perr

	// Racc::Parser is a module: a generated parser class `include`s it to inherit
	// do_parse / yyparse and the default seam methods.
	parser := newClass("Racc::Parser", nil)
	parser.isModule = true
	mod.consts["Parser"] = parser
	vm.consts["Racc::Parser"] = parser

	parser.define("do_parse", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.raccDoParse(self)
	})
	parser.define("yyparse", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.raccYyparse(self, args)
	})

	// The default seams a generated parser overrides. next_token has no default
	// on MRI beyond raising; on_error raises Racc::ParseError with MRI's message;
	// token_to_str reads the parser's Racc_token_to_s_table constant.
	parser.define("next_token", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return raise("NotImplementedError", "#next_token is not defined")
	})
	parser.define("on_error", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.raccOnError(self, args)
	})
	parser.define("token_to_str", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.raccTokenToStr(self, args)
	})
}

// raccParser builds the engine for a parser instance: it reads the `Racc_arg`
// constant off the instance's class, decodes it into a racc.Tables, and wires
// the Reduce and OnError seams to the instance's Ruby methods. do_parse adds the
// NextToken seam; yyparse supplies the token iterator directly. A missing or
// malformed table raises, matching a parser whose generated preamble never ran.
func (vm *VM) raccParser(self object.Value) *racc.Parser {
	cls := vm.classOf(self)
	argv, ok := vm.constInAncestors(cls, "Racc_arg")
	if !ok {
		raise("NameError", "uninitialized constant %s::Racc_arg", cls.ToS())
	}
	tables, methods, ok := raccBuildTables(argv)
	if !ok {
		raise("ArgumentError", "malformed Racc_arg parse table")
	}
	p := &racc.Parser{Tables: tables}

	// Reduce seam: dispatch the rule's generated `_reduce_N(val, _values, result)`
	// method. The library hands back the rule index as the method id; methods maps
	// it to the Ruby method name. _values (the live value stack) is not exposed by
	// the engine, so it is passed as an empty Array — the near-universal case,
	// since generated actions read val and result.
	p.Reduce = func(methodID int, values []any, result any) any {
		return vm.send(self, methods[methodID],
			[]object.Value{raccValArray(values), object.NewArray(), raccToVal(result)}, nil)
	}

	// OnError seam: call the parser's on_error. Returning normally enters the
	// engine's error-recovery mode (MRI's on_error returning); the default
	// on_error raises Racc::ParseError, which propagates out as a Ruby exception.
	p.OnError = func(tok int, val any, stack []any) error {
		vm.send(self, "on_error",
			[]object.Value{object.IntValue(int64(tok)), raccToVal(val), raccValArray(stack)}, nil)
		return nil
	}
	return p
}

// raccDoParse implements Racc::Parser#do_parse: run the engine over the parser's
// next_token lexer and return the accepted result (Ruby nil on a recovered/failed
// parse that does not raise).
func (vm *VM) raccDoParse(self object.Value) object.Value {
	p := vm.raccParser(self)
	p.NextToken = func() (any, any) {
		sym, val := raccDecodeToken(vm.send(self, "next_token", nil, nil))
		return sym, val
	}
	return raccFinish(p.DoParse())
}

// raccYyparse implements Racc::Parser#yyparse(recv=nil, mid=nil): run the engine
// over the tokens recv.mid yields. recv defaults to the parser itself and mid to
// :next_token, mirroring the gem; the block passed to recv.mid feeds each yielded
// [tok, val] into the engine's iterator seam.
func (vm *VM) raccYyparse(self object.Value, args []object.Value) object.Value {
	p := vm.raccParser(self)

	recv := self
	if len(args) > 0 && !object.IsNil(args[0]) {
		recv = args[0]
	}
	mid := "next_token"
	if len(args) > 1 && !object.IsNil(args[1]) {
		if name := raccSymName(args[1]); name != "" {
			mid = name
		}
	}

	res, err := p.Yyparse(func(yield func(sym any, val any)) {
		blk := &Proc{nativeArity: -1, native: func(_ *VM, a []object.Value) object.Value {
			sym, val := raccDecodeYield(a)
			yield(sym, val)
			return object.NilV
		}}
		vm.send(recv, mid, nil, blk)
	})
	return raccFinish(res, err)
}

// raccFinish maps the engine's (result, error) return onto Ruby: a non-nil error
// (a *racc.ParseError the engine produced without a seam raising) becomes a
// Racc::ParseError; otherwise the accepted value is lifted to a Ruby Value. The
// default on_error raises before the engine returns, so the error arm is the
// belt-and-braces path for an engine-level failure.
func raccFinish(res any, err error) object.Value {
	if err != nil {
		raise("Racc::ParseError", "%s", err.Error())
	}
	return raccToVal(res)
}

// raccOnError is the default Racc::Parser#on_error: raise Racc::ParseError with
// MRI's "parse error on value <val.inspect> (<token_to_str or '?'>)" message. A
// generated parser overrides it for custom recovery.
func (vm *VM) raccOnError(self object.Value, args []object.Value) object.Value {
	var tok int64
	if len(args) > 0 {
		tok = intArg(args[0])
	}
	val := object.Value(object.NilV)
	if len(args) > 1 {
		val = args[1]
	}
	ts := "?"
	if s, ok := vm.send(self, "token_to_str", []object.Value{object.IntValue(tok)}, nil).(*object.String); ok {
		ts = s.Str()
	}
	return raise("Racc::ParseError", "parse error on value %s (%s)", val.Inspect(), ts)
}

// raccTokenToStr is Racc::Parser#token_to_str: return the display string for an
// internal token id from the parser's Racc_token_to_s_table constant, or Ruby
// nil when there is no table or the id is out of range (MRI returns nil there).
func (vm *VM) raccTokenToStr(self object.Value, args []object.Value) object.Value {
	if len(args) == 0 {
		return object.NilV
	}
	tok := intArg(args[0])
	tbl, ok := vm.constInAncestors(vm.classOf(self), "Racc_token_to_s_table")
	if !ok {
		return object.NilV
	}
	arr, ok := tbl.(*object.Array)
	if !ok || tok < 0 || tok >= int64(len(arr.Elems)) {
		return object.NilV
	}
	return arr.Elems[tok]
}
