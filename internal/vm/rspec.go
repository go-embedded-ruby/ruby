// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rspec "github.com/go-ruby-rspec/rspec"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// RSpecMatcher is the Ruby wrapper around an rspec.Matcher — an RSpec matcher
// object (the result of eq / be / include / match / …). Its match logic and
// byte-faithful failure messages live in the github.com/go-ruby-rspec/rspec
// library (the deterministic, interpreter-independent core of
// rspec-expectations); this shell exposes matches? / failure_message /
// failure_message_when_negated / description and the chainable refinements
// (be_within(d).of(x), respond_to(:m).with(n)). Running the it / describe / hook
// bodies is the rbgo eval seam and is not modelled here.
type RSpecMatcher struct {
	m rspec.Matcher
	// chain, when non-nil, refines this matcher: `of` and `with` build a new
	// matcher from the pending constructor. It is set for the matchers RSpec
	// spells with a fluent tail (be_within, respond_to).
	chain *rspecChain
	// raiseSpec, when non-nil, records a raise_error matcher's class / message
	// constraints so a block expectation can rebuild it over the observed error
	// (the library's matcher is opaque, so the host keeps the constraints).
	raiseSpec *rspecRaiseSpec
}

// rspecRaiseSpec records a raise_error matcher's constraints.
type rspecRaiseSpec struct {
	class   string
	message any
}

// rspecChain records a pending fluent refinement so of / with can rebuild the
// underlying matcher.
type rspecChain struct {
	within *rspecWithinChain
	respTo *rspecRespondChain
}

type rspecWithinChain struct{ delta float64 }
type rspecRespondChain struct{ names []rspec.Symbol }

func (m *RSpecMatcher) ToS() string     { return "#<RSpec::Matchers::Matcher>" }
func (m *RSpecMatcher) Inspect() string { return "#<RSpec::Matchers::Matcher>" }
func (m *RSpecMatcher) Truthy() bool    { return true }

// RSpecExpectation is the Ruby wrapper around an `expect(actual)` /
// `expect { block }` target: `.to(matcher)` / `.not_to(matcher)` run the matcher
// and raise RSpec::Expectations::ExpectationNotMetError on failure. A block
// target carries the Proc so a block matcher (raise_error) can observe the
// block's execution — the one place the rbgo eval seam is driven from the
// matcher surface.
type RSpecExpectation struct {
	actual any
	block  *Proc
}

func (e *RSpecExpectation) ToS() string     { return "#<RSpec::Expectations::ExpectationTarget>" }
func (e *RSpecExpectation) Inspect() string { return e.ToS() }
func (e *RSpecExpectation) Truthy() bool    { return true }

// registerRSpec installs the RSpec module, its matcher surface and expect
// (require "rspec" / "rspec/expectations"): the matcher constructors return
// RSpecMatcher objects (eq/eql/equal/be_*/include/start_with/end_with/match/
// match_array/contain_exactly/all/respond_to/satisfy/cover/be_within/raise_error
// plus the and/or combinators via &/|), and expect(x).to / .not_to drive them.
// The it/describe/hook runner drives example bodies via rbgo and is not part of
// this deterministic surface.
func (vm *VM) registerRSpec() {
	mod := newClass("RSpec", nil)
	mod.isModule = true
	vm.consts["RSpec"] = mod

	// The matcher constructors and expect live on the top level (Kernel), matching
	// how a spec file calls them bare (`expect(x).to eq(1)`), and also as module
	// methods on RSpec::Matchers for explicit qualification.
	matchers := newClass("RSpec::Matchers", nil)
	matchers.isModule = true
	mod.consts["Matchers"] = matchers
	vm.consts["RSpec::Matchers"] = matchers

	vm.registerRSpecErrors(mod)
	vm.registerRSpecMatcherClass(mod)
	vm.registerRSpecExpectationClass(mod)

	// def installs a matcher constructor both as a private Kernel method (bare
	// call) and as an RSpec::Matchers module method.
	def := func(name string, fn NativeFn) {
		vm.cObject.define(name, fn)
		matchers.smethods[name] = &Method{name: name, owner: matchers, native: fn}
	}

	simple := func(name string, build func(vm *VM, args []object.Value) rspec.Matcher) {
		def(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return &RSpecMatcher{m: build(vm, args)}
		})
	}

	// Equality family.
	simple("eq", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.Eq(rspecArg(vm, a)) })
	simple("eql", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.Eql(rspecArg(vm, a)) })
	simple("equal", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.Equal(rspecArg(vm, a)) })

	// be — with an argument it is object identity (equal); with no argument it is
	// the truthiness matcher.
	def("be", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			return &RSpecMatcher{m: rspec.BeTruthy()}
		}
		return &RSpecMatcher{m: rspec.Equal(rspecArg(vm, args))}
	})
	simple("be_truthy", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.BeTruthy() })
	simple("be_falsey", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.BeFalsey() })
	simple("be_falsy", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.BeFalsey() })
	simple("be_nil", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.BeNil() })

	// Comparison matchers (be > x, etc.).
	simple("be_greater_than", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.BeGreaterThan(rspecArg(vm, a)) })
	simple("be_less_than", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.BeLessThan(rspecArg(vm, a)) })

	// Type matchers.
	simple("be_a", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.BeKindOf(rspecClassName(a)) })
	simple("be_an", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.BeKindOf(rspecClassName(a)) })
	simple("be_kind_of", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.BeKindOf(rspecClassName(a)) })
	simple("be_a_kind_of", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.BeKindOf(rspecClassName(a)) })
	simple("be_an_instance_of", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.BeInstanceOf(rspecClassName(a)) })
	simple("be_instance_of", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.BeInstanceOf(rspecClassName(a)) })

	// Collection matchers.
	simple("include", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.Include(rspecArgs(vm, a)...) })
	simple("start_with", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.StartWith(rspecArgs(vm, a)...) })
	simple("end_with", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.EndWith(rspecArgs(vm, a)...) })
	simple("contain_exactly", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.ContainExactly(rspecArgs(vm, a)...) })
	simple("match_array", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.MatchArray(rspecArrayArg(vm, a)) })
	simple("cover", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.Cover(rspecArgs(vm, a)...) })

	// match(pattern) — a Regexp or a matcher-style string/value.
	simple("match", func(vm *VM, a []object.Value) rspec.Matcher { return rspec.Match(rspecArg(vm, a)) })

	// all(matcher) wraps an inner matcher.
	def("all", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return &RSpecMatcher{m: rspec.All(rspecMatcherArg(args))}
	})

	// be_within(delta) — chainable: .of(centre).
	def("be_within", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		delta := rspecFloatArg(args)
		return &RSpecMatcher{
			m:     rspec.BeWithin(delta),
			chain: &rspecChain{within: &rspecWithinChain{delta: delta}},
		}
	})

	// respond_to(:m, …) — chainable: .with(n).arguments.
	def("respond_to", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		names := rspecSymbols(args)
		return &RSpecMatcher{
			m:     rspec.RespondTo(names...),
			chain: &rspecChain{respTo: &rspecRespondChain{names: names}},
		}
	})

	// raise_error([class[, message]]) — a block matcher; expect { … } observes it.
	raiseErr := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		spec := rspecRaiseSpecOf(args)
		return &RSpecMatcher{
			m:         rspec.RaiseErrorObserved(rspec.RaisedError{}, spec.class, spec.message),
			raiseSpec: spec,
		}
	}
	def("raise_error", raiseErr)
	def("raise_exception", raiseErr)

	// expect(actual) / expect { block } builds the expectation target.
	def("expect", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if blk != nil {
			return &RSpecExpectation{block: blk}
		}
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return &RSpecExpectation{actual: rspecFromRuby(vm, args[0])}
	})
}

// registerRSpecErrors installs RSpec::Expectations::ExpectationNotMetError (the
// exception `to` / `not_to` raise on a failed expectation), a subclass of the
// Ruby Exception root (RSpec raises it as an Exception, not a StandardError, so
// a bare `rescue` does not swallow a failed expectation).
func (vm *VM) registerRSpecErrors(mod *RClass) {
	expectations := newClass("RSpec::Expectations", nil)
	expectations.isModule = true
	mod.consts["Expectations"] = expectations
	vm.consts["RSpec::Expectations"] = expectations

	exc := object.Kind[*RClass](vm.consts["Exception"])
	notMet := newClass("RSpec::Expectations::ExpectationNotMetError", exc)
	expectations.consts["ExpectationNotMetError"] = notMet
	vm.consts["RSpec::Expectations::ExpectationNotMetError"] = notMet
}

// registerRSpecMatcherClass installs RSpec::Matchers::BuiltIn::BaseMatcher (the
// class of every matcher object) and its predicate / message methods.
func (vm *VM) registerRSpecMatcherClass(mod *RClass) {
	matchers := object.Kind[*RClass](mod.consts["Matchers"])
	builtIn := newClass("RSpec::Matchers::BuiltIn", nil)
	builtIn.isModule = true
	matchers.consts["BuiltIn"] = builtIn
	vm.consts["RSpec::Matchers::BuiltIn"] = builtIn

	cls := newClass("RSpec::Matchers::BuiltIn::BaseMatcher", vm.cObject)
	builtIn.consts["BaseMatcher"] = cls
	vm.consts["RSpec::Matchers::BuiltIn::BaseMatcher"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *RSpecMatcher { return object.Kind[*RSpecMatcher](v) }

	// matches?(actual) runs the match.
	d("matches?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Bool(self(v).m.Matches(rspecFromRuby(vm, args[0])))
	})
	// failure_message / failure_message_when_negated.
	d("failure_message", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).m.FailureMessage())
	})
	d("failure_message_when_negated", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).m.FailureMessageNegated())
	})
	// description falls back to a generic form for matchers without one.
	d("description", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if dsc, ok := self(v).m.(rspec.Describer); ok {
			return object.NewString(dsc.Description())
		}
		return object.NewString("match")
	})

	// of(centre) refines be_within(delta).of(x).
	d("of", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		if m.chain == nil || m.chain.within == nil {
			raise("NoMethodError", "undefined method `of' for matcher")
		}
		return &RSpecMatcher{m: rspec.BeWithin(m.chain.within.delta).Of(rspecFloatArg(args))}
	})
	// with(n) refines respond_to(:m).with(n); arguments is the no-op tail.
	d("with", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		if m.chain == nil || m.chain.respTo == nil {
			raise("NoMethodError", "undefined method `with' for matcher")
		}
		n := int(rspecIntArg(args))
		return &RSpecMatcher{m: rspec.RespondTo(m.chain.respTo.names...).With(n)}
	})
	d("arguments", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value { return v })
	d("argument", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value { return v })

	// & / | build the and / or combinators.
	d("&", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &RSpecMatcher{m: rspec.And(self(v).m, rspecMatcherArg(args))}
	})
	d("|", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &RSpecMatcher{m: rspec.Or(self(v).m, rspecMatcherArg(args))}
	})
}

// registerRSpecExpectationClass installs RSpec::Expectations::ExpectationTarget
// and its to / not_to (to_not) methods.
func (vm *VM) registerRSpecExpectationClass(mod *RClass) {
	expectations := object.Kind[*RClass](mod.consts["Expectations"])
	cls := newClass("RSpec::Expectations::ExpectationTarget", vm.cObject)
	expectations.consts["ExpectationTarget"] = cls
	vm.consts["RSpec::Expectations::ExpectationTarget"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// to(matcher) passes when the matcher matches; otherwise it raises
	// ExpectationNotMetError with the matcher's failure message.
	d("to", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.rspecRunExpectation(object.Kind[*RSpecExpectation](v), rspecMatcherWrap(args), true)
	})
	notTo := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.rspecRunExpectation(object.Kind[*RSpecExpectation](v), rspecMatcherWrap(args), false)
	}
	d("not_to", notTo)
	d("to_not", notTo)
}
