// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// pubObs is a shared preamble: an Observable publisher class and an observer
// class that records every update call's arguments into a global $log. Asserted
// values come from MRI 4.0.5 (ruby -robserver).
const pubObs = `require "observer"
$log = []
class Pub
  include Observable
end
class Obs
  def initialize(tag) = @tag = tag
  def update(*a) = $log << [@tag, a]
  def go(*a)     = $log << [@tag, :go, a]
end
`

// TestObservableLifecycle covers the changed/notify lifecycle end to end: a
// fresh registry is unchanged and notify is a no-op (nil); after changed the
// notify dispatches each observer's update in insertion order with the args,
// then resets changed? to false and returns false; a second notify is again a
// no-op. The dispatch is proven by what each observer recorded in $log.
func TestObservableLifecycle(t *testing.T) {
	cases := []struct{ src, want string }{
		// add_observer returns the func symbol; count/changed? track state.
		{pubObs + `pub = Pub.new
p pub.add_observer(Obs.new(:a))
p pub.count_observers
p pub.changed?`, ":update\n1\nfalse"},
		// notify when not changed: no-op returning nil, nothing dispatched.
		{pubObs + `pub = Pub.new
pub.add_observer(Obs.new(:a))
p pub.notify_observers(1, 2)
p $log`, "nil\n[]"},
		// changed returns its state; notify when changed dispatches in insertion
		// order, returns false, and clears changed?.
		{pubObs + `pub = Pub.new
pub.add_observer(Obs.new(:first))
pub.add_observer(Obs.new(:second))
p pub.changed
p pub.notify_observers("x", "y")
p $log
p pub.changed?`, "true\nfalse\n[[:first, [\"x\", \"y\"]], [:second, [\"x\", \"y\"]]]\nfalse"},
		// after a notify, a second notify is a no-op (changed was reset).
		{pubObs + `pub = Pub.new
pub.add_observer(Obs.new(:a))
pub.changed
pub.notify_observers(1)
$log.clear
p pub.notify_observers(2)
p $log`, "nil\n[]"},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestObservableChangedFalse covers changed(false) clearing a set flag (the
// non-default-argument branch of #changed and its returned state).
func TestObservableChangedFalse(t *testing.T) {
	got := runSrc(t, pubObs+`pub = Pub.new
pub.changed
p pub.changed?
p pub.changed(false)
p pub.changed?`)
	if want := "true\nfalse\nfalse"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestObservableCustomFunc covers add_observer with an explicit method name: the
// returned symbol is the custom func and notify dispatches that method.
func TestObservableCustomFunc(t *testing.T) {
	got := runSrc(t, pubObs+`pub = Pub.new
p pub.add_observer(Obs.new(:a), :go)
pub.changed
pub.notify_observers(7)
p $log`)
	if want := ":go\n[[:a, :go, [7]]]"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestObservableFuncString covers a String func argument (coerced like a Symbol)
// — the *object.String branch of methodNameArg.
func TestObservableFuncString(t *testing.T) {
	got := runSrc(t, pubObs+`pub = Pub.new
p pub.add_observer(Obs.new(:a), "go")
pub.changed
pub.notify_observers(9)
p $log`)
	if want := ":go\n[[:a, :go, [9]]]"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestObservableDelete covers delete_observer: removing a registered observer
// returns its func and drops the count; removing an unregistered observer
// returns nil and leaves the count; the removed observer is no longer notified.
func TestObservableDelete(t *testing.T) {
	got := runSrc(t, pubObs+`pub = Pub.new
a = Obs.new(:a)
b = Obs.new(:b)
pub.add_observer(a)
pub.add_observer(b, :go)
p pub.delete_observer(b)
p pub.delete_observer(Obs.new(:never))
p pub.count_observers
pub.changed
pub.notify_observers(1)
p $log`)
	want := ":go\nnil\n1\n[[:a, [1]]]"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestObservableDeleteObservers covers delete_observers clearing every observer
// and returning the now-empty hash {} (matching MRI's @observer_peers.clear).
func TestObservableDeleteObservers(t *testing.T) {
	got := runSrc(t, pubObs+`pub = Pub.new
pub.add_observer(Obs.new(:a))
pub.add_observer(Obs.new(:b))
p pub.delete_observers
p pub.count_observers
pub.changed
pub.notify_observers(1)
p $log`)
	if want := "{}\n0\n[]"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestObservableReadd covers re-adding an already-registered observer: its
// position is unchanged and its func is updated (the library's Hash-keyed store
// semantics), and rbgo's func table tracks the new func for delete's return.
func TestObservableReadd(t *testing.T) {
	got := runSrc(t, pubObs+`pub = Pub.new
a = Obs.new(:a)
pub.add_observer(a)
pub.add_observer(Obs.new(:b))
p pub.add_observer(a, :go)
p pub.count_observers
p pub.delete_observer(a)
pub.changed
pub.notify_observers(5)
p $log`)
	// re-add returns :go, count stays 2, delete returns the updated :go, and only
	// :b (still :update) is notified after a is removed.
	if want := ":go\n2\n:go\n[[:b, [5]]]"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestObservableNonResponding covers add_observer raising NoMethodError, with
// MRI's verbatim message, when the observer does not respond to the func.
func TestObservableNonResponding(t *testing.T) {
	cases := []struct{ src, want string }{
		// default func :update on a bare Object.
		{pubObs + `pub = Pub.new
begin
  pub.add_observer(Object.new)
rescue NoMethodError => e
  p e.message
end`, "\"observer does not respond to `update'\""},
		// explicit func the observer lacks.
		{pubObs + `pub = Pub.new
begin
  pub.add_observer(Obs.new(:a), :missing)
rescue NoMethodError => e
  p e.message
end`, "\"observer does not respond to `missing'\""},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestObservableBadFuncArg covers methodNameArg's TypeError branch: a func
// argument that is neither a Symbol nor a String.
func TestObservableBadFuncArg(t *testing.T) {
	got := runSrc(t, pubObs+`pub = Pub.new
begin
  pub.add_observer(Obs.new(:a), 123)
rescue TypeError => e
  p e.message
end`)
	if want := "\"123 is not a symbol nor a string\""; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestObservableRequire covers the require contract: "observer" is a provided
// feature (true on first require, false after), and Observable is a module.
func TestObservableRequire(t *testing.T) {
	got := runSrc(t, `p require "observer"
p require "observer"
p Observable.class`)
	if want := "true\nfalse\nModule"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestObservableBoxNative covers the observerBox's never-user-visible Value
// surface (ToS/Inspect/Truthy) directly, since no Ruby path returns the box.
func TestObservableBoxNative(t *testing.T) {
	b := &observerBox{}
	if b.ToS() != "#<Observable state>" || b.Inspect() != "#<Observable state>" || !b.Truthy() {
		t.Fatalf("box surface: ToS=%q Inspect=%q Truthy=%v", b.ToS(), b.Inspect(), b.Truthy())
	}
}
