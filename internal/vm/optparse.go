// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
	optparse "github.com/go-ruby-optparse/optparse"
)

// optGetoptsScan mirrors the library's getopts scan grammar, so the result Hash
// keeps MRI's spec order ("ab" is one name, a trailing ":" takes an argument).
var optGetoptsScan = regexp.MustCompile(`([a-zA-Z0-9][a-zA-Z0-9_-]*)(:)?`)

// OptionParser binds github.com/go-ruby-optparse/optparse — a pure-Go (cgo-free)
// reimplementation of the deterministic core of Ruby's OptionParser (require
// "optparse") — into rbgo, replacing the former pure-Ruby prelude implementation.
// The argv-parsing ENGINE (option specs, long/short/abbreviation matching, the
// =/bundled/--[no-]/optional-argument forms, Integer/Float/Array/list coercion,
// and the full OptionParser::ParseError taxonomy with MRI-exact messages) lives
// entirely in the library, ported from that prelude and validated against MRI
// 4.0.5. This file is the thin host wrapper: each on(*args, &block) call splits
// the Ruby block out (the library never runs blocks) and feeds the flag/coerce
// strings to optparse.MakeSpec/On; parse!/order!/permute!/getopts then run the
// library and dispatch the stored Ruby block for every returned Match, in order,
// with the coerced Value mapped back to a Ruby object. The ParseError class tree
// (the Ruby-object surface tests and Puppet's recover path touch) stays in the
// prelude; the parsing itself is the library.
//
// The block-holding / Class-or-Array/Hash-coerce surface is the part that cannot
// live in the interpreter-independent library, so it is held here per parser: the
// blocks slice (indexed by spec index) and, for a candidate list, the original
// Ruby candidate objects so a matched key maps back to the very Symbol/String/…
// the program passed (MRI returns the original object, e.g. :big, not its name).
type OptionParser struct {
	p      *optparse.Parser
	blocks []*Proc
	// listVals[i] holds, for spec index i declared with an Array/Hash coercion,
	// the per-candidate Ruby value to report when that candidate matches (parallel
	// to listKeys[i]). Absent for a spec without a candidate list.
	listVals map[int][]object.Value
	// listKeys[i] holds the candidate key strings parallel to listVals[i] — the
	// library reports a matched key string, which is mapped back through these to
	// the original Ruby object.
	listKeys map[int][]string
	// acceptables records accept/reject for the chainable Ruby surface; the
	// library owns the built-in converters, so the stored block is never run.
	acceptables map[*RClass]*Proc
	programName *string
	version     *string
	release     *string
	defaultArgv *object.Array
}

func (o *OptionParser) ToS() string     { return "#<OptionParser>" }
func (o *OptionParser) Inspect() string { return o.ToS() }
func (o *OptionParser) Truthy() bool    { return true }

// opOf returns the receiver as a *OptionParser.
func opOf(v object.Value) *OptionParser { return object.Kind[*OptionParser](v) }

// registerOptionParser installs the native OptionParser class backed by the
// go-ruby-optparse library. OptionParser.new builds a fresh library Parser (with
// MRI's summary defaults), optionally taking a banner and yielding itself to a
// block. The declaration methods (on / on_head / define / separator / accept /
// reject) and the attribute accessors return self so they stay chainable, exactly
// as the prelude did. parse!/order!/permute!/getopts forward to the library and
// dispatch the stored Ruby blocks.
func (vm *VM) registerOptionParser() {
	cls := newClass("OptionParser", vm.cObject)
	vm.cOptionParser = cls
	vm.consts["OptionParser"] = object.Wrap(cls)
	vm.consts["OptParse"] = object.Wrap(cls)
	cls.consts["OptParse"] = object.Wrap(cls)

	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			op := &OptionParser{
				p:           optparse.New(),
				listVals:    map[int][]object.Value{},
				listKeys:    map[int][]string{},
				acceptables: map[*RClass]*Proc{},
				defaultArgv: object.NewArray(),
			}
			// OptionParser.new(banner=nil, width=32, indent="    "): a non-nil banner
			// becomes the first help line; the width/indent override the summary
			// layout. Each is optional and a nil placeholder keeps the default.
			if len(args) >= 1 {
				if s, ok := object.KindOK[*object.String](args[0]); ok {
					op.p.Banner = s.Str()
				}
			}
			if len(args) >= 2 {
				if n, ok := object.AsIntegerOK(args[1]); ok {
					op.p.SummaryWidth = int(n)
				}
			}
			if len(args) >= 3 {
				if s, ok := object.KindOK[*object.String](args[2]); ok {
					op.p.SummaryIndent = s.Str()
				}
			}
			if blk != nil {
				vm.callBlock(blk, []object.Value{object.Wrap(op)})
			}
			return object.Wrap(op)
		}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// --- declaration: on / on_head / on_tail / define / def_option -----------
	// Each splits the &block out (held here), builds a Spec from the string flags
	// plus a Class/Array/Hash positional, registers it, and returns self.
	onFn := func(head bool) NativeFn {
		return func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
			op := opOf(v)
			spec, keys, vals := vm.optSpecFromArgs(args)
			var idx int
			if head {
				idx = op.p.OnHead(spec)
			} else {
				idx = op.p.On(spec)
			}
			// Grow the blocks slice to cover the assigned index (on_head can insert
			// at a non-tail position in the help list but the spec index is still
			// monotonic, so a simple append-by-index keeps blocks aligned).
			for len(op.blocks) <= idx {
				op.blocks = append(op.blocks, nil)
			}
			op.blocks[idx] = blk
			if vals != nil {
				op.listVals[idx] = vals
				op.listKeys[idx] = keys
			}
			return v
		}
	}
	onTail := onFn(false)
	d("on", onTail)
	d("on_tail", onTail)
	d("define", onTail)
	d("def_option", onTail)
	d("on_head", onFn(true))

	d("separator", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		text := ""
		if len(args) > 0 {
			if s, ok := object.KindOK[*object.String](args[0]); ok {
				text = s.Str()
			}
		}
		opOf(v).p.Separator(text)
		return v
	})

	// accept/reject keep the chainable Ruby surface; the library owns the built-in
	// converters, so a custom accept block is recorded but never consulted.
	d("accept", func(_ *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if c, ok := object.KindOK[*RClass](args[0]); ok {
			opOf(v).acceptables[c] = blk
		}
		return v
	})
	d("reject", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if c, ok := object.KindOK[*RClass](args[0]); ok {
			delete(opOf(v).acceptables, c)
		}
		return v
	})

	// --- attributes ----------------------------------------------------------
	d("banner", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		op := opOf(v)
		if op.p.Banner == "" {
			return object.NilVal()
		}
		return object.Wrap(object.NewString(op.p.Banner))
	})
	d("banner=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		opOf(v).p.Banner = strArg(args[0])
		return args[0]
	})
	d("summary_width", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(opOf(v).p.SummaryWidth))
	})
	d("summary_width=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		opOf(v).p.SummaryWidth = int(intArg(args[0]))
		return args[0]
	})
	d("summary_indent", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(opOf(v).p.SummaryIndent))
	})
	d("summary_indent=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		opOf(v).p.SummaryIndent = strArg(args[0])
		return args[0]
	})
	d("default_argv", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(opOf(v).defaultArgv)
	})
	d("default_argv=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if a, ok := object.KindOK[*object.Array](args[0]); ok {
			opOf(v).defaultArgv = a
		}
		return args[0]
	})

	// program_name defaults to $0's basename (or "optparse"), like MRI; an explicit
	// program_name= overrides it and feeds the library's default banner.
	d("program_name", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		op := opOf(v)
		if op.programName != nil {
			return object.Wrap(object.NewString(*op.programName))
		}
		return object.Wrap(object.NewString(vm.optDefaultProgramName()))
	})
	d("program_name=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		op := opOf(v)
		s := strArg(args[0])
		op.programName = &s
		op.p.ProgramName = s
		return args[0]
	})
	d("version", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return optStrPtr(opOf(v).version)
	})
	d("version=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := strArg(args[0])
		opOf(v).version = &s
		return args[0]
	})
	d("release", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return optStrPtr(opOf(v).release)
	})
	d("release=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := strArg(args[0])
		opOf(v).release = &s
		return args[0]
	})
	// ver renders "<program_name> <version> (<release>)" (release in parens, when
	// set), or nil when no version is set — MRI's OptionParser#ver.
	d("ver", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		op := opOf(v)
		if op.version == nil {
			return object.NilVal()
		}
		name := vm.optDefaultProgramName()
		if op.programName != nil {
			name = *op.programName
		}
		str := name + " " + *op.version
		if op.release != nil {
			str += " (" + *op.release + ")"
		}
		return object.Wrap(object.NewString(str))
	})

	// --- help ----------------------------------------------------------------
	helpFn := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		opOf(v).p.ProgramName = vm.optProgramNameFor(opOf(v))
		return object.Wrap(object.NewString(opOf(v).p.Help()))
	}
	d("help", helpFn)
	d("to_s", helpFn)
	d("summarize", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		lines := opOf(v).p.Summarize()
		out := make([]object.Value, len(lines))
		for i, l := range lines {
			out[i] = object.Wrap(object.NewString(l))
		}
		return object.Wrap(object.NewArrayFromSlice(out))
	})

	// --- parsing -------------------------------------------------------------
	// parse!/permute! parse anywhere; order! stops at the first non-option (or, with
	// a block, sinks each leading non-option). Each mutates the supplied argv array
	// in place to the leftover operands and returns it, dispatching the stored block
	// for every match in order, then translating a *ParseError into the matching
	// Ruby OptionParser::* exception.
	bangFn := func(perm bool) NativeFn {
		return func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
			op := opOf(v)
			argv := op.argvArg(args)
			var matches []optparse.Match
			var rest []string
			var err error
			if perm {
				matches, rest, err = op.p.ParseBang(stringsOf(argv))
			} else {
				var sink func(string)
				if blk != nil {
					sink = func(s string) { vm.callBlock(blk, []object.Value{object.Wrap(object.NewString(s))}) }
				}
				matches, rest, err = op.p.Order(stringsOf(argv), sink)
			}
			vm.optDispatch(op, matches)
			if err != nil {
				vm.optRaise(err)
			}
			setArgv(argv, rest)
			return object.Wrap(argv)
		}
	}
	d("parse!", bangFn(true))
	d("permute!", bangFn(true))
	d("order!", bangFn(false))

	// parse/permute/order are the non-mutating forms: they parse a dup and return
	// the leftover operands as a fresh array (the source argv is untouched).
	dupFn := func(perm bool) NativeFn {
		return func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
			op := opOf(v)
			src := op.argvArg(args)
			in := stringsOf(src)
			var matches []optparse.Match
			var rest []string
			var err error
			if perm {
				matches, rest, err = op.p.ParseBang(in)
			} else {
				var sink func(string)
				if blk != nil {
					sink = func(s string) { vm.callBlock(blk, []object.Value{object.Wrap(object.NewString(s))}) }
				}
				matches, rest, err = op.p.Order(in, sink)
			}
			vm.optDispatch(op, matches)
			if err != nil {
				vm.optRaise(err)
			}
			return object.Wrap(strArray(rest))
		}
	}
	d("parse", dupFn(true))
	d("permute", dupFn(true))
	d("order", dupFn(false))

	// getopts registers the spec letters/words, parses argv, and returns the
	// name→value Hash (true/false for flags, the string or nil for arg options),
	// mutating argv to the leftover operands.
	d("getopts", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		op := opOf(v)
		var argv *object.Array
		rest := args
		if len(args) > 0 {
			if a, ok := object.KindOK[*object.Array](args[0]); ok {
				argv = a
				rest = args[1:]
			}
		}
		if argv == nil {
			argv = op.defaultArgv
		}
		specs := make([]string, len(rest))
		for i, s := range rest {
			specs[i] = strArg(s)
		}
		result, err := op.p.Getopts(stringsOf(argv), specs...)
		if err != nil {
			vm.optRaise(err)
		}
		h := object.NewHash()
		// MRI's getopts returns the entries in spec order; Getopts builds them in
		// the same order it scanned, so iterate the spec strings to preserve it.
		for _, name := range optGetoptsNames(specs) {
			r := result[name]
			var val object.Value
			switch {
			case r.TakesArg && r.Str != nil:
				val = object.Wrap(object.NewString(*r.Str))
			case r.TakesArg:
				val = object.NilVal()
			default:
				val = object.BoolValue(bool(object.Bool(r.Flag)))
			}
			h.Set(object.Wrap(object.NewString(name)), val)
		}
		// getopts (no leftover form is bang) mutates the source argv: re-run to get
		// the rest. The library already consumed argv; the operands fall out of the
		// same parse, so recompute from a fresh ParseBang is wasteful — instead the
		// Getopts call above left the rest implicit. Recover it by parsing rest off
		// the same registered parser is unnecessary; getopts in MRI uses parse!,
		// which mutates ARGV to the operands. Mirror that here.
		_, leftover, _ := op.p.ParseBang(stringsOf(argv))
		setArgv(argv, leftover)
		return object.Wrap(h)
	})
}

// optDispatch invokes the stored Ruby block for every match, in order, with the
// coerced Value mapped to a Ruby object. A spec declared with no block (on(...)
// without &block) is skipped — MRI simply records the value with nowhere to send
// it.
func (vm *VM) optDispatch(op *OptionParser, matches []optparse.Match) {
	for _, m := range matches {
		blk := op.blocks[m.SpecIndex]
		if blk == nil {
			continue
		}
		vm.callBlock(blk, []object.Value{vm.optValueToRuby(op, m)})
	}
}

// optValueToRuby maps a library Match value to the Ruby object the block receives:
// bool→true/false, nil→nil, int64/*big.Int→Integer, float64→Float, []string→Array
// of String, and a candidate-list string back to the original Ruby candidate
// object (so on("--s S",[:big]) yields :big, not "big").
func (vm *VM) optValueToRuby(op *OptionParser, m optparse.Match) object.Value {
	switch val := m.Value.(type) {
	case nil:
		return object.NilVal()
	case bool:
		return object.BoolValue(bool(object.Bool(val)))
	case string:
		// A coerced string is either a plain value or a candidate-list key; for a
		// list, map it back to the original Ruby object via the per-spec table.
		if vals, ok := op.listVals[m.SpecIndex]; ok {
			if rv := optListLookup(op, m.SpecIndex, val, vals); !object.IsNil(rv) {
				return rv
			}
		}
		return object.Wrap(object.NewString(val))
	case int64:
		return object.IntValue(val)
	case *big.Int:
		return object.NormInt(new(big.Int).Set(val))
	case float64:
		return object.FloatValue(float64(object.Float(val)))
	case []string:
		return object.Wrap(strArray(val))
	default:
		return object.NilVal()
	}
}

// optListLookup returns the original Ruby candidate value whose key (the library's
// candidate string) equals the matched string, or nil when no key matched (the
// matched string is itself the value to report).
func optListLookup(op *OptionParser, idx int, matched string, vals []object.Value) object.Value {
	for i, k := range op.listKeys[idx] {
		if k == matched {
			return vals[i]
		}
	}
	return object.NilVal()
}

// optSpecFromArgs splits an on(*args) argument list into the library Spec and,
// when a candidate Array/Hash was supplied, the parallel Ruby candidate objects to
// report on a match. Strings starting with a dash (or carrying an arg descriptor)
// are flag forms; a Class is a built-in coercion; an Array/Hash is a candidate
// list; any other String is a description line.
func (vm *VM) optSpecFromArgs(args []object.Value) (optparse.Spec, []string, []object.Value) {
	var opts []string
	coerce := optparse.CoerceNone
	var list, values []string
	var ruby []object.Value
	for _, a := range args {
		{
			__sw115 := a
			switch {
			case object.IsKind[*object.String](__sw115):
				v := object.Kind[*object.String](__sw115)
				_ = v
				opts = append(opts, v.Str())
			case object.IsKind[*RClass](__sw115):
				v := object.Kind[*RClass](__sw115)
				_ = v
				coerce = vm.optCoerceForClass(v)
			case object.IsKind[*object.Array](__sw115):
				v := object.Kind[*object.Array](__sw115)
				_ = v
				coerce = optparse.CoerceList
				list, values, ruby = optListFromArray(vm, v)
			case object.IsKind[*object.Hash](__sw115):
				v := object.Kind[*object.Hash](__sw115)
				_ = v
				coerce = optparse.CoerceList
				list, values, ruby = optListFromHash(vm, v)
			}
		}
	}
	spec := optparse.MakeSpec(opts, coerce, list, values)
	return spec, list, ruby
}

// optCoerceForClass maps a Ruby Class positional to the library coercion name. The
// built-ins (Integer/Float/Array/String) name their converter; anything else
// (Object/NilClass or a user class) is the identity String accept.
func (vm *VM) optCoerceForClass(c *RClass) string {
	switch c {
	case vm.cInteger:
		return optparse.CoerceInteger
	case vm.cFloat:
		return optparse.CoerceFloat
	case vm.cArray:
		return optparse.CoerceArray
	case vm.cString:
		return optparse.CoerceString
	default:
		return optparse.CoerceString
	}
}

// optListFromArray turns an Array candidate set into the library's parallel keys
// (the to_s of each element) and the Ruby objects to report. Values is left equal
// to the keys so the library reports the key string, which optValueToRuby maps
// back to the original object.
func optListFromArray(vm *VM, a *object.Array) (keys, values []string, ruby []object.Value) {
	for _, e := range a.Elems {
		k := optToS(vm, e)
		keys = append(keys, k)
		values = append(values, k)
		ruby = append(ruby, e)
	}
	return
}

// optListFromHash turns a Hash candidate map (MRI's on("--x X", {"a"=>1}) form)
// into keys (the to_s of each Hash key) with the Hash value as the reported Ruby
// object.
func optListFromHash(vm *VM, h *object.Hash) (keys, values []string, ruby []object.Value) {
	for _, k := range h.Keys {
		ks := optToS(vm, k)
		val, _ := h.Get(k)
		keys = append(keys, ks)
		values = append(values, ks)
		ruby = append(ruby, val)
	}
	return
}

// optToS renders a candidate value's key string the way the prelude did (k.to_s):
// a Symbol/String contributes its text, anything else its #to_s.
func optToS(vm *VM, v object.Value) string {
	{
		__sw116 := v
		switch {
		case object.IsKind[object.Symbol](__sw116):
			t := object.Kind[object.Symbol](__sw116)
			_ = t
			return string(t)
		case object.IsKind[*object.String](__sw116):
			t := object.Kind[*object.String](__sw116)
			_ = t
			return t.Str()
		default:
			t := __sw116
			_ = t
			return strArg(vm.send(v, "to_s", nil, nil))
		}
	}
}

// optRaise translates a library *ParseError into the matching Ruby
// OptionParser::* exception, constructed through the Ruby class so the raised
// object carries the same args/reason/message/recover surface the prelude exposes.
func (vm *VM) optRaise(err error) {
	pe, ok := err.(*optparse.ParseError)
	if !ok {
		raise("OptionParser::ParseError", "%s", err.Error())
		return
	}
	// pe.Class() is "OptionParser::<Name>"; the subclasses live in the OptionParser
	// class's own constant table (nested), so resolve <Name> there.
	name := strings.TrimPrefix(pe.Class(), "OptionParser::")
	cls, ok := object.KindOK[*RClass](vm.cOptionParser.consts[name])
	if !ok {
		raise("OptionParser::ParseError", "%s", pe.Error())
		return
	}
	argv := make([]object.Value, len(pe.Args))
	for i, a := range pe.Args {
		argv[i] = object.Wrap(object.NewString(a))
	}
	exc := vm.send(object.Wrap(cls), "new", argv, nil)
	panic(vm.excError(vm.captureBacktrace(exc)))
}

// optDefaultProgramName is MRI's File.basename($0 || "optparse"): the basename of
// the program global, or "optparse" when it is unset.
func (vm *VM) optDefaultProgramName() string {
	name := "optparse"
	if v, set := vm.globals["$0"]; set {
		if s, ok := object.KindOK[*object.String](v); ok && s.Str() != "" {
			name = s.Str()
		}
	}
	return filepath.Base(name)
}

// optProgramNameFor resolves the program name the library should print in a
// default banner: an explicit program_name, else the $0 basename default.
func (vm *VM) optProgramNameFor(op *OptionParser) string {
	if op.programName != nil {
		return *op.programName
	}
	return vm.optDefaultProgramName()
}

// argvArg returns the argv array a parse call operates on: the first Array
// argument, or the parser's default_argv when none is given.
func (op *OptionParser) argvArg(args []object.Value) *object.Array {
	if len(args) > 0 {
		if a, ok := object.KindOK[*object.Array](args[0]); ok {
			return a
		}
	}
	return op.defaultArgv
}

// stringsOf snapshots an Array's String elements into a []string for the library.
func stringsOf(a *object.Array) []string {
	out := make([]string, len(a.Elems))
	for i, e := range a.Elems {
		out[i] = strArg(e)
	}
	return out
}

// setArgv replaces an Array's contents in place with the given strings, the way
// MRI's parse! rewrites ARGV to the leftover operands.
func setArgv(a *object.Array, rest []string) {
	a.Elems = a.Elems[:0]
	for _, s := range rest {
		a.Elems = append(a.Elems, object.Wrap(object.NewString(s)))
	}
}

// strArray builds a fresh Ruby Array of String from a []string.
func strArray(ss []string) *object.Array {
	out := make([]object.Value, len(ss))
	for i, s := range ss {
		out[i] = object.Wrap(object.NewString(s))
	}
	return object.NewArrayFromSlice(out)
}

// optStrPtr returns a String for a non-nil *string, or nil.
func optStrPtr(p *string) object.Value {
	if p == nil {
		return object.NilVal()
	}
	return object.Wrap(object.NewString(*p))
}

// optGetoptsNames returns the getopts option names in scan order across the spec
// strings, mirroring the library's getopts scan grammar so the result Hash keeps
// MRI's order.
func optGetoptsNames(specs []string) []string {
	var names []string
	for _, s := range specs {
		for _, m := range optGetoptsScan.FindAllStringSubmatch(s, -1) {
			names = append(names, m[1])
		}
	}
	return names
}
