// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"unicode/utf8"

	minitest "github.com/go-ruby-minitest/minitest"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent github.com/go-ruby-minitest/minitest library.
// The library owns every assert_*/refute_* and its byte-exact failure message,
// the run lifecycle (setup→body→teardown into a Result), the exception model
// (Assertion/Skip/UnexpectedError) and the Mock framework; everything that needs
// genuine Ruby object semantics is funneled through the library's Runtime seam,
// which minitestRuntime implements below over vm.send (see minitest.go for the
// module/class/method registration). The library never evaluates Ruby — it only
// formats, orchestrates and aggregates; this file supplies the object semantics
// and maps the library's failures onto rbgo exceptions.

// minitestRuntime implements the library's Runtime interface (the ~19 Ruby-value
// operations the assertion layer would otherwise send to a Ruby object) over a
// *VM. Every method maps to the matching rbgo operation, most via vm.send so a
// user override of #==, #inspect, #=~, … is honoured exactly as the gem's would
// be. The library's Value is `any`; each concrete value is an object.Value.
type minitestRuntime struct{ vm *VM }

// v narrows a library Value (any) back to an rbgo object.Value. Every Value that
// crosses the seam originated as an object.Value, so the assertion is total.
func mtv(v minitest.Value) object.Value { return v.(object.Value) }

// Inspect returns obj.inspect — the raw material of mu_pp.
func (r *minitestRuntime) Inspect(obj minitest.Value) string {
	return r.vm.send(mtv(obj), "inspect", nil, nil).ToS()
}

// Encoding reports a String's encoding name and whether it is validly encoded;
// mu_pp consults it only for String values, so a non-string result is ignored.
func (r *minitestRuntime) Encoding(obj minitest.Value) (string, bool) {
	if s, ok := mtv(obj).(*object.String); ok {
		return s.EncName(), utf8.ValidString(s.Str())
	}
	return "UTF-8", true
}

// DefaultExternalEncoding returns Encoding.default_external's name; rbgo's
// default external encoding is UTF-8.
func (r *minitestRuntime) DefaultExternalEncoding() string { return "UTF-8" }

// IsString reports String === obj, used by mu_pp's encoding annotation and by
// assert_match's String→Regexp promotion.
func (r *minitestRuntime) IsString(obj minitest.Value) bool {
	_, ok := mtv(obj).(*object.String)
	return ok
}

// Equal reports obj == other (Ruby #==), honouring a user-defined ==.
func (r *minitestRuntime) Equal(a, b minitest.Value) bool {
	return r.vm.send(mtv(a), "==", []object.Value{mtv(b)}, nil).Truthy()
}

// Same reports a.equal?(b) — Ruby object identity.
func (r *minitestRuntime) Same(a, b minitest.Value) bool {
	return r.vm.send(mtv(a), "equal?", []object.Value{mtv(b)}, nil).Truthy()
}

// ObjectID returns obj.object_id, used verbatim in the assert_same message.
func (r *minitestRuntime) ObjectID(obj minitest.Value) int64 {
	if n, ok := r.vm.objectID(mtv(obj)).(object.Integer); ok {
		return int64(n)
	}
	return 0
}

// Truthy reports Ruby truthiness (false only for nil and false).
func (r *minitestRuntime) Truthy(obj minitest.Value) bool { return mtv(obj).Truthy() }

// IsNil reports obj.nil?.
func (r *minitestRuntime) IsNil(obj minitest.Value) bool { return object.IsNil(mtv(obj)) }

// Match reports the truthiness of (matcher =~ obj); assert_match has already
// promoted a bare String matcher to a Regexp via StringToRegexp.
func (r *minitestRuntime) Match(matcher, obj minitest.Value) bool {
	return r.vm.send(mtv(matcher), "=~", []object.Value{mtv(obj)}, nil).Truthy()
}

// StringToRegexp builds Regexp.new(Regexp.escape(s)) for a String matcher.
func (r *minitestRuntime) StringToRegexp(s minitest.Value) minitest.Value {
	esc := r.vm.send(r.vm.cRegexp, "escape", []object.Value{mtv(s)}, nil)
	return r.vm.send(r.vm.cRegexp, "new", []object.Value{esc}, nil)
}

// RespondTo reports obj.respond_to?(meth, includeAll).
func (r *minitestRuntime) RespondTo(obj minitest.Value, meth string, includeAll bool) bool {
	return r.vm.send(mtv(obj), "respond_to?",
		[]object.Value{object.SymVal(meth), object.Bool(includeAll)}, nil).Truthy()
}

// Includes reports collection.include?(obj).
func (r *minitestRuntime) Includes(collection, obj minitest.Value) bool {
	return r.vm.send(mtv(collection), "include?", []object.Value{mtv(obj)}, nil).Truthy()
}

// Empty reports obj.empty?.
func (r *minitestRuntime) Empty(obj minitest.Value) bool {
	return r.vm.send(mtv(obj), "empty?", nil, nil).Truthy()
}

// InstanceOf reports obj.instance_of?(cls).
func (r *minitestRuntime) InstanceOf(obj, cls minitest.Value) bool {
	return r.vm.send(mtv(obj), "instance_of?", []object.Value{mtv(cls)}, nil).Truthy()
}

// KindOf reports obj.kind_of?(cls).
func (r *minitestRuntime) KindOf(obj, cls minitest.Value) bool {
	return r.vm.send(mtv(obj), "kind_of?", []object.Value{mtv(cls)}, nil).Truthy()
}

// ClassName returns obj.class.name.
func (r *minitestRuntime) ClassName(obj minitest.Value) string {
	return r.vm.classOf(mtv(obj)).name
}

// Name returns a class/module's printable #to_s (its name).
func (r *minitestRuntime) Name(cls minitest.Value) string {
	return r.vm.send(mtv(cls), "to_s", nil, nil).ToS()
}

// Send invokes obj.__send__(op, args...) and returns the result, driving
// assert_operator / assert_predicate.
func (r *minitestRuntime) Send(obj minitest.Value, op string, args ...minitest.Value) minitest.Value {
	gargs := make([]object.Value, len(args))
	for i, a := range args {
		gargs[i] = mtv(a)
	}
	return r.vm.send(mtv(obj), op, gargs, nil)
}

// --- wrapper value types --------------------------------------------------

// MinitestAssertionsBox carries the per-receiver *minitest.Assertions (the
// assertion counter + Runtime) attached to a test instance as a hidden ivar. It
// is never handed to Ruby directly; classOf maps it to Object defensively.
type MinitestAssertionsBox struct{ a *minitest.Assertions }

func (b *MinitestAssertionsBox) ToS() string     { return "#<Minitest::Assertions>" }
func (b *MinitestAssertionsBox) Inspect() string { return "#<Minitest::Assertions>" }
func (b *MinitestAssertionsBox) Truthy() bool    { return true }

// MinitestResult wraps a *minitest.Result — the snapshot Minitest::Test#run
// produces (assertion count, captured failures, timing, and the gem's rendering
// / predicates).
type MinitestResult struct{ r *minitest.Result }

func (v *MinitestResult) ToS() string     { return v.r.String("") }
func (v *MinitestResult) Inspect() string { return "#<Minitest::Result " + v.r.ResultCode() + ">" }
func (v *MinitestResult) Truthy() bool    { return true }

// MinitestMock wraps a *minitest.Mock (Minitest::Mock) and the MockMatcher that
// carries its VM-backed case-equality / block seam.
type MinitestMock struct {
	m       *minitest.Mock
	matcher *minitestMockMatcher
}

func (v *MinitestMock) ToS() string     { return "#<Minitest::Mock>" }
func (v *MinitestMock) Inspect() string { return "#<Minitest::Mock>" }
func (v *MinitestMock) Truthy() bool    { return true }

// minitestMockMatcher implements the library's MockMatcher seam: Ruby
// case-equality (=== falling back to ==) for argument matching, #inspect for the
// error messages, and CallBlock for the block-validated expect form. blocks holds
// the Procs passed to expect { … }, indexed by the order they were queued.
type minitestMockMatcher struct {
	vm     *VM
	blocks []*Proc
}

// Match reports (expected === actual) || (expected == actual), the gem's mock
// argument case-equality.
func (mm *minitestMockMatcher) Match(expected, actual minitest.Value) bool {
	e, a := mtv(expected), mtv(actual)
	if mm.vm.send(e, "===", []object.Value{a}, nil).Truthy() {
		return true
	}
	return mm.vm.send(e, "==", []object.Value{a}, nil).Truthy()
}

// Inspect renders v as Ruby #inspect for the mock error messages.
func (mm *minitestMockMatcher) Inspect(v minitest.Value) string {
	return mm.vm.send(mtv(v), "inspect", nil, nil).ToS()
}

// CallBlock invokes the idx-th queued expect block with the actual call args and
// reports its truthiness (the gem's val_block.call(*args, &block)).
func (mm *minitestMockMatcher) CallBlock(idx int, args []minitest.Value, _ []minitest.KV) bool {
	if idx < 0 || idx >= len(mm.blocks) {
		return false
	}
	gargs := make([]object.Value, len(args))
	for i, a := range args {
		gargs[i] = mtv(a)
	}
	return mm.vm.callBlock(mm.blocks[idx], gargs).Truthy()
}

// --- helpers ---------------------------------------------------------------

// minitestAssertionsFor returns the *minitest.Assertions bound to self, creating
// and stashing it (as a hidden ivar) on first use so the assertion count
// accumulates across a test instance's assert_* calls — the Ruby `assertions`
// accessor.
func (vm *VM) minitestAssertionsFor(self object.Value) *minitest.Assertions {
	if box, ok := getIvar(self, "@__minitest_assertions").(*MinitestAssertionsBox); ok {
		return box.a
	}
	a := minitest.NewAssertions(&minitestRuntime{vm: vm})
	setIvar(self, "@__minitest_assertions", &MinitestAssertionsBox{a: a})
	return a
}

// minitestArg returns args[i], or nil when the index is out of range — the
// default an omitted optional assertion argument takes.
func minitestArg(args []object.Value, i int) object.Value {
	if i < len(args) {
		return args[i]
	}
	return object.NilV
}

// minitestMsg resolves the optional trailing custom-message argument to its
// string form ("" when absent): a String is used verbatim, a Proc is called (the
// gem's lazy-message form), any other value is #to_s'd.
func (vm *VM) minitestMsg(args []object.Value, idx int) string {
	if idx >= len(args) {
		return ""
	}
	v := args[idx]
	if object.IsNil(v) {
		return ""
	}
	if p, ok := v.(*Proc); ok {
		return vm.callBlock(p, nil).ToS()
	}
	return v.ToS()
}

// minitestFloat coerces args[idx] to a float64, falling back to def when the
// argument is absent or non-numeric (the delta/epsilon defaults).
func minitestFloat(args []object.Value, idx int, def float64) float64 {
	if idx < len(args) {
		if f, ok := toFloat(args[idx]); ok {
			return f
		}
	}
	return def
}

// minitestName coerces a method/symbol argument (Symbol or String) to its bare
// name.
func minitestName(v object.Value) string {
	switch s := v.(type) {
	case object.Symbol:
		return string(s)
	case *object.String:
		return s.Str()
	}
	return v.ToS()
}

// minitestResult raises err as the matching Ruby exception when non-nil, else
// returns true — the value a successful Minitest assertion yields.
func (vm *VM) minitestResult(err error) object.Value {
	if err != nil {
		vm.raiseMinitest(err)
	}
	return object.Bool(true)
}

// raiseMinitest re-raises a library assertion failure as its Ruby exception: a
// *Skip as Minitest::Skip, any other *Assertion as Minitest::Assertion, so the
// byte-exact failure message reaches Ruby-level rescue and the run lifecycle.
func (vm *VM) raiseMinitest(err error) {
	switch e := err.(type) {
	case *minitest.Skip:
		raise("Minitest::Skip", "%s", e.Msg)
	case *minitest.Assertion:
		raise("Minitest::Assertion", "%s", e.Msg)
	default:
		raise("Minitest::Assertion", "%s", err.Error())
	}
}

// raiseMockErr maps a library Mock failure to its Ruby exception: an expectation
// failure to MockExpectationError, an arity mismatch to ArgumentError, and an
// unmocked method to NoMethodError, matching the gem's classes.
func (vm *VM) raiseMockErr(err error) {
	switch e := err.(type) {
	case *minitest.MockExpectationError:
		raise("MockExpectationError", "%s", e.Msg)
	case *minitest.MockArgumentError:
		raise("ArgumentError", "%s", e.Msg)
	case *minitest.MockNoMethodError:
		raise("NoMethodError", "%s", e.Msg)
	default:
		raise("StandardError", "%s", err.Error())
	}
}

// minitestRaisedClass resolves the Ruby class of a caught exception: its object's
// class when the raise carried an exception instance, else the class named by the
// error, falling back to StandardError.
func (vm *VM) minitestRaisedClass(e RubyError) *RClass {
	if e.Obj != nil {
		if c := vm.classOf(e.Obj); c != nil {
			return c
		}
	}
	if c, ok := vm.consts[e.Class].(*RClass); ok {
		return c
	}
	return vm.consts["StandardError"].(*RClass)
}

// minitestIsA reports whether class c is, or descends from, the class registered
// under the given constant name (false when that constant is absent).
func (vm *VM) minitestIsA(c *RClass, name string) bool {
	if target, ok := vm.consts[name].(*RClass); ok {
		return classIsA(c, target)
	}
	return false
}

// minitestCatch runs blk and reports whether it raised a Ruby exception, plus the
// raised RubyError (or the block's normal result). A non-Ruby Go panic is
// re-raised untouched.
func (vm *VM) minitestCatch(blk *Proc) (raised bool, re RubyError, result object.Value) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(RubyError)
			if !ok {
				panic(r)
			}
			raised, re = true, e
		}
	}()
	result = vm.callBlock(blk, nil)
	return false, RubyError{}, result
}

// minitestClassify narrows a caught RubyError to the library's run-lifecycle
// failure model: a SystemExit/SignalException/NoMemoryError is a *Passthrough
// (aborts the run), a Minitest::Skip a *Skip, a Minitest::Assertion an
// *Assertion, and anything else an *UnexpectedError carrying the wrapped error's
// class and message.
func (vm *VM) minitestClassify(e RubyError) error {
	rc := vm.minitestRaisedClass(e)
	if vm.minitestIsA(rc, "SystemExit") || vm.minitestIsA(rc, "SignalException") ||
		vm.minitestIsA(rc, "NoMemoryError") {
		return &minitest.Passthrough{Err: e}
	}
	if vm.minitestIsA(rc, "Minitest::Skip") {
		return &minitest.Skip{Assertion: minitest.Assertion{Msg: e.Message}}
	}
	if vm.minitestIsA(rc, "Minitest::Assertion") {
		return &minitest.Assertion{Msg: e.Message}
	}
	return &minitest.UnexpectedError{
		Assertion:    minitest.Assertion{Msg: e.Message},
		ErrorClass:   rc.name,
		ErrorMessage: e.Message,
	}
}

// minitestTestBody is the library TestBody seam over one Minitest::Test instance:
// Invoke runs a named instance method (a setup/teardown hook or the test body)
// through the VM, classifying any raise; the readers expose the name, class and
// running assertion count RunTest folds into the Result.
type minitestTestBody struct {
	vm    *VM
	self  object.Value
	name  string
	klass string
}

// Invoke runs the named method on the test instance, returning the classified
// exception it raised, or nil on success.
func (b *minitestTestBody) Invoke(method string) (errOut error) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(RubyError)
			if !ok {
				panic(r)
			}
			errOut = b.vm.minitestClassify(e)
		}
	}()
	b.vm.send(b.self, method, nil, nil)
	return nil
}

func (b *minitestTestBody) Name() string      { return b.name }
func (b *minitestTestBody) ClassName() string { return b.klass }
func (b *minitestTestBody) Assertions() int {
	return b.vm.minitestAssertionsFor(b.self).Count
}
func (b *minitestTestBody) SourceLocation() (string, int) { return "", 0 }
