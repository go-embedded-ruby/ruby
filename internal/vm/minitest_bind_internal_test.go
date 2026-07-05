// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"math"
	"testing"

	minitest "github.com/go-ruby-minitest/minitest"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestMinitestValueProtocol covers the ToS / Inspect / Truthy arms of every
// Minitest wrapper (they never reach Ruby directly, so they are exercised here).
func TestMinitestValueProtocol(t *testing.T) {
	box := &MinitestAssertionsBox{a: minitest.NewAssertions(&minitestRuntime{})}
	if box.ToS() != "#<Minitest::Assertions>" || box.Inspect() != "#<Minitest::Assertions>" || !box.Truthy() {
		t.Errorf("box protocol: %q %q %v", box.ToS(), box.Inspect(), box.Truthy())
	}
	res := &MinitestResult{r: &minitest.Result{Klass: "K", TestName: "test_it", Assertions: 1}}
	if res.ToS() == "" || res.Inspect() == "" || !res.Truthy() {
		t.Errorf("result protocol: %q %q %v", res.ToS(), res.Inspect(), res.Truthy())
	}
	mock := &MinitestMock{}
	if mock.ToS() != "#<Minitest::Mock>" || mock.Inspect() != "#<Minitest::Mock>" || !mock.Truthy() {
		t.Errorf("mock protocol: %q %q %v", mock.ToS(), mock.Inspect(), mock.Truthy())
	}
}

// TestMinitestRuntimeGoOnly covers the minitestRuntime arms not reachable through
// a Ruby assertion: Encoding on a non-String value and ObjectID's non-Integer
// fallback (a near-max Integer's object_id is a Bignum, not an Integer).
func TestMinitestRuntimeGoOnly(t *testing.T) {
	vm := New(io.Discard)
	rt := &minitestRuntime{vm: vm}
	if enc, ok := rt.Encoding(object.Integer(1)); enc != "UTF-8" || !ok {
		t.Errorf("Encoding non-string got=%q,%v", enc, ok)
	}
	// A near-max Integer's object_id (2n+1) overflows int64 into a Bignum, so the
	// Integer type-assertion in ObjectID fails and it returns 0.
	if id := rt.ObjectID(object.Integer(math.MaxInt64)); id != 0 {
		t.Errorf("ObjectID bignum fallback got=%d", id)
	}
}

// TestMinitestMockMatcherCallBlockRange covers CallBlock's out-of-range guard.
func TestMinitestMockMatcherCallBlockRange(t *testing.T) {
	mm := &minitestMockMatcher{vm: New(io.Discard)}
	if mm.CallBlock(0, nil, nil) {
		t.Error("CallBlock with no queued blocks should be false")
	}
	if mm.CallBlock(-1, nil, nil) {
		t.Error("CallBlock negative index should be false")
	}
}

// TestMinitestNameDefault covers minitestName's non-Symbol/String fallback.
func TestMinitestNameDefault(t *testing.T) {
	if got := minitestName(object.Integer(7)); got != "7" {
		t.Errorf("minitestName default got=%q", got)
	}
}

// TestMinitestClassMatches covers minitestClassMatches' positive arm (a subclass
// of an expected exception class matches).
func TestMinitestClassMatches(t *testing.T) {
	vm := New(io.Discard)
	arg := vm.consts["ArgumentError"].(*RClass)
	std := vm.consts["StandardError"].(*RClass)
	if !minitestClassMatches(arg, []object.Value{std}) {
		t.Error("ArgumentError should match StandardError")
	}
	if minitestClassMatches(arg, []object.Value{object.NewString("x")}) {
		t.Error("a non-class expectation must not match")
	}
}

// TestMinitestExceptionFreshBuilt covers minitestException building an instance
// when the caught RubyError carried no exception object (a native raise-by-name).
func TestMinitestExceptionFreshBuilt(t *testing.T) {
	vm := New(io.Discard)
	obj := vm.minitestException(RubyError{Class: "ArgumentError", Message: "boom"})
	ro, ok := obj.(*RObject)
	if !ok || ro.class != vm.consts["ArgumentError"].(*RClass) {
		t.Fatalf("minitestException built %T", obj)
	}
	if getIvar(ro, "@message").ToS() != "boom" {
		t.Errorf("message = %q", getIvar(ro, "@message").ToS())
	}
}

// TestMinitestIsAAbsent covers minitestIsA returning false for an unregistered
// constant name.
func TestMinitestIsAAbsent(t *testing.T) {
	vm := New(io.Discard)
	if vm.minitestIsA(vm.cObject, "NoSuchConstantXYZ") {
		t.Error("minitestIsA on an absent constant should be false")
	}
}

// TestMinitestRaisedClassFallbacks covers minitestRaisedClass' by-name arm and
// its StandardError fallback for an unknown class name.
func TestMinitestRaisedClassFallbacks(t *testing.T) {
	vm := New(io.Discard)
	if c := vm.minitestRaisedClass(RubyError{Class: "ArgumentError"}); c != vm.consts["ArgumentError"].(*RClass) {
		t.Error("by-name arm")
	}
	if c := vm.minitestRaisedClass(RubyError{Class: "NoSuchClassXYZ"}); c != vm.consts["StandardError"].(*RClass) {
		t.Error("fallback arm")
	}
}

// TestMinitestCatchNonRubyPanic covers minitestCatch re-raising a non-RubyError
// Go panic untouched.
func TestMinitestCatchNonRubyPanic(t *testing.T) {
	vm := New(io.Discard)
	blk := &Proc{nativeArity: -1, native: func(_ *VM, _ []object.Value) object.Value {
		panic("plain go panic")
	}}
	defer func() {
		if r := recover(); r != "plain go panic" {
			t.Errorf("minitestCatch should re-raise non-Ruby panic, got %v", r)
		}
	}()
	vm.minitestCatch(blk)
	t.Fatal("expected a re-raised panic")
}

// TestMinitestInvokeNonRubyPanic covers minitestTestBody.Invoke re-raising a
// non-RubyError Go panic untouched, and the body's readers.
func TestMinitestInvokeNonRubyPanic(t *testing.T) {
	vm := New(io.Discard)
	cls := newClass("BoomKlass", vm.cObject)
	cls.define("boom", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		panic("plain go panic")
	})
	obj := &RObject{class: cls, ivars: map[string]object.Value{}}
	body := &minitestTestBody{vm: vm, self: obj, name: "boom", klass: "BoomKlass"}
	if body.Name() != "boom" || body.ClassName() != "BoomKlass" {
		t.Errorf("body readers: %q %q", body.Name(), body.ClassName())
	}
	if f, l := body.SourceLocation(); f != "" || l != 0 {
		t.Errorf("SourceLocation = %q,%d", f, l)
	}
	defer func() {
		if r := recover(); r != "plain go panic" {
			t.Errorf("Invoke should re-raise non-Ruby panic, got %v", r)
		}
	}()
	_ = body.Invoke("boom")
	t.Fatal("expected a re-raised panic")
}

// TestMinitestRaiseMockErrDefault covers raiseMockErr's default arm (a non-Mock
// error surfaces as StandardError).
func TestMinitestRaiseMockErrDefault(t *testing.T) {
	vm := New(io.Discard)
	defer func() {
		r := recover()
		re, ok := r.(RubyError)
		if !ok || re.Class != "StandardError" || re.Message != "other" {
			t.Errorf("raiseMockErr default got %v", r)
		}
	}()
	vm.raiseMockErr(errOther("other"))
	t.Fatal("expected raise")
}

// TestMinitestRaiseMinitestDefault covers raiseMinitest's default arm (a non-Skip,
// non-Assertion error surfaces as Minitest::Assertion).
func TestMinitestRaiseMinitestDefault(t *testing.T) {
	vm := New(io.Discard)
	defer func() {
		r := recover()
		re, ok := r.(RubyError)
		if !ok || re.Class != "Minitest::Assertion" || re.Message != "weird" {
			t.Errorf("raiseMinitest default got %v", r)
		}
	}()
	vm.raiseMinitest(errOther("weird"))
	t.Fatal("expected raise")
}

// errOther is a plain error hitting the default arms of the raise* mappers.
type errOther string

func (e errOther) Error() string { return string(e) }
