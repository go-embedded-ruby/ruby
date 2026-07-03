// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestMonitorSynchronize covers Monitor#synchronize / #mon_synchronize running
// the block and returning its value, plus the standalone Monitor and the
// MonitorMixin include path.
func TestMonitorSynchronize(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "monitor"; p(Monitor.new.synchronize { 42 })`, "42"},
		{`require "monitor"; p(Monitor.new.mon_synchronize { 7 })`, "7"},
		// MonitorMixin included into a class gives the same protocol.
		{`require "monitor"
class Foo; include MonitorMixin; end
p(Foo.new.synchronize { 99 })`, "99"},
		// the trivial no-op protocol methods return self.
		{`require "monitor"
m = Monitor.new
p [m.mon_enter.equal?(m), m.mon_exit.equal?(m), m.mon_initialize.equal?(m)]`, "[true, true, true]"},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMonitorSynchronizeNoBlock covers the no-block branch of synchronize, which
// returns the receiver (self) under the single-thread model.
func TestMonitorSynchronizeNoBlock(t *testing.T) {
	vm := New(nil)
	mon := object.Kind[*RClass](vm.consts["Monitor"])
	self := mon.smethods["new"].native(vm, mon, nil, nil)
	for _, name := range []string{"synchronize", "mon_synchronize"} {
		m := lookupMethod(object.Kind[*RObject](self).class, name)
		if got := m.native(vm, self, nil, nil); got != self {
			t.Fatalf("%s no-block: got %v, want self", name, got)
		}
	}
}

// TestMonitorConditionVariable covers new_cond and the no-op ConditionVariable
// methods (wait/wait_while/wait_until/signal/broadcast), each returning self.
func TestMonitorConditionVariable(t *testing.T) {
	got := runSrc(t, `require "monitor"
m = Monitor.new
c = m.new_cond
p [c.wait.equal?(c), c.wait_while.equal?(c), c.wait_until.equal?(c),
   c.signal.equal?(c), c.broadcast.equal?(c)]`)
	if got != "[true, true, true, true, true]" {
		t.Fatalf("cond vars: %q", got)
	}

	// ConditionVariable.new constructs an instance of the cond class.
	vm := New(nil)
	cond := object.Kind[*RClass](object.Kind[*RClass](vm.consts["MonitorMixin"]).consts["ConditionVariable"])
	o := cond.smethods["new"].native(vm, cond, nil, nil)
	if ro, ok := object.KindOK[*RObject](o); !ok || ro.class != cond {
		t.Fatalf("ConditionVariable.new: %#v", o)
	}
	_ = object.NilV
}
