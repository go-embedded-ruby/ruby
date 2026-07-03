// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"

	rspec "github.com/go-ruby-rspec/rspec"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent matcher / value model of
// github.com/go-ruby-rspec/rspec — the deterministic core of rspec-expectations.
// The match logic and byte-faithful failure messages live in that library; rbgo
// only converts values to and from its `any` model, builds matcher wrappers, and
// drives expect(...).to / .not_to (plus the raise_error block observation, the
// one place a block is evaluated through rbgo). The it / describe / hook runner
// is a separate concern (the eval seam) and is not modelled here.

// --- Ruby value -> rspec value ---------------------------------------------

// rspecFromRuby maps a Ruby value to the go-ruby-rspec value model so a matcher
// sees exactly the shapes it inspects: nil/bool/int64/*big.Int/float64/string,
// a Symbol, an Array ([]any), a Hash (*rspec.Hash, order preserved), a Range
// (*rspec.Range), a Regexp (*rspec.Regexp), a Class (rspec.Class), and any other
// object as an *rspec.Object carrying its class name, ancestry and method names
// so be_a / respond_to / equality reflect faithfully.
func rspecFromRuby(vm *VM, v object.Value) any {
	{
		__sw145 := v
		switch {
		case __sw145 == nil || object.IsNilObj(__sw145):
			n := __sw145
			_ = n
			return nil
		case object.IsBool(__sw145):
			n := object.AsBoolV(__sw145)
			_ = n
			return bool(n)
		case object.IsInt(__sw145):
			n := object.AsInteger(__sw145)
			_ = n
			return int64(n)
		case object.IsKind[*object.Bignum](__sw145):
			n := object.Kind[*object.Bignum](__sw145)
			_ = n
			return new(big.Int).Set(n.I)
		case object.IsFloat(__sw145):
			n := object.AsFloatV(__sw145)
			_ = n
			return float64(n)
		case object.IsKind[*object.String](__sw145):
			n := object.Kind[*object.String](__sw145)
			_ = n
			return n.Str()
		case object.IsKind[object.Symbol](__sw145):
			n := object.Kind[object.Symbol](__sw145)
			_ = n
			return rspec.Symbol(string(n))
		case object.IsKind[*object.Array](__sw145):
			n := object.Kind[*object.Array](__sw145)
			_ = n
			out := make([]any, len(n.Elems))
			for i, el := range n.Elems {
				out[i] = rspecFromRuby(vm, el)
			}
			return out
		case object.IsKind[*object.Hash](__sw145):
			n := object.Kind[*object.Hash](__sw145)
			_ = n
			h := rspec.NewHash()
			for _, k := range n.Keys {
				val, _ := n.Get(k)
				h.Set(rspecFromRuby(vm, k), rspecFromRuby(vm, val))
			}
			return h
		case object.IsKind[*object.Range](__sw145):
			n := object.Kind[*object.Range](__sw145)
			_ = n
			return &rspec.Range{
				Begin:     rspecFromRuby(vm, n.Lo),
				End:       rspecFromRuby(vm, n.Hi),
				Exclusive: n.Exclusive,
			}
		case object.IsKind[*Regexp](__sw145):
			n := object.Kind[*Regexp](__sw145)
			_ = n
			return &rspec.Regexp{Source: n.source, Flags: orderFlags(n.flags)}
		case object.IsKind[*RClass](__sw145):
			n := object.Kind[*RClass](__sw145)
			_ = n
			return rspec.Class(n.ToS())
		}
	}
	return rspecObject(vm, v)
}

// rspecObject builds an *rspec.Object snapshot of an arbitrary Ruby object: its
// class name, its ancestry (for be_a / be_kind_of), the ivar values (for
// have_attributes), and the instance-method names it answers to (for
// respond_to). The snapshot is deterministic and self-contained, so the matchers
// reflect without a live call back into rbgo.
func rspecObject(vm *VM, v object.Value) *rspec.Object {
	cls := vm.classOf(v)
	obj := &rspec.Object{
		Class:      cls.ToS(),
		IVars:      map[string]any{},
		RespondsTo: rspecMethodNames(vm, v),
	}
	if ro, ok := object.KindOK[*RObject](v); ok {
		for name, val := range ro.ivars {
			obj.IVars[name] = rspecFromRuby(vm, val)
			obj.Order = append(obj.Order, name)
		}
	}
	return obj
}

// rspecMethodNames returns the instance-method names v answers to, walking its
// class ancestry (and any singleton). It backs the respond_to matcher's snapshot.
func rspecMethodNames(vm *VM, v object.Value) []string {
	seen := map[string]bool{}
	var out []string
	add := func(m map[string]*Method) {
		for name := range m {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	if sc := vm.objSingleton(v); sc != nil {
		add(sc.methods)
	}
	for _, a := range vm.ancestors(vm.classOf(v)) {
		add(a.methods)
	}
	return out
}

// --- argument coercion -----------------------------------------------------

// rspecArg converts the single matcher argument (e.g. eq(x)) to the rspec model,
// defaulting to nil when absent.
func rspecArg(vm *VM, args []object.Value) any {
	if len(args) == 0 {
		return nil
	}
	return rspecFromRuby(vm, args[0])
}

// rspecArgs converts a whole argument list (e.g. include(a, b)).
func rspecArgs(vm *VM, args []object.Value) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = rspecFromRuby(vm, a)
	}
	return out
}

// rspecArrayArg converts a single Array argument to []any (match_array([…])).
func rspecArrayArg(vm *VM, args []object.Value) []any {
	if len(args) == 1 {
		if arr, ok := object.KindOK[*object.Array](args[0]); ok {
			return rspecArgs(vm, arr.Elems)
		}
	}
	return rspecArgs(vm, args)
}

// rspecClassName resolves a class argument to its bare name: a Class yields its
// name, a String / Symbol its text.
func rspecClassName(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	{
		__sw146 := args[0]
		switch {
		case object.IsKind[*RClass](__sw146):
			n := object.Kind[*RClass](__sw146)
			_ = n
			return n.ToS()
		case object.IsKind[*object.String](__sw146):
			n := object.Kind[*object.String](__sw146)
			_ = n
			return n.Str()
		case object.IsKind[object.Symbol](__sw146):
			n := object.Kind[object.Symbol](__sw146)
			_ = n
			return string(n)
		}
	}
	return args[0].ToS()
}

// rspecSymbols converts a respond_to argument list to []rspec.Symbol.
func rspecSymbols(args []object.Value) []rspec.Symbol {
	out := make([]rspec.Symbol, 0, len(args))
	for _, a := range args {
		{
			__sw147 := a
			switch {
			case object.IsKind[object.Symbol](__sw147):
				n := object.Kind[object.Symbol](__sw147)
				_ = n
				out = append(out, rspec.Symbol(string(n)))
			case object.IsKind[*object.String](__sw147):
				n := object.Kind[*object.String](__sw147)
				_ = n
				out = append(out, rspec.Symbol(n.Str()))
			default:
				n := __sw147
				_ = n
				out = append(out, rspec.Symbol(a.ToS()))
			}
		}
	}
	return out
}

// rspecFloatArg coerces a numeric argument (be_within(delta) / of(centre)).
func rspecFloatArg(args []object.Value) float64 {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	{
		__sw148 := args[0]
		switch {
		case object.IsInt(__sw148):
			n := object.AsInteger(__sw148)
			_ = n
			return float64(n)
		case object.IsFloat(__sw148):
			n := object.AsFloatV(__sw148)
			_ = n
			return float64(n)
		case object.IsKind[*object.Bignum](__sw148):
			n := object.Kind[*object.Bignum](__sw148)
			_ = n
			f, _ := new(big.Float).SetInt(n.I).Float64()
			return f
		}
	}
	raise("TypeError", "no implicit conversion to Float")
	return 0
}

// rspecIntArg coerces an Integer argument (respond_to(:m).with(n)).
func rspecIntArg(args []object.Value) int64 {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	{
		__sw149 := args[0]
		switch {
		case object.IsInt(__sw149):
			n := object.AsInteger(__sw149)
			_ = n
			return int64(n)
		case object.IsKind[*object.Bignum](__sw149):
			n := object.Kind[*object.Bignum](__sw149)
			_ = n
			return n.I.Int64()
		}
	}
	raise("TypeError", "no implicit conversion to Integer")
	return 0
}

// rspecMatcherArg unwraps a matcher argument, raising ArgumentError if it is not
// an RSpecMatcher.
func rspecMatcherArg(args []object.Value) rspec.Matcher {
	return rspecMatcherWrap(args).m
}

// rspecMatcherWrap unwraps a matcher argument to its RSpecMatcher wrapper (so a
// raise_error matcher's constraints are available), raising ArgumentError when it
// is not a matcher.
func rspecMatcherWrap(args []object.Value) *RSpecMatcher {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	m, ok := object.KindOK[*RSpecMatcher](args[0])
	if !ok {
		raise("ArgumentError", "expected a matcher, got %s", args[0].Inspect())
	}
	return m
}

// --- raise_error / expectation running -------------------------------------

// rspecRaiseSpecOf builds a raise_error matcher's constraints from its (optional)
// arguments: a Class constrains the error class, a String / Regexp the message.
func rspecRaiseSpecOf(args []object.Value) *rspecRaiseSpec {
	spec := &rspecRaiseSpec{}
	for _, a := range args {
		{
			__sw150 := a
			switch {
			case object.IsKind[*RClass](__sw150):
				n := object.Kind[*RClass](__sw150)
				_ = n
				spec.class = n.ToS()
			case object.IsKind[*object.String](__sw150):
				n := object.Kind[*object.String](__sw150)
				_ = n
				spec.message = n.Str()
			case object.IsKind[*Regexp](__sw150):
				n := object.Kind[*Regexp](__sw150)
				_ = n
				spec.message = &rspec.Regexp{Source: n.source, Flags: orderFlags(n.flags)}
			}
		}
	}
	return spec
}

// rspecRunExpectation drives expect(actual).to(matcher) / .not_to(matcher). For a
// value expectation it runs the matcher against the stored actual. For a block
// expectation (expect { … }) with a raise_error matcher, it evaluates the block,
// observes any Ruby exception, rebuilds the matcher over that observation and
// matches. A failed expectation raises RSpec::Expectations::ExpectationNotMetError.
func (vm *VM) rspecRunExpectation(e *RSpecExpectation, mw *RSpecMatcher, positive bool) object.Value {
	m := mw.m
	if e.block != nil && mw.raiseSpec != nil {
		raised, class, message, _ := vm.rspecCallBlockCatching(e.block)
		m = rspec.RaiseErrorObserved(
			rspec.RaisedError{Raised: raised, Class: class, Message: message},
			mw.raiseSpec.class, mw.raiseSpec.message)
	}
	ok, msg := rspec.Expect(e.actual, m, positive)
	if !ok {
		raise("RSpec::Expectations::ExpectationNotMetError", "%s", msg)
	}
	return object.NilVal()
}

// rspecCallBlockCatching runs blk and reports whether it raised, plus the raised
// exception's class name, message and return value (when it did not raise). A
// non-Ruby Go panic is re-raised (never swallowed).
func (vm *VM) rspecCallBlockCatching(blk *Proc) (raised bool, class, message string, result object.Value) {
	defer func() {
		if r := recover(); r != nil {
			re, ok := r.(RubyError)
			if !ok {
				panic(r)
			}
			raised = true
			class = re.Class
			message = re.Message
		}
	}()
	result = vm.callBlock(blk, nil)
	return false, "", "", result
}
