// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"unicode/utf8"

	pp "github.com/go-ruby-prettyprint/prettyprint"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// PrettyPrint binds github.com/go-ruby-prettyprint/prettyprint — the pure-Go,
// MRI-4.0.5-faithful port of Ruby's `prettyprint` standard library (the
// Wadler/Lindig layout engine) — into rbgo. The library owns the whole layout
// algorithm: group fit-versus-overflow, breakable insertion, nest indentation,
// fill mode and the single-line variant. This file is only the thin shell that
// maps `require "prettyprint"`, the `PrettyPrint` class, its `format` /
// `singleline_format` class methods, and the document-builder instance methods
// (`text`, `breakable`, `group`, `nest`, `fill_breakable`, `flush`,
// `current_group`, `break_outmost_groups`) onto the engine, plus the MRI
// accessors `output`, `maxwidth`, `newline` and `indent`.
//
// MRI's output is a target object seeded with an optional leading string; the
// engine here accumulates into its own internal buffer, so the wrapper keeps the
// caller's seed in prefix and concatenates it ahead of the engine's render when
// output is read. The pp object-inspector that consumes this engine stays in the
// host and is out of scope here — only the engine is wired.

// PrettyPrint is the Ruby wrapper around a *prettyprint.PrettyPrint buffer.
type PrettyPrint struct {
	q        *pp.PrettyPrint // the layout engine
	prefix   string          // the caller-seeded leading output (MRI's output arg)
	maxwidth int             // mirrors the engine's maxwidth (for #maxwidth)
	newline  string          // mirrors the engine's newline (for #newline)
	depth    int             // current group nesting depth (for #current_group)
}

func (p *PrettyPrint) ToS() string     { return "#<PrettyPrint>" }
func (p *PrettyPrint) Inspect() string { return "#<PrettyPrint>" }
func (p *PrettyPrint) Truthy() bool    { return true }

// output renders the caller-seeded prefix followed by the engine's accumulated
// text, the value MRI's PrettyPrint#output (and the return of .format) exposes.
func (p *PrettyPrint) output() string { return p.prefix + p.q.String() }

// PrettyPrintGroup is the wrapper for PrettyPrint#current_group's return value
// (MRI's PrettyPrint::Group). The engine's group is opaque, so the wrapper
// carries the depth the binding tracks across group_sub nesting, which is the
// only field MRI callers read (q.current_group.depth + 1).
type PrettyPrintGroup struct{ depth int }

func (g *PrettyPrintGroup) ToS() string     { return "#<PrettyPrint::Group>" }
func (g *PrettyPrintGroup) Inspect() string { return "#<PrettyPrint::Group>" }
func (g *PrettyPrintGroup) Truthy() bool    { return true }

// ppText coerces a #text/#group obj argument to its string content the way MRI's
// `@output << obj` does: a String contributes its bytes, any other value its #to_s.
func (vm *VM) ppText(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	if s, ok := vm.send(v, "to_s", nil, nil).(*object.String); ok {
		return s.Str()
	}
	return ""
}

// ppWidth picks the width column count for an obj/sep: an explicit width argument
// when given, otherwise the string's character length (MRI's obj.length default).
func ppWidth(s string, args []object.Value, idx int) int {
	if len(args) > idx {
		return int(intArg(args[idx]))
	}
	return utf8.RuneCountInString(s)
}

// newPrettyPrint builds the wrapper, seeding maxwidth/newline (MRI defaults 79
// and "\n") and the engine. prefix carries any caller-supplied leading output.
func newPrettyPrint(prefix string, maxwidth int, newline string) *PrettyPrint {
	return &PrettyPrint{
		q:        pp.New(maxwidth, newline, pp.DefaultGenSpace),
		prefix:   prefix,
		maxwidth: maxwidth,
		newline:  newline,
	}
}

// ppNewArgs reads the (output, maxwidth, newline) argument triple shared by
// PrettyPrint.new and .format, applying MRI's defaults (empty output, 79, "\n").
// The output
// seed is taken from a String argument (nil/anything else seeds an empty prefix).
func (vm *VM) ppNewArgs(args []object.Value) (prefix string, maxwidth int, newline string) {
	maxwidth, newline = 79, "\n"
	if len(args) > 0 {
		if s, ok := args[0].(*object.String); ok {
			prefix = s.Str()
		}
	}
	if len(args) > 1 {
		if _, isNil := args[1].(object.Nil); !isNil {
			maxwidth = int(intArg(args[1]))
		}
	}
	if len(args) > 2 {
		if s, ok := args[2].(*object.String); ok {
			newline = s.Str()
		}
	}
	return prefix, maxwidth, newline
}

// registerPrettyPrint installs the PrettyPrint class, its class methods and the
// builder instance methods, plus the nested PrettyPrint::Group constant.
func (vm *VM) registerPrettyPrint() {
	vm.cPrettyPrint = newClass("PrettyPrint", vm.cObject)
	vm.consts["PrettyPrint"] = vm.cPrettyPrint

	grp := newClass("PrettyPrint::Group", vm.cObject)
	vm.cPrettyPrint.consts["Group"] = grp
	vm.consts["PrettyPrint::Group"] = grp
	grp.define("depth", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(v.(*PrettyPrintGroup).depth)
	})

	// PrettyPrint.new(output='', maxwidth=79, newline="\n").
	vm.cPrettyPrint.smethods["new"] = &Method{name: "new", owner: vm.cPrettyPrint,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			prefix, maxwidth, newline := vm.ppNewArgs(args)
			return newPrettyPrint(prefix, maxwidth, newline)
		}}

	// PrettyPrint.format(output='', maxwidth=79, newline="\n") { |q| ... }: build,
	// yield, flush, and return the output object (MRI's convenience wrapper).
	vm.cPrettyPrint.smethods["format"] = &Method{name: "format", owner: vm.cPrettyPrint,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			if blk == nil {
				raise("LocalJumpError", "no block given (format)")
			}
			prefix, maxwidth, newline := vm.ppNewArgs(args)
			p := newPrettyPrint(prefix, maxwidth, newline)
			vm.callBlock(blk, []object.Value{p})
			p.q.Flush()
			return object.NewString(p.output())
		}}

	// PrettyPrint.singleline_format(output='', ...) { |q| ... }: render with no
	// breaks (breakables become their separator text), returning the output.
	// maxwidth/newline/genspace are ignored, as in MRI.
	vm.cPrettyPrint.smethods["singleline_format"] = &Method{name: "singleline_format", owner: vm.cPrettyPrint,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			if blk == nil {
				raise("LocalJumpError", "no block given (singleline_format)")
			}
			prefix, _, _ := vm.ppNewArgs(args)
			sl := newSingleLine(prefix)
			vm.callBlock(blk, []object.Value{sl})
			return object.NewString(sl.output())
		}}

	d := func(name string, fn NativeFn) { vm.cPrettyPrint.define(name, fn) }
	self := func(v object.Value) *PrettyPrint { return v.(*PrettyPrint) }

	// text(obj, width=obj.length): add obj as width columns of text.
	d("text", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		p := self(v)
		s := vm.ppText(args[0])
		p.q.Text(s, ppWidth(s, args, 1))
		return object.NilV
	})

	// breakable(sep=' ', width=sep.length): a permitted line break, rendering sep
	// (width columns) when the enclosing group is laid out on one line.
	d("breakable", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		p := self(v)
		sep := " "
		if len(args) > 0 {
			sep = vm.ppText(args[0])
		}
		p.q.Breakable(sep, ppWidth(sep, args, 1))
		return object.NilV
	})

	// nest(indent) { ... }: increase the left margin by indent for breaks added in
	// the block.
	d("nest", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (nest)")
		}
		p := self(v)
		indent := int(intArg(args[0]))
		p.q.Nest(indent, func() { vm.callBlock(blk, nil) })
		return object.NilV
	})

	// group(indent=0, open_obj='', close_obj='', open_width=open_obj.length,
	// close_width=close_obj.length) { ... }: group the breakables in the block (all
	// break or none), optionally bracketing with open/close text and nesting indent.
	d("group", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (group)")
		}
		p := self(v)
		indent := 0
		if len(args) > 0 {
			indent = int(intArg(args[0]))
		}
		openObj, closeObj := "", ""
		if len(args) > 1 {
			openObj = vm.ppText(args[1])
		}
		if len(args) > 2 {
			closeObj = vm.ppText(args[2])
		}
		openWidth, closeWidth := ppWidth(openObj, args, 3), ppWidth(closeObj, args, 4)
		p.depth++
		p.q.Group(indent, openObj, openWidth, closeObj, closeWidth, func() { vm.callBlock(blk, nil) })
		p.depth--
		return object.NilV
	})

	// fill_breakable(sep=' ', width=sep.length): a breakable whose break decision is
	// made individually at this point (it groups a single breakable).
	d("fill_breakable", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		p := self(v)
		sep := " "
		if len(args) > 0 {
			sep = vm.ppText(args[0])
		}
		p.q.FillBreakable(sep, ppWidth(sep, args, 1))
		return object.NilV
	})

	// flush: drain the buffered document into the output.
	d("flush", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).q.Flush()
		return object.NilV
	})

	// break_outmost_groups: break the buffer into lines shorter than maxwidth.
	d("break_outmost_groups", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).q.BreakOutmostGroups()
		return object.NilV
	})

	// current_group: the group most recently pushed on the stack.
	d("current_group", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &PrettyPrintGroup{depth: self(v).depth}
	})

	d("output", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).output())
	})
	d("maxwidth", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self(v).maxwidth)
	})
	d("newline", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).newline)
	})
	d("indent", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self(v).q.Indent())
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ToS())
	})
	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Inspect())
	})
}

// SingleLine is the Ruby wrapper around a *prettyprint.SingleLine — the no-break
// variant used by PrettyPrint.singleline_format. It accepts the same builder
// calls; breakables emit their separator text and groups/nests are transparent.
type SingleLine struct {
	s      *pp.SingleLine
	prefix string
}

func (s *SingleLine) ToS() string     { return "#<PrettyPrint::SingleLine>" }
func (s *SingleLine) Inspect() string { return "#<PrettyPrint::SingleLine>" }
func (s *SingleLine) Truthy() bool    { return true }

func (s *SingleLine) output() string { return s.prefix + s.s.String() }

func newSingleLine(prefix string) *SingleLine {
	return &SingleLine{s: pp.NewSingleLine(), prefix: prefix}
}

// registerSingleLine installs PrettyPrint::SingleLine and its builder methods.
// The block yielded by singleline_format receives one of these.
func (vm *VM) registerSingleLine() {
	cls := newClass("PrettyPrint::SingleLine", vm.cObject)
	vm.cPrettyPrint.consts["SingleLine"] = cls
	vm.consts["PrettyPrint::SingleLine"] = cls

	self := func(v object.Value) *SingleLine { return v.(*SingleLine) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("text", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).s.Text(vm.ppText(args[0]), 0)
		return object.NilV
	})
	d("breakable", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		sep := " "
		if len(args) > 0 {
			sep = vm.ppText(args[0])
		}
		self(v).s.Breakable(sep, 0)
		return object.NilV
	})
	d("nest", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (nest)")
		}
		self(v).s.Nest(0, func() { vm.callBlock(blk, nil) })
		return object.NilV
	})
	d("group", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (group)")
		}
		openObj, closeObj := "", ""
		if len(args) > 1 {
			openObj = vm.ppText(args[1])
		}
		if len(args) > 2 {
			closeObj = vm.ppText(args[2])
		}
		self(v).s.Group(0, openObj, 0, closeObj, 0, func() { vm.callBlock(blk, nil) })
		return object.NilV
	})
	d("flush", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).s.Flush()
		return object.NilV
	})
	d("first?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).s.First())
	})
	d("output", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).output())
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ToS())
	})
	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Inspect())
	})
}
