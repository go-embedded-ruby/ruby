// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	minitest "github.com/go-ruby-minitest/minitest"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerMinitest installs the Minitest module (require "minitest"): the
// Minitest::Assertions mixin (every assert_*/refute_* / flunk / skip / pass, with
// the gem's byte-exact failure messages), the Minitest::Test run lifecycle
// (setup→body→teardown captured into a Minitest::Result), the exception tree
// (Minitest::Assertion < Exception, Minitest::Skip < Assertion,
// Minitest::UnexpectedError < Assertion, plus the top-level MockExpectationError)
// and the Minitest::Mock object framework. The interpreter-independent core —
// the failure-message formatting, the run orchestration, the Result rendering and
// the Mock expectation engine — lives in github.com/go-ruby-minitest/minitest;
// this file is the class + method wiring and minitest_bind.go implements the
// library's Runtime seam over rbgo's object graph via vm.send. The value
// semantics (#==, #inspect, #=~, #respond_to?, …) are the host's, so the messages
// reproduce the minitest gem's exactly.
//
// Deferred (noted in the binding): the spec DSL (describe/it/before/after and the
// must_*/wont_* expectations, which need the spec-class synthesis + a running
// spec context), Object#stub (the singleton alias/undef harness),
// assert_output/assert_silent (the stdout/stderr capture seam) and assert_throws
// (the throw/catch seam).
func (vm *VM) registerMinitest() {
	mod := newClass("Minitest", nil)
	mod.isModule = true
	vm.consts["Minitest"] = mod

	vm.registerMinitestExceptions(mod)
	assertions := vm.registerMinitestAssertions(mod)
	vm.registerMinitestTest(mod, assertions)
	vm.registerMinitestResult(mod)
	vm.registerMinitestMock(mod)
}

// registerMinitestExceptions installs the Minitest failure tree mirroring the
// gem: Minitest::Assertion < Exception (so a bare rescue does not swallow it),
// Minitest::Skip < Assertion, Minitest::UnexpectedError < Assertion, and the
// top-level MockExpectationError < StandardError. Each carries @message so
// Exception#message renders the failure text.
func (vm *VM) registerMinitestExceptions(mod *RClass) {
	exc := vm.consts["Exception"].(*RClass)
	std := vm.consts["StandardError"].(*RClass)

	assertion := newClass("Minitest::Assertion", exc)
	mod.consts["Assertion"] = assertion
	vm.consts["Minitest::Assertion"] = assertion

	for _, d := range []struct{ simple, qualified string }{
		{"Skip", "Minitest::Skip"},
		{"UnexpectedError", "Minitest::UnexpectedError"},
	} {
		c := newClass(d.qualified, assertion)
		mod.consts[d.simple] = c
		vm.consts[d.qualified] = c
	}

	// MockExpectationError is a top-level class in the gem (minitest/mock.rb).
	mee := newClass("MockExpectationError", std)
	vm.consts["MockExpectationError"] = mee
}

// registerMinitestAssertions installs the Minitest::Assertions mixin — every
// assert_*/refute_*, flunk, skip and pass — each delegating to the library's
// Assertions (bound through minitestAssertionsFor) so the failure MESSAGE is
// byte-exact, then raising the returned failure in the VM. The module is included
// into Minitest::Test and can be included into any object; the assertion count
// lives per-receiver. It returns the module for the Test class to include.
func (vm *VM) registerMinitestAssertions(mod *RClass) *RClass {
	am := newClass("Minitest::Assertions", nil)
	am.isModule = true
	mod.consts["Assertions"] = am
	vm.consts["Minitest::Assertions"] = am

	def := func(name string, fn NativeFn) { am.define(name, fn) }

	// assertions accessor — the running assertion count.
	def("assertions", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(vm.minitestAssertionsFor(self).Count))
	})

	// Truthiness assertions.
	def("assert", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.Assert(minitestArg(args, 0), vm.minitestMsg(args, 1)))
	})
	def("refute", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.Refute(minitestArg(args, 0), vm.minitestMsg(args, 1)))
	})

	// Always-fail / always-pass / skip.
	def("flunk", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.Flunk(vm.minitestMsg(args, 0)))
	})
	def("pass", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.minitestResult(vm.minitestAssertionsFor(self).Pass())
	})
	def("skip", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		vm.raiseMinitest(a.SkipError(vm.minitestMsg(args, 0)))
		return object.NilV
	})

	// Equality / nil.
	def("assert_equal", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		err, _ := a.AssertEqual(minitestArg(args, 0), minitestArg(args, 1), vm.minitestMsg(args, 2))
		return vm.minitestResult(err)
	})
	def("refute_equal", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.RefuteEqual(minitestArg(args, 0), minitestArg(args, 1), vm.minitestMsg(args, 2)))
	})
	def("assert_nil", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.AssertNil(minitestArg(args, 0), vm.minitestMsg(args, 1)))
	})
	def("refute_nil", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.RefuteNil(minitestArg(args, 0), vm.minitestMsg(args, 1)))
	})

	// Emptiness / inclusion.
	def("assert_empty", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.AssertEmpty(minitestArg(args, 0), vm.minitestMsg(args, 1)))
	})
	def("refute_empty", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.RefuteEmpty(minitestArg(args, 0), vm.minitestMsg(args, 1)))
	})
	def("assert_includes", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.AssertIncludes(minitestArg(args, 0), minitestArg(args, 1), vm.minitestMsg(args, 2)))
	})
	def("refute_includes", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.RefuteIncludes(minitestArg(args, 0), minitestArg(args, 1), vm.minitestMsg(args, 2)))
	})

	// Class relationship.
	def("assert_instance_of", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.AssertInstanceOf(minitestArg(args, 0), minitestArg(args, 1), vm.minitestMsg(args, 2)))
	})
	def("refute_instance_of", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.RefuteInstanceOf(minitestArg(args, 0), minitestArg(args, 1), vm.minitestMsg(args, 2)))
	})
	def("assert_kind_of", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.AssertKindOf(minitestArg(args, 0), minitestArg(args, 1), vm.minitestMsg(args, 2)))
	})
	def("refute_kind_of", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.RefuteKindOf(minitestArg(args, 0), minitestArg(args, 1), vm.minitestMsg(args, 2)))
	})

	// respond_to / match / same.
	def("assert_respond_to", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.AssertRespondTo(minitestArg(args, 0), minitestName(minitestArg(args, 1)), vm.minitestMsg(args, 2), false))
	})
	def("refute_respond_to", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.RefuteRespondTo(minitestArg(args, 0), minitestName(minitestArg(args, 1)), vm.minitestMsg(args, 2), false))
	})
	def("assert_match", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.AssertMatch(minitestArg(args, 0), minitestArg(args, 1), vm.minitestMsg(args, 2)))
	})
	def("refute_match", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.RefuteMatch(minitestArg(args, 0), minitestArg(args, 1), vm.minitestMsg(args, 2)))
	})
	def("assert_same", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.AssertSame(minitestArg(args, 0), minitestArg(args, 1), vm.minitestMsg(args, 2)))
	})
	def("refute_same", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.RefuteSame(minitestArg(args, 0), minitestArg(args, 1), vm.minitestMsg(args, 2)))
	})

	// operator / predicate.
	def("assert_operator", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		o2, msgIdx := minitestOperatorArgs(args)
		return vm.minitestResult(a.AssertOperator(minitestArg(args, 0), minitestName(minitestArg(args, 1)), o2, vm.minitestMsg(args, msgIdx)))
	})
	def("refute_operator", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		o2, msgIdx := minitestOperatorArgs(args)
		return vm.minitestResult(a.RefuteOperator(minitestArg(args, 0), minitestName(minitestArg(args, 1)), o2, vm.minitestMsg(args, msgIdx)))
	})
	def("assert_predicate", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.AssertPredicate(minitestArg(args, 0), minitestName(minitestArg(args, 1)), vm.minitestMsg(args, 2)))
	})
	def("refute_predicate", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.RefutePredicate(minitestArg(args, 0), minitestName(minitestArg(args, 1)), vm.minitestMsg(args, 2)))
	})

	// delta / epsilon.
	def("assert_in_delta", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.AssertInDelta(minitestFloat(args, 0, 0), minitestFloat(args, 1, 0), minitestFloat(args, 2, 0.001), vm.minitestMsg(args, 3)))
	})
	def("refute_in_delta", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.RefuteInDelta(minitestFloat(args, 0, 0), minitestFloat(args, 1, 0), minitestFloat(args, 2, 0.001), vm.minitestMsg(args, 3)))
	})
	def("assert_in_epsilon", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.AssertInEpsilon(minitestFloat(args, 0, 0), minitestFloat(args, 1, 0), minitestFloat(args, 2, 0.001), vm.minitestMsg(args, 3)))
	})
	def("refute_in_epsilon", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.minitestAssertionsFor(self)
		return vm.minitestResult(a.RefuteInEpsilon(minitestFloat(args, 0, 0), minitestFloat(args, 1, 0), minitestFloat(args, 2, 0.001), vm.minitestMsg(args, 3)))
	})

	// assert_raises drives the block through the library and returns the caught
	// exception on success.
	def("assert_raises", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.minitestAssertRaises(self, args, blk)
	})

	return am
}

// minitestOperatorArgs parses assert_operator's optional third operand: with 2
// args the predicate form (o2 = UNDEFINED, msg at index 2); with 3+ args o2 is the
// third argument and the message follows at index 3.
func minitestOperatorArgs(args []object.Value) (o2 minitest.Value, msgIdx int) {
	if len(args) >= 3 {
		return minitestArg(args, 2), 3
	}
	return minitest.UNDEFINED, 2
}

// minitestAssertRaises implements assert_raises: it separates the expected
// exception classes from an optional trailing String message, runs the block,
// classifies the outcome (matched / a Minitest assertion / a passthrough / wrong
// class), and either returns the caught exception (success), re-raises a
// passthrough/assertion, or raises the library's flunk. With no class given it
// defaults to StandardError, as the gem does.
func (vm *VM) minitestAssertRaises(self object.Value, args []object.Value, blk *Proc) object.Value {
	a := vm.minitestAssertionsFor(self)
	var expClasses []object.Value
	customMsg := ""
	for _, ar := range args {
		if s, ok := ar.(*object.String); ok {
			customMsg = s.Str()
			continue
		}
		expClasses = append(expClasses, ar)
	}
	if blk == nil {
		vm.raiseMinitest(a.RaisesRequiresBlock())
		return object.NilV
	}
	if len(expClasses) == 0 {
		expClasses = []object.Value{vm.consts["StandardError"].(*RClass)}
	}

	expArrayInspect := vm.send(object.NewArrayFromSlice(expClasses), "inspect", nil, nil).ToS()
	expSingleInspect := ""
	if len(expClasses) == 1 {
		expSingleInspect = vm.send(expClasses[0], "inspect", nil, nil).ToS()
	}

	raised, re, _ := vm.minitestCatch(blk)
	var out minitest.RaiseOutcome
	if raised {
		rc := vm.minitestRaisedClass(re)
		out = minitest.RaiseOutcome{
			Raised:        true,
			ErrClass:      rc.name,
			ErrMessage:    re.Message,
			Matched:       minitestClassMatches(rc, expClasses),
			IsAssertion:   vm.minitestIsA(rc, "Minitest::Assertion"),
			IsPassthrough: vm.minitestIsA(rc, "SystemExit") || vm.minitestIsA(rc, "SignalException"),
		}
	}

	reRaise, err := a.AssertRaises(out, customMsg, expArrayInspect, expSingleInspect)
	if reRaise {
		panic(re)
	}
	if err != nil {
		vm.raiseMinitest(err)
	}
	return vm.minitestException(re)
}

// minitestClassMatches reports whether rc is, or descends from, any of the
// expected exception classes (RaiseOutcome.Matched).
func minitestClassMatches(rc *RClass, exp []object.Value) bool {
	for _, e := range exp {
		if c, ok := e.(*RClass); ok && classIsA(rc, c) {
			return true
		}
	}
	return false
}

// minitestException returns the caught exception object: the raised instance when
// the raise carried one, else a freshly built instance of the raised class
// carrying its message, so assert_raises's return value is a usable exception.
func (vm *VM) minitestException(re RubyError) object.Value {
	if re.Obj != nil {
		return re.Obj
	}
	cls := vm.minitestRaisedClass(re)
	return &RObject{class: cls, ivars: map[string]object.Value{"@message": object.NewString(re.Message)}}
}

// registerMinitestTest installs Minitest::Test — the run-lifecycle class that
// includes Minitest::Assertions. It defines the six no-op setup/teardown hooks
// (users override setup/teardown), initialize(name) to record the test-method
// name, the name/assertions readers, and #run, which drives the library's RunTest
// (setup→body→teardown, capturing failures) and returns a Minitest::Result.
func (vm *VM) registerMinitestTest(mod, assertions *RClass) {
	c := newClass("Minitest::Test", vm.cObject)
	c.includes = append(c.includes, assertions)
	mod.consts["Test"] = c
	vm.consts["Minitest::Test"] = c

	for _, hook := range []string{"before_setup", "setup", "after_setup",
		"before_teardown", "teardown", "after_teardown"} {
		c.define(hook, func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NilV
		})
	}

	c.define("initialize", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := ""
		if len(args) > 0 && !object.IsNil(args[0]) {
			name = minitestName(args[0])
		}
		setIvar(self, "@NAME", object.NewString(name))
		return object.NilV
	})
	c.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@NAME")
	})

	c.define("run", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		body := &minitestTestBody{
			vm:    vm,
			self:  self,
			name:  getIvar(self, "@NAME").ToS(),
			klass: vm.classOf(self).name,
		}
		res, _ := minitest.RunTest(body, 0)
		return &MinitestResult{r: res}
	})
}

// registerMinitestResult installs Minitest::Result, the snapshot #run returns:
// the assertion count, the pass/skip/error predicates, the single-character
// result code, the captured failure messages and the gem's to_s rendering.
func (vm *VM) registerMinitestResult(mod *RClass) {
	c := newClass("Minitest::Result", vm.cObject)
	mod.consts["Result"] = c
	vm.consts["Minitest::Result"] = c

	resOf := func(self object.Value) *minitest.Result { return self.(*MinitestResult).r }

	c.define("assertions", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(resOf(self).Assertions))
	})
	c.define("passed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(resOf(self).Passed())
	})
	c.define("skipped?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(resOf(self).Skipped())
	})
	c.define("error?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(resOf(self).Errored())
	})
	c.define("result_code", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(resOf(self).ResultCode())
	})
	c.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(resOf(self).TestName)
	})
	c.define("failures", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		r := resOf(self)
		elems := make([]object.Value, len(r.Failures))
		for i, f := range r.Failures {
			elems[i] = object.NewString(f.Message())
		}
		return object.NewArrayFromSlice(elems)
	})
	c.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(resOf(self).String(""))
	})
}

// registerMinitestMock installs Minitest::Mock: new builds a clean mock; expect
// queues an expectation (positional args, or a block-validated form); verify
// checks every queued expectation was met; and method_missing routes an arbitrary
// call into the library's Mock, raising MockExpectationError / ArgumentError /
// NoMethodError to match the gem. Argument case-equality (=== / ==) and the
// expect-block invocation go through the VM-backed MockMatcher.
func (vm *VM) registerMinitestMock(mod *RClass) {
	c := newClass("Minitest::Mock", vm.cObject)
	mod.consts["Mock"] = c
	vm.consts["Minitest::Mock"] = c

	c.smethods["new"] = &Method{name: "new", owner: c, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		matcher := &minitestMockMatcher{vm: vm}
		return &MinitestMock{m: minitest.NewMock(matcher), matcher: matcher}
	}}

	mockOf := func(self object.Value) *MinitestMock { return self.(*MinitestMock) }

	c.define("expect", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		mk := mockOf(self)
		name := minitestName(minitestArg(args, 0))
		retval := minitestArg(args, 1)
		var expArgs []minitest.Value
		if len(args) > 2 && !object.IsNil(args[2]) {
			arr, ok := args[2].(*object.Array)
			if !ok {
				raise("ArgumentError", "args must be an array")
			}
			expArgs = make([]minitest.Value, len(arr.Elems))
			for i, a := range arr.Elems {
				expArgs[i] = a
			}
		}
		// The block-validated form (Mock#expect { |*a| … }) queues the block and,
		// like the gem, rejects positional args given alongside it (the library
		// returns "args ignored when block given" from Expect).
		block := blk != nil
		if block {
			mk.matcher.blocks = append(mk.matcher.blocks, blk)
		}
		if err := mk.m.Expect(name, retval, expArgs, nil, block); err != nil {
			vm.raiseMockErr(err)
		}
		return self
	})

	c.define("verify", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := mockOf(self).m.Verify(); err != nil {
			vm.raiseMockErr(err)
		}
		return object.Bool(true)
	})

	c.define("method_missing", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mk := mockOf(self)
		name := minitestName(minitestArg(args, 0))
		callArgs := make([]minitest.Value, 0, len(args))
		for _, a := range args[1:] {
			callArgs = append(callArgs, a)
		}
		ret, err := mk.m.Call(name, callArgs, nil)
		if err != nil {
			vm.raiseMockErr(err)
		}
		return ret.(object.Value)
	})
}
