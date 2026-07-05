// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"sort"
	"strings"
	"time"

	minitest "github.com/go-ruby-minitest/minitest"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file completes the Minitest binding's deferred follow-up on top of the
// core in minitest.go / minitest_bind.go (which own the Runtime adapter, the
// assert_*/refute_* surface, Minitest::Test#run, Result and Mock): the spec DSL
// (describe/it/before/after/let), the must_*/wont_* expectation surface over `_`,
// and the aggregate runner + reporter driven by `require "minitest/autorun"`.
//
// It reuses the core's helpers — Minitest::Test, the Minitest::Assertions mixin
// and its per-instance counter, Test#run and its MinitestResult — so a spec `it`
// block and a bare `must_equal` both flow through exactly the same byte-exact
// assertion path as a classic assert_equal.
//
// Still deferred (as in the core): assert_output/assert_silent, assert_throws,
// Object#stub. The must_*/wont_* variants that map onto those are not installed.

// registerMinitestSpec installs the runnable registry (the inherited hook on
// Minitest::Test), Minitest::Spec and its DSL, the must_*/wont_* expectations,
// and the autorun runner/reporter. It runs after registerMinitest, layering on
// the Test class and assertions module that registration created.
func (vm *VM) registerMinitestSpec() {
	mod := vm.consts["Minitest"].(*RClass)
	test := vm.consts["Minitest::Test"].(*RClass)

	// Runnable registry: the inherited hook records every Minitest::Test descendant
	// (user Test subclasses and Spec subclasses) in definition order, so the runner
	// can find them. Test/Spec themselves are created with newClass (no hook), so
	// they are never registered as runnables.
	test.smethods["inherited"] = &Method{name: "inherited", owner: test,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if c, ok := args[0].(*RClass); ok {
				vm.minitestRunnables = append(vm.minitestRunnables, c)
			}
			return object.NilV
		}}
	// rbgo runs tests in deterministic (alphabetical) order; the gem's ordering
	// controls are accepted as no-ops so real suites load unchanged.
	for _, name := range []string{"i_suck_and_my_tests_are_order_dependent!", "make_my_diffs_pretty!", "parallelize_me!"} {
		test.smethods[name] = &Method{name: name, owner: test, native: minitestSpecNoop}
	}

	spec := newClass("Minitest::Spec", test)
	vm.cMinitestSpec = spec
	mod.consts["Spec"] = spec
	vm.consts["Minitest::Spec"] = spec
	vm.registerMinitestSpecDSL(spec)

	vm.registerMinitestExpectations()

	// Minitest.run runs the whole suite through the reporter (idempotent under
	// autorun) and returns whether it passed.
	mod.smethods["run"] = &Method{name: "run", owner: mod,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Bool(vm.minitestReport())
		}}

	// require "minitest/autorun" schedules the run at program exit (at_exit LIFO).
	vm.featureHooks["minitest/autorun"] = vm.minitestInstallAutorun
}

// minitestSpecNoop is the shared no-op native for the accepted ordering directives.
func minitestSpecNoop(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
	return object.NilV
}

// --- the runner + reporter ---------------------------------------------------

// minitestReport runs every registered runnable's test methods through the core
// Minitest::Test#run and prints the gem-format report (progress codes, statistics,
// numbered failures, summary) to the current $stdout, returning whether the suite
// passed. The seed and timing lines are the only non-deterministic output (rbgo
// pins the seed to 0); source-location line numbers are 0 because rbgo carries no
// source positions.
func (vm *VM) minitestReport() bool {
	start := time.Now()
	var results []*minitest.Result
	for _, cls := range vm.minitestRunnables {
		for _, name := range vm.minitestTestMethods(cls) {
			results = append(results, vm.minitestRunOne(cls, name))
		}
	}
	elapsed := time.Since(start).Seconds()

	var b strings.Builder
	b.WriteString("Run options: --seed 0\n\n# Running:\n\n")
	for _, r := range results {
		b.WriteString(r.ResultCode())
	}
	b.WriteString("\n\n")

	assertions, failures, errors, skips := 0, 0, 0, 0
	for _, r := range results {
		assertions += r.Assertions
		switch {
		case r.Skipped():
			skips++
		case r.Errored():
			errors++
		case !r.Passed():
			failures++
		}
	}

	runsRate, assertRate := 0.0, 0.0
	if elapsed > 0 {
		runsRate = float64(len(results)) / elapsed
		assertRate = float64(assertions) / elapsed
	}
	b.WriteString(fmt.Sprintf("Finished in %.6fs, %.4f runs/s, %.4f assertions/s.\n", elapsed, runsRate, assertRate))

	// The default (non-verbose) reporter lists failures and errors but not skips.
	i := 0
	for _, r := range results {
		if r.Passed() || r.Skipped() {
			continue
		}
		i++
		b.WriteString(fmt.Sprintf("\n%3d) %s", i, r.String("")))
	}

	b.WriteString(fmt.Sprintf("\n%d runs, %d assertions, %d failures, %d errors, %d skips\n",
		len(results), assertions, failures, errors, skips))

	if skips > 0 {
		b.WriteString("\nYou have skipped tests. Run with --verbose for details.\n")
	}

	vm.curStdout().writeStr(b.String())
	return failures == 0 && errors == 0
}

// minitestRunOne instantiates cls with the test-method name (the core Test#run
// reads @NAME), runs it, and returns the underlying *minitest.Result. The instance
// is recorded as the current one for the duration of the run so a bare must_*/_
// inside the body dispatches its assertion against it.
func (vm *VM) minitestRunOne(cls *RClass, name string) *minitest.Result {
	inst := vm.send(cls, "new", []object.Value{object.NewString(name)}, nil)
	prev := vm.minitestCurInstance
	vm.minitestCurInstance = inst
	defer func() { vm.minitestCurInstance = prev }()
	res := vm.send(inst, "run", nil, nil)
	if mr, ok := res.(*MinitestResult); ok {
		return mr.r
	}
	// Defensive: a non-standard #run override that does not return a Result yields
	// an empty passing result so the reporter stays total.
	return &minitest.Result{Klass: cls.name, TestName: name}
}

// minitestTestMethods returns the test_* method names of a runnable, walking its
// ancestry and sorting alphabetically (rbgo's deterministic order).
func (vm *VM) minitestTestMethods(cls *RClass) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range vm.ancestors(cls) {
		for name := range a.methods {
			if strings.HasPrefix(name, "test_") && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// minitestInstallAutorun schedules the suite run at program exit — the effect of
// require "minitest/autorun". The at_exit block guards against a double run.
func (vm *VM) minitestInstallAutorun() {
	vm.atExit = append(vm.atExit, &Proc{native: func(vm *VM, _ []object.Value) object.Value {
		if vm.minitestAutorunDone {
			return object.NilV
		}
		vm.minitestAutorunDone = true
		vm.minitestReport()
		return object.NilV
	}})
}

// --- the spec DSL ------------------------------------------------------------

// registerMinitestSpecDSL installs describe (a top-level Kernel method building an
// anonymous Minitest::Spec subclass) and the class-level it/before/after/let that
// run against that class when the describe block is class_eval'd.
func (vm *VM) registerMinitestSpecDSL(spec *RClass) {
	vm.cObject.define("describe", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.minitestDescribe(args, blk, vm.cMinitestSpec)
	})

	// it / specify: define a test_%04d_<desc> method from the block (or a skipping
	// stub when no block is given).
	itFn := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		cls := self.(*RClass)
		desc := minitest.DefaultItDesc
		if len(args) > 0 {
			desc = vm.minitestMsg(args, 0)
		}
		name := minitest.ItName(vm.minitestNextSpecSeq(cls), desc)
		if blk == nil {
			cls.methods[name] = &Method{name: name, owner: cls, native: func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
				vm.raiseMinitest(vm.minitestAssertionsFor(self).SkipError("(no tests defined)"))
				return object.NilV
			}}
		} else {
			cls.methods[name] = &Method{name: name, proc: blk, owner: cls}
		}
		return object.SymVal(name)
	}
	spec.smethods["it"] = &Method{name: "it", owner: spec, native: itFn}
	spec.smethods["specify"] = &Method{name: "specify", owner: spec, native: itFn}

	// before/after: accumulate hook blocks on the spec class; the run-time
	// setup/teardown replay them against the instance.
	hook := func(ivar string) NativeFn {
		return func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
			if blk == nil {
				return object.NilV
			}
			cls := self.(*RClass)
			arr, _ := getIvar(cls, ivar).(*object.Array)
			if arr == nil {
				arr = object.NewArray()
			}
			arr.Elems = append(arr.Elems, &minitestProcBox{p: blk})
			setIvar(cls, ivar, arr)
			return object.NilV
		}
	}
	spec.smethods["before"] = &Method{name: "before", owner: spec, native: hook("@__befores__")}
	spec.smethods["after"] = &Method{name: "after", owner: spec, native: hook("@__afters__")}

	// setup/teardown replay the accumulated before/after blocks, outermost spec
	// first, each instance_exec'd against the running test instance.
	spec.define("setup", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.minitestRunHooks(self, "@__befores__")
		return object.NilV
	})
	spec.define("teardown", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.minitestRunHooks(self, "@__afters__")
		return object.NilV
	})

	// let: define a memoized reader (name validated per the gem).
	spec.smethods["let"] = &Method{name: "let", owner: spec, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		cls := self.(*RClass)
		name := minitestName(minitestArg(args, 0))
		if msg := minitest.ValidateLetName(name, vm.minitestSpecReserved()); msg != "" {
			raise("ArgumentError", "%s", msg)
		}
		ivar := "@__let_" + name
		cls.methods[name] = &Method{name: name, owner: cls, native: func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			if v := getIvar(self, ivar); !object.IsNil(v) {
				return v
			}
			r := vm.callBlockSelf(blk, self, nil)
			setIvar(self, ivar, r)
			return r
		}}
		return object.SymVal(name)
	}}
}

// minitestDescribe builds an anonymous spec class named after desc, registers it
// as a runnable, and class_evals the block so it/before/let land on it.
func (vm *VM) minitestDescribe(args []object.Value, blk *Proc, super *RClass) object.Value {
	desc := ""
	if len(args) > 0 {
		desc = vm.minitestMsg(args, 0)
	}
	cls := newClass(desc, super)
	cls.named = true
	setIvar(cls, "@__desc__", object.NewString(desc))
	vm.minitestRunnables = append(vm.minitestRunnables, cls)
	if blk != nil {
		vm.classEval(cls, blk, nil)
	}
	return cls
}

// minitestNextSpecSeq returns the next 1-based it/specify sequence number for a
// spec class (the counter behind the test_%04d_ name morphing).
func (vm *VM) minitestNextSpecSeq(cls *RClass) int {
	n := 0
	if v, ok := getIvar(cls, "@__specs__").(object.Integer); ok {
		n = int(v)
	}
	n++
	setIvar(cls, "@__specs__", object.IntValue(int64(n)))
	return n
}

// minitestRunHooks replays the before/after blocks stored on the instance's spec
// class chain (outermost first) against the instance.
func (vm *VM) minitestRunHooks(inst object.Value, ivar string) {
	chain := vm.ancestors(vm.classOf(inst))
	for i := len(chain) - 1; i >= 0; i-- {
		if arr, ok := getIvar(chain[i], ivar).(*object.Array); ok {
			for _, e := range arr.Elems {
				if pb, ok := e.(*minitestProcBox); ok {
					vm.callBlockSelf(pb.p, inst, nil)
				}
			}
		}
	}
}

// minitestSpecReserved is the Minitest::Spec instance-method set let must not
// override (minus "subject"), computed from the class ancestry.
func (vm *VM) minitestSpecReserved() []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range vm.ancestors(vm.cMinitestSpec) {
		for name := range a.methods {
			if name == "subject" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// minitestProcBox wraps a Proc so it can live inside an Array ivar (the
// before/after hook lists). It is never surfaced to Ruby as a manipulable value.
type minitestProcBox struct{ p *Proc }

func (p *minitestProcBox) ToS() string     { return "#<Proc>" }
func (p *minitestProcBox) Inspect() string { return "#<Proc>" }
func (p *minitestProcBox) Truthy() bool    { return true }

// --- the must_*/wont_* expectation surface ----------------------------------

// registerMinitestExpectations installs `_` and the must_*/wont_* spec-expectation
// methods on Object. An expectation dispatches, per its flip rule, to the
// underlying assertion on the currently-running test instance. Only expectations
// whose assertion the core binds are installed (assert_output/assert_silent/
// assert_throws/assert_pattern/assert_path_exists/assert_mock are deferred).
func (vm *VM) registerMinitestExpectations() {
	// `_` wraps the expectation target: a block target (must_raise) is returned as
	// the Proc so the expectation can pass it as the assertion block; a value target
	// is returned as-is. Minitest's other spellings are left unbound to avoid
	// polluting every Object in the shared VM: `expect` already belongs to RSpec (a
	// different surface) and `value` is a common method name; `_` is the canonical
	// collision-safe form.
	vm.cObject.define("_", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk != nil {
			return blk
		}
		if len(args) > 0 {
			return args[0]
		}
		return self
	})

	for _, e := range minitest.Expectations() {
		if !minitestBoundAssertions[e.Assertion] {
			continue
		}
		ex := e
		vm.cObject.define(ex.Method, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			ctx := vm.minitestCurInstance
			if ctx == nil {
				raise("RuntimeError", "%s called outside of a Minitest run", ex.Method)
			}
			out, blockIsTarget := ex.BindArgs(self, minitestToValues(args))
			gargs := minitestToRuby(out)
			if blockIsTarget {
				p, ok := self.(*Proc)
				if !ok {
					raise("ArgumentError", "%s expects a callable target", ex.Method)
				}
				return vm.send(ctx, ex.Assertion, gargs, p)
			}
			return vm.send(ctx, ex.Assertion, gargs, nil)
		})
	}
}

// minitestBoundAssertions is the set of underlying assertions the core binds, so
// only the must_*/wont_* expectations that map onto a real assertion are installed.
var minitestBoundAssertions = map[string]bool{
	"assert_empty": true, "assert_equal": true, "assert_in_delta": true,
	"assert_in_epsilon": true, "assert_includes": true, "assert_instance_of": true,
	"assert_kind_of": true, "assert_match": true, "assert_nil": true,
	"assert_operator": true, "assert_respond_to": true, "assert_same": true,
	"assert_raises": true,
	"refute_empty":  true, "refute_equal": true, "refute_in_delta": true,
	"refute_in_epsilon": true, "refute_includes": true, "refute_instance_of": true,
	"refute_kind_of": true, "refute_match": true, "refute_nil": true,
	"refute_operator": true, "refute_respond_to": true, "refute_same": true,
}

// minitestToValues lifts a Ruby argument slice into the library's []Value.
func minitestToValues(args []object.Value) []minitest.Value {
	out := make([]minitest.Value, len(args))
	for i, a := range args {
		out[i] = a
	}
	return out
}

// minitestToRuby lowers a library []Value back to Ruby values (a nil placeholder —
// the FlipDefault "no expected arg" case — becomes nil).
func minitestToRuby(vals []minitest.Value) []object.Value {
	out := make([]object.Value, len(vals))
	for i, v := range vals {
		if v == nil {
			out[i] = object.NilV
			continue
		}
		if ov, ok := v.(object.Value); ok {
			out[i] = ov
		} else {
			out[i] = object.NilV
		}
	}
	return out
}
