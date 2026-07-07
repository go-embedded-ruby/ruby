// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io"
	"testing"

	aasm "github.com/go-ruby-aasm/aasm"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// aasmSrc prefixes a program with require "aasm".
func aasmSrc(body string) string { return "require \"aasm\"\n" + body }

// TestAASMRequire covers the feature registration and the exposed constants.
func TestAASMRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "aasm"`, "true\n"},
		{`require "aasm"; p require "aasm"`, "false\n"},
		{`require "aasm"; p AASM.is_a?(Module)`, "true\n"},
		{`require "aasm"; p AASM::Error < StandardError`, "true\n"},
		{`require "aasm"; p AASM::InvalidTransition < AASM::Error`, "true\n"},
		{`require "aasm"; p AASM::UndefinedState < AASM::Error`, "true\n"},
		{`require "aasm"; p AASM::InstanceBase.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestAASMBasic covers the core surface: initial state, per-state predicates, the
// generated fire / fire! / may_fire? event methods, and the aasm reader.
func TestAASMBasic(t *testing.T) {
	got := eval(t, aasmSrc(`
class Light
  include AASM
  aasm do
    state :sleeping, initial: true
    state :running
    state :cleaning
    event :run do
      transitions from: :sleeping, to: :running
    end
    event :clean do
      transitions from: :running, to: :cleaning
    end
    event :sleep do
      transitions from: [:running, :cleaning], to: :sleeping
    end
  end
end
l = Light.new
p l.sleeping?
p l.aasm.current_state
p l.may_run?
p l.may_clean?
p l.run
p l.running?
p l.aasm.current_state
p l.clean
p l.aasm.current_state
p l.sleep
p l.sleeping?
p l.aasm_state
p Light.aasm.states
p Light.aasm.events
p Light.aasm.initial_state
`))
	want := "true\n:sleeping\ntrue\nfalse\ntrue\ntrue\n:running\ntrue\n:cleaning\ntrue\ntrue\n:sleeping\n[:sleeping, :running, :cleaning]\n[:run, :clean, :sleep]\n:sleeping\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestAASMGuards covers symbol guards, block guards, unless guards and the whiny
// InvalidTransition raise a blocked transition triggers by default.
func TestAASMGuards(t *testing.T) {
	got := eval(t, aasmSrc(`
class Door
  include AASM
  attr_accessor :ok, :locked
  aasm do
    state :closed, initial: true
    state :open
    event :unlock do
      transitions from: :closed, to: :open, guard: :ok?, unless: :locked?
    end
    event :force do
      transitions from: :closed, to: :open, guard: -> { ok }
    end
  end
  def ok?; @ok; end
  def locked?; @locked; end
end
d = Door.new
p d.may_unlock?
begin
  d.unlock
rescue AASM::InvalidTransition => e
  puts "blocked"
end
d.ok = true
p d.may_unlock?
p d.unlock
p d.open?

d2 = Door.new
d2.ok = true
d2.locked = true
p d2.may_unlock?
p d2.force
`))
	want := "false\nblocked\ntrue\ntrue\ntrue\nfalse\ntrue\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestAASMCallbacks covers the callback ordering: event/transition before &
// after, state enter & exit, event-level success — declared both inline (kwargs)
// and via the event-block DSL.
func TestAASMCallbacks(t *testing.T) {
	got := eval(t, aasmSrc(`
class Machine
  include AASM
  aasm do
    state :a, initial: true, exit: :log_exit
    state :b, enter: :log_enter
    event :go do
      transitions from: :a, to: :b, before: :t_before, after: :t_after
      before :e_before
      after :e_after
      success { puts "success" }
    end
  end
  def log_exit; puts "exit a"; end
  def log_enter; puts "enter b"; end
  def t_before; puts "t_before"; end
  def t_after; puts "t_after"; end
  def e_before; puts "e_before"; end
  def e_after; puts "e_after"; end
end
Machine.new.go
`))
	want := "e_before\nt_before\nexit a\nenter b\nt_after\ne_after\nsuccess\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestAASMPersist covers fire! -> Persist (save! preferred, else save) and that
// the plain fire never persists.
func TestAASMPersist(t *testing.T) {
	got := eval(t, aasmSrc(`
class Order
  include AASM
  aasm do
    state :draft, initial: true
    state :placed
    state :shipped
    event :place do
      transitions from: :draft, to: :placed
    end
    event :ship do
      transitions from: :placed, to: :shipped
    end
  end
  def save; puts "save"; end
  def save!; puts "save!"; end
end
o = Order.new
o.place       # fire, no persist
puts o.aasm.current_state
o.ship!       # fire!, persists via save!
puts o.aasm.current_state

class Plain
  include AASM
  aasm do
    state :x, initial: true
    state :y
    event :go do
      transitions from: :x, to: :y
    end
  end
  def save; puts "plain-save"; end
end
Plain.new.go!   # only #save defined
`))
	want := "placed\nsave!\nshipped\nplain-save\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestAASMErrorCallback covers routing a Ruby raise inside a callback to the
// event error handler (which may swallow it) and re-raising when it does not.
func TestAASMErrorCallback(t *testing.T) {
	got := eval(t, aasmSrc(`
class Job
  include AASM
  aasm do
    state :new, initial: true
    state :done
    event :finish do
      transitions from: :new, to: :done, after: :boom
      error :handle
    end
  end
  def boom; raise "kaboom"; end
  def handle(e); puts "rescued: #{e.message}"; end
end
j = Job.new
j.finish
puts j.aasm.current_state
`))
	want := "rescued: kaboom\ndone\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}

	// An error handler that re-raises propagates the exception.
	class_, msg := evalErr(t, aasmSrc(`
class Job2
  include AASM
  aasm do
    state :new, initial: true
    state :done
    event :finish do
      transitions from: :new, to: :done, after: :boom
      error :reraise
    end
  end
  def boom; raise "kaboom"; end
  def reraise(e); raise e; end
end
Job2.new.finish
`))
	if class_ != "RuntimeError" || msg != "kaboom" {
		t.Errorf("re-raise: class=%q msg=%q", class_, msg)
	}

	// With no error handler, a callback raise propagates unchanged.
	class_, msg = evalErr(t, aasmSrc(`
class Job3
  include AASM
  aasm do
    state :new, initial: true
    state :done
    event :finish do
      transitions from: :new, to: :done, after: :boom
    end
  end
  def boom; raise ArgumentError, "bad"; end
end
Job3.new.finish
`))
	if class_ != "ArgumentError" || msg != "bad" {
		t.Errorf("propagate: class=%q msg=%q", class_, msg)
	}
}

// TestAASMNamedMachine covers multiple named machines on one class, the column:
// option, and the per-machine reader accessed through aasm(:name).
func TestAASMNamedMachine(t *testing.T) {
	got := eval(t, aasmSrc(`
class Report
  include AASM
  aasm do
    state :new, initial: true
    state :seen
    event :view do
      transitions from: :new, to: :seen
    end
  end
  aasm(:approval, column: :approval_state) do
    state :pending, initial: true
    state :approved
    event :approve do
      transitions from: :pending, to: :approved
    end
  end
end
r = Report.new
p r.aasm.current_state
p r.aasm(:approval).current_state
r.view
r.approve
p r.aasm_state
p r.approval_state
p r.aasm(:approval).states
p r.aasm(:approval).events
`))
	want := ":new\n:pending\n:seen\n:approved\n[:pending, :approved]\n[:approve]\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestAASMReader covers the aasm reader API: current_state=, permitted_events,
// fire / fire! / may_fire?, and permitted_events narrowing under a guard.
func TestAASMReader(t *testing.T) {
	got := eval(t, aasmSrc(`
class Flow
  include AASM
  attr_accessor :ready
  aasm do
    state :one, initial: true
    state :two
    state :three
    event :step do
      transitions from: :one, to: :two, guard: :ready?
    end
    event :jump do
      transitions from: :one, to: :three
    end
  end
  def ready?; @ready; end
end
f = Flow.new
p f.aasm.permitted_events
f.ready = true
p f.aasm.permitted_events
p f.aasm.may_fire?(:step)
p f.aasm.fire(:step)
p f.aasm.current_state
f.aasm.current_state = :three
p f.aasm.current_state
begin
  f.aasm.fire!(:jump)
rescue AASM::InvalidTransition
  puts "no jump from three"
end
`))
	want := "[:jump]\n[:step, :jump]\ntrue\ntrue\n:two\n:three\nno jump from three\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestAASMWhinyOff covers whiny_transitions: false — a blocked / invalid
// transition returns false rather than raising.
func TestAASMWhinyOff(t *testing.T) {
	got := eval(t, aasmSrc(`
class Quiet
  include AASM
  aasm whiny_transitions: false do
    state :a, initial: true
    state :b
    event :go do
      transitions from: :b, to: :a
    end
  end
end
q = Quiet.new
p q.go
p q.go!
p q.a?
`))
	want := "false\nfalse\ntrue\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestAASMSubclassInherit covers a subclass inheriting a machine defined on its
// parent, including the after_commit callback (fire! only) and event-level guard.
func TestAASMSubclassInherit(t *testing.T) {
	got := eval(t, aasmSrc(`
class Base
  include AASM
  attr_accessor :allow
  aasm do
    state :s0, initial: true
    state :s1
    event :advance, guard: :allow? do
      transitions from: :s0, to: :s1
      after_commit { puts "committed" }
    end
  end
  def allow?; @allow; end
  def save; end
end
class Sub < Base
end
s = Sub.new
p s.may_advance?
s.allow = true
s.advance!
p s.s1?
`))
	want := "false\ncommitted\ntrue\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestAASMUndefinedEvent covers firing an undefined event through the reader,
// which raises AASM::UndefinedState under whiny transitions.
func TestAASMUndefinedEvent(t *testing.T) {
	class_, _ := evalErr(t, aasmSrc(`
class Tiny
  include AASM
  aasm do
    state :a, initial: true
  end
end
Tiny.new.aasm.fire(:nope)
`))
	if class_ != "AASM::UndefinedState" {
		t.Errorf("undefined event: class=%q", class_)
	}
}

// TestAASMTransitionExtras covers transition-level success callbacks, a final
// state, and fire arguments threaded to a callback.
func TestAASMTransitionExtras(t *testing.T) {
	got := eval(t, aasmSrc(`
class Payment
  include AASM
  aasm do
    state :pending, initial: true
    state :paid, final: true
    event :pay do
      transitions from: :pending, to: :paid, success: :log_success
      after :record
    end
  end
  def log_success(*); puts "success"; end
  def record(amount); puts "recorded #{amount}"; end
end
p = Payment.new
p.pay(42)
p p.paid?
`))
	want := "recorded 42\nsuccess\ntrue\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestAASMRaisingGuard covers a guard that raises: through the generated may_/
// event methods and through the aasm reader, all of which propagate the error.
func TestAASMRaisingGuard(t *testing.T) {
	got := eval(t, aasmSrc(`
class Risky
  include AASM
  aasm do
    state :a, initial: true
    state :b
    event :go do
      transitions from: :a, to: :b, guard: :boom?
    end
  end
  def boom?; raise "guard fail"; end
end
r = Risky.new
begin; r.may_go?; rescue RuntimeError; puts "may raised"; end
begin; r.go; rescue RuntimeError; puts "go raised"; end
begin; r.aasm.may_fire?(:go); rescue RuntimeError; puts "reader may raised"; end
`))
	want := "may raised\ngo raised\nreader may raised\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestAASMClassReaderNoObj covers the class-level (object-less) aasm reader arms:
// current_state / initial_state / permitted_events / may_fire? with no bound
// object, for a machine with and without an initial state, plus the column
// reader defaulting to nil when no initial state is declared.
func TestAASMClassReaderNoObj(t *testing.T) {
	got := eval(t, aasmSrc(`
class NoInit
  include AASM
  aasm do
    state :a
    state :b
    event :go do
      transitions from: :a, to: :b
    end
  end
end
p NoInit.aasm.current_state
p NoInit.aasm.initial_state
p NoInit.aasm.permitted_events
p NoInit.aasm.may_fire?(:go)
p NoInit.new.aasm_state

class HasInit
  include AASM
  aasm do
    state :a, initial: true
    state :b
    event :go do
      transitions from: :a, to: :b
    end
  end
end
p HasInit.aasm.current_state
`))
	want := "nil\nnil\n[]\nfalse\nnil\n:a\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// --- Go-level tests for branches the Ruby surface does not exercise ---

// TestAASMSpecHelpers covers the spec lookups on paths a normal program skips
// (an absent state / event / machine name).
func TestAASMSpecHelpers(t *testing.T) {
	m := &aasmMachineSpec{name: "m"}
	if m.stateSpec("x") != nil || m.eventSpec("y") != nil || m.initialState() != "" {
		t.Error("absent lookups should be nil/empty")
	}
	m.states = []aasmStateSpec{{name: "s"}}
	m.events = []aasmEventSpec{{name: "e"}}
	if m.stateSpec("s") == nil || m.eventSpec("e") == nil {
		t.Error("present lookups should resolve")
	}
	vm := New(io.Discard)
	if vm.aasmSpecFor(vm.cObject, "missing") != nil {
		t.Error("aasmSpecFor with no machines should be nil")
	}
}

// TestAASMMachineName covers aasmMachineName's String arm and the empty default.
func TestAASMMachineName(t *testing.T) {
	if aasmMachineName([]object.Value{object.NewString("s")}) != "s" {
		t.Error("string machine name")
	}
	if aasmMachineName([]object.Value{object.Symbol("y")}) != "y" {
		t.Error("symbol machine name")
	}
	if aasmMachineName(nil) != "" {
		t.Error("default machine name")
	}
}

// TestAASMArgsToRuby covers the engine-arg conversion, including the arm that
// skips a non-object.Value element.
func TestAASMArgsToRuby(t *testing.T) {
	got := aasmArgsToRuby([]any{object.Symbol("x"), 42, object.NewString("y")})
	if len(got) != 2 || got[0] != object.Symbol("x") {
		t.Errorf("argsToRuby = %+v", got)
	}
}

// TestAASMSeamsIvarFallback covers the seam ivar path taken when the host object
// has no column accessor method (GetState / SetState fall back to the ivar), and
// the Persist no-op when neither save! nor save is defined.
func TestAASMSeamsIvarFallback(t *testing.T) {
	vm := New(io.Discard)
	spec := &aasmMachineSpec{name: "", column: "aasm_state"}
	obj := &RObject{class: vm.cObject, ivars: map[string]object.Value{}}
	seams := vm.aasmSeams(spec, obj)
	if seams.GetState() != "" {
		t.Error("blank ivar should read as empty")
	}
	if err := seams.SetState("running"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if seams.GetState() != "running" {
		t.Errorf("ivar read-back = %q", seams.GetState())
	}
	if err := seams.Persist(); err != nil {
		t.Errorf("Persist no-op should be nil, got %v", err)
	}
}

// TestAASMRubyErrError covers the aasmRubyErr Error() accessor.
func TestAASMRubyErrError(t *testing.T) {
	e := aasmRubyErr{re: RubyError{Class: "ArgumentError", Message: "bad"}}
	if e.Error() != "ArgumentError: bad" {
		t.Errorf("Error() = %q", e.Error())
	}
}

// TestAASMCbSpecsAndNames covers the DSL-value readers on the arms Ruby rarely
// reaches: an empty/unknown value, an Array of names, and a single name.
func TestAASMCbSpecsAndNames(t *testing.T) {
	if aasmCbSpecs(nil) != nil || aasmCbSpecs(object.NilV) != nil {
		t.Error("nil callback spec should be nil")
	}
	if aasmCbSpecs(object.IntValue(3)) != nil {
		t.Error("unknown-type callback spec should be nil")
	}
	if got := aasmCbSpecs(object.NewString("m")); len(got) != 1 || got[0].sym != "m" {
		t.Errorf("string spec = %+v", got)
	}
	arr := object.NewArrayFromSlice([]object.Value{object.Symbol("a"), object.Symbol("b")})
	if got := aasmCbSpecs(arr); len(got) != 2 {
		t.Errorf("array spec = %+v", got)
	}
	if got := aasmNames(object.Symbol("only")); len(got) != 1 || got[0] != "only" {
		t.Errorf("single name = %+v", got)
	}
	if got := aasmNames(arr); len(got) != 2 {
		t.Errorf("array names = %+v", got)
	}
}

// TestAASMStateStr covers aasmStateStr across nil, Symbol, String and the generic
// ToS fallback (a non-string state value stored in the column ivar).
func TestAASMStateStr(t *testing.T) {
	if aasmStateStr(nil) != "" || aasmStateStr(object.NilV) != "" {
		t.Error("nil state should be empty")
	}
	if aasmStateStr(object.Symbol("s")) != "s" || aasmStateStr(object.NewString("s")) != "s" {
		t.Error("symbol/string state")
	}
	if aasmStateStr(object.IntValue(7)) != "7" {
		t.Error("generic ToS fallback")
	}
}

// TestAASMArgOrBlock covers the event-level callback reader for the method-name
// argument arm (the block arm is covered through the DSL).
func TestAASMArgOrBlock(t *testing.T) {
	got := aasmArgOrBlock([]object.Value{object.Symbol("a"), object.Symbol("b")}, nil)
	if len(got) != 2 || got[0].sym != "a" || got[1].sym != "b" {
		t.Errorf("arg form = %+v", got)
	}
}

// TestAASMRaiseAndErrConv covers the transition-error mapping and the exception
// conversion on the arms only reachable with a synthetic engine error: a bare
// sentinel-free error (AASM::Error), a re-raised Ruby error, and a fresh-object
// fallback in aasmErrToRuby.
func TestAASMRaiseAndErrConv(t *testing.T) {
	vm := New(io.Discard)

	// A plain (non-sentinel, non-Ruby) error maps to AASM::Error.
	func() {
		defer func() {
			r := recover()
			re, ok := r.(RubyError)
			if !ok || re.Class != "AASM::Error" {
				t.Errorf("plain error: %#v", r)
			}
		}()
		vm.aasmRaise(errors.New("boom"))
	}()

	// A wrapped Ruby error is re-raised unchanged.
	func() {
		defer func() {
			r := recover()
			re, ok := r.(RubyError)
			if !ok || re.Class != "ZeroDivisionError" {
				t.Errorf("ruby re-raise: %#v", r)
			}
		}()
		vm.aasmRaise(aasmRubyErr{re: RubyError{Class: "ZeroDivisionError", Message: "x"}})
	}()

	// aasmErrToRuby builds a fresh AASM::Error object for a non-Ruby error.
	obj := vm.aasmErrToRuby(errors.New("plain"))
	if vm.classOf(obj).name != "AASM::Error" {
		t.Errorf("errToRuby fresh: %s", vm.classOf(obj).name)
	}
	// And rebuilds one from a Ruby error that carried no object.
	obj = vm.aasmErrToRuby(aasmRubyErr{re: RubyError{Class: "AASM::Error", Message: "m"}})
	if vm.classOf(obj).name != "AASM::Error" {
		t.Errorf("errToRuby rebuilt: %s", vm.classOf(obj).name)
	}
}

// TestAASMBuildExceptionFallback covers buildException's fallback to RuntimeError
// for an unknown class name.
func TestAASMBuildExceptionFallback(t *testing.T) {
	vm := New(io.Discard)
	obj := vm.buildException("No::Such::Class", "msg")
	if vm.classOf(obj).name != "RuntimeError" {
		t.Errorf("fallback class = %s", vm.classOf(obj).name)
	}
}

// TestAASMInvokeNonRubyPanic covers aasmInvoke re-panicking a non-Ruby Go panic
// raised inside a native callback body.
func TestAASMInvokeNonRubyPanic(t *testing.T) {
	vm := New(io.Discard)
	blk := &Proc{native: func(_ *VM, _ []object.Value) object.Value { panic("go-panic") }}
	defer func() {
		if r := recover(); r != "go-panic" {
			t.Errorf("expected re-panic of go-panic, got %#v", r)
		}
	}()
	_, _ = vm.aasmInvoke(aasmCbSpec{blk: blk}, vm.main, "e", nil)
}

// TestAASMErrorCallbackSwallowGo covers aasmErrorCallback returning nil when the
// handler runs cleanly (the swallow path) at the Go seam level.
func TestAASMErrorCallbackSwallowGo(t *testing.T) {
	vm := New(io.Discard)
	blk := &Proc{native: func(_ *VM, _ []object.Value) object.Value { return object.NilV }}
	cb := vm.aasmErrorCallback(aasmCbSpec{blk: blk}, vm.main, "e")
	if err := cb(aasm.ErrInvalidTransition, nil); err != nil {
		t.Errorf("swallow should return nil, got %v", err)
	}
	// And it propagates a Ruby raise from the handler.
	braise := &Proc{native: func(vm *VM, _ []object.Value) object.Value { return raise("RuntimeError", "again") }}
	cb = vm.aasmErrorCallback(aasmCbSpec{blk: braise}, vm.main, "e")
	if err := cb(aasm.ErrInvalidTransition, nil); err == nil {
		t.Error("handler raise should surface an error")
	}
}
