// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	mvvm "github.com/go-ruby-widgets/mvvm"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestMvvmShells covers ToS/Inspect/Truthy of the three handle wrappers.
func TestMvvmShells(t *testing.T) {
	o := &MvvmObservable{}
	if o.ToS() != "#<Mvvm::Observable>" || o.Inspect() != o.ToS() || !o.Truthy() {
		t.Errorf("observable shell: %q / %q / %v", o.ToS(), o.Inspect(), o.Truthy())
	}
	c := &MvvmCommand{}
	if c.ToS() != "#<Mvvm::Command>" || c.Inspect() != c.ToS() || !c.Truthy() {
		t.Errorf("command shell: %q / %q / %v", c.ToS(), c.Inspect(), c.Truthy())
	}
	l := &MvvmList{}
	if l.ToS() != "#<Mvvm::ObservableList>" || l.Inspect() != l.ToS() || !l.Truthy() {
		t.Errorf("list shell: %q / %q / %v", l.ToS(), l.Inspect(), l.Truthy())
	}
}

// TestMvvmRequireEndToEnd is the headline scenario: require the library, create
// an observable, set a new value and read it back, and drain the change event —
// the task's acceptance test run through the interpreter (the get/set surface
// lives on the handle, as it does in the underlying adapter).
func TestMvvmRequireEndToEnd(t *testing.T) {
	src := `
require "mvvm"
o = Mvvm.observable(1)
sid = o.subscribe("on_change")
o.set(2)
raise "get" unless o.get == 2
o.set(2) # deeply-equal: a no-op, no event
ev = Mvvm.drain_events
raise "one event" unless ev.length == 1
raise "cb"    unless ev[0]["callback_id"] == "on_change"
raise "kind"  unless ev[0]["kind"] == "changed"
raise "value" unless ev[0]["value"] == 2
o.unsubscribe(sid)
o.set(3)
raise "unsubscribed" unless Mvvm.drain_events.length == 0
puts "get=#{o.get}"
`
	if got := runSrc(t, src); got != "get=3" {
		t.Fatalf("end-to-end output = %q", got)
	}
}

// TestMvvmSurface exercises the rest of the Ruby-visible surface so every
// remaining marshalling branch and handle method runs through the interpreter:
// the scalar / Array / Hash / Symbol observed-value shapes, the Command surface
// (can_execute / execute / set_can_execute / raise_can_execute_changed and the
// no-callback silent path), and the full ObservableList mutation + observer
// surface.
func TestMvvmSurface(t *testing.T) {
	src := `
require "mvvm"

# Observed values of every shape: Hash (String + Symbol keys), Array, Float,
# Symbol, Bool and nil.
oh = Mvvm.observable({"a" => 1})
oh.set({"a" => 1})            # equal by content: no change
oh.set({:k => 2})             # Symbol Hash key
oh.set([1, 2, 3])             # Array value
of = Mvvm.observable(1.5)
raise "float" unless (of.get - 1.5).abs < 0.001
osym = Mvvm.observable(:sym)  # a Symbol coerces to its name
raise "sym" unless osym.get == "sym"
ob = Mvvm.observable(true)
raise "bool" unless ob.get == true
on = Mvvm.observable(nil)
raise "nil" unless on.get.nil?

# Command: can_execute, execute (queues an execute event carrying its args),
# set_can_execute (queues a can-execute-changed event), raise_can_execute_changed.
c = Mvvm.command("can_cb", "exec_cb")
raise "can" unless c.can_execute == true
c.execute([10, 20])
c.set_can_execute(false)
raise "cannot" unless c.can_execute == false
c.raise_can_execute_changed
cev = Mvvm.drain_events
raise "cmd events" unless cev.length == 3
raise "exec kind" unless cev[0]["kind"] == "execute"
raise "exec args" unless cev[0]["args"] == [10, 20]

# A command with no callback ids queues nothing on execute.
c2 = Mvvm.command(nil, nil)
c2.execute(nil)
raise "silent" unless Mvvm.drain_events.length == 0

# ObservableList: the full mutation surface under an observer.
l = Mvvm.observable_list([1, 2, 3])
oid = l.observe("list_cb")
raise "size" unless l.size == 3
l.add(4)
l.insert(0, 0)
l.set(1, 9)
l.move(0, 2)
l.remove_at(0)
raise "get" unless l.get(0).is_a?(Integer)
raise "slice" unless l.slice.length == 4
levs = Mvvm.drain_events
raise "list events" unless levs.length == 5
raise "first action" unless levs[0]["action"] == "insert"
l.clear
raise "reset" unless Mvvm.drain_events[0]["action"] == "reset"
l.unobserve(oid)

puts "ok"
`
	if got := runSrc(t, src); got != "ok" {
		t.Fatalf("surface output = %q", got)
	}
}

// TestMvvmErrors covers the raise paths: an out-of-range ObservableList.get and
// an unknown adapter method both surface as a rescuable Mvvm::Error.
func TestMvvmErrors(t *testing.T) {
	src := `
require "mvvm"
l = Mvvm.observable_list([1])
result = begin
  l.get(99)
  "no-raise"
rescue Mvvm::Error => e
  "raised"
end
puts result
`
	if got := runSrc(t, src); got != "raised" {
		t.Fatalf("out-of-range error = %q", got)
	}

	// An unknown method name funnels through Call's error into Mvvm::Error.
	if got := otRecover(func() { New(nil).mvvmDispatch(mvvm.NewModule(), "no_such_method", nil) }); got != "Mvvm::Error" {
		t.Fatalf("unknown method class = %q", got)
	}
}

// TestMvvmValueToAny covers the one mvvmValueToAny branch the Ruby surface cannot
// reach: an unmappable value (a Range) raising TypeError. Every other arm is
// driven by the surface test's observed-value shapes.
func TestMvvmValueToAny(t *testing.T) {
	rng := object.NewRange(object.Integer(1), object.Integer(2), false)
	if cls := otRecover(func() { mvvmValueToAny(rng) }); cls != "TypeError" {
		t.Errorf("unmappable arg class = %q", cls)
	}
}

// TestMvvmAnyToValue covers mvvmAnyToValue branches the adapter's own methods
// never emit: a bare int64 scalar and a value type with no Ruby peer (which
// raises TypeError).
func TestMvvmAnyToValue(t *testing.T) {
	if v, ok := mvvmAnyToValue(int64(7)).(object.Integer); !ok || v != 7 {
		t.Errorf("int64 -> %#v", mvvmAnyToValue(int64(7)))
	}
	if cls := otRecover(func() { mvvmAnyToValue(complex(1, 2)) }); cls != "TypeError" {
		t.Errorf("unmappable result class = %q", cls)
	}
}

// TestMvvmKeyString covers mvvmKeyString's to_s fallback for a non-String,
// non-Symbol Hash key (the String and Symbol arms are exercised by the surface
// test's Hash-valued observables).
func TestMvvmKeyString(t *testing.T) {
	if got := mvvmKeyString(object.Integer(7)); got != "7" {
		t.Errorf("integer key -> %q", got)
	}
}
