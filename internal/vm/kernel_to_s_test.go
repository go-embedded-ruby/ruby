// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestKernelToSPrintsTheAddress (#756).
//
// MRI's rb_any_to_s ALWAYS prints the object's address, and Kernel#to_s is
// rb_any_to_s. rbgo printed the short "#<Plain>" form, so #to_s and #inspect
// disagreed with each other INSIDE rbgo while #inspect agreed with MRI.
//
// The shape is asserted in Ruby rather than by pinning bytes, because the
// address changes every run -- but the PATTERN is exact: "#<", the class name,
// ":0x", sixteen hex digits, ">". A case that merely checked for "0x" would
// pass on a truncated or malformed address.
//
// SCOPE, and it is narrower than MRI's. The fix applies to a plain *RObject
// only. Thirteen core classes reach Kernel#to_s here (Binding,
// ConditionVariable, Dir, File, IO, Monitor, Mutex, Numeric, Object, Queue,
// Random, SizedQueue, TracePoint) and MRI gives all thirteen an address, but
// most of them are Go types whose own ToS() is their #to_s -- rbgo implements
// many objects' to_s as the Go method rather than as a registered Ruby one.
//
// A first attempt applied the identity form to everything reaching Kernel#to_s
// and broke 78 tests, every one of them rightly: Capistrano::Server#to_s is its
// hostname, Net::SMTP::Response#to_s its status line, the top-level self "main".
// So the remaining classes still answer their short form and are out of scope
// here; they need an address their ToS() cannot compute, having no VM.
func TestKernelToSPrintsTheAddress(t *testing.T) {
	checkCases(t, []runCase{
		{`class Plain; end
p Plain.new.to_s =~ /\A#<Plain:0x[0-9a-f]{16}>\z/ ? :ok : Plain.new.to_s`, ":ok\n"},
		{`p Object.new.to_s =~ /\A#<Object:0x[0-9a-f]{16}>\z/ ? :ok : Object.new.to_s`, ":ok\n"},
		// puts and interpolation dispatch #to_s, so fixing the method fixes them.
		// Without these a fix could leave the three paths disagreeing.
		{`class P2; end
s = "#{P2.new}"
p s =~ /\A#<P2:0x[0-9a-f]{16}>\z/ ? :ok : s`, ":ok\n"},
		// An object whose class is built in Ruby, to show this is about the
		// RECEIVER's representation and not about where the class came from.
		{`class Outer; class Inner; end; end
s = Outer::Inner.new.to_s
p s =~ /\A#<Outer::Inner:0x[0-9a-f]{16}>\z/ ? :ok : s`, ":ok\n"},
		// #to_s and #inspect must now agree with each other, which was the
		// internal contradiction #756 opened with.
		{`class P4; end
o = P4.new
p o.to_s == o.inspect`, "true\n"},
	})
}

// TestKernelToSLeavesEveryTypeThatOwnsOneAlone is the control, and it is the
// half that matters: Kernel#to_s is inherited by everything, so a change to it
// is a change to every receiver that does not override it. These do override
// it, and each expectation is MRI 4.0.7's output.
func TestKernelToSLeavesEveryTypeThatOwnsOneAlone(t *testing.T) {
	checkCases(t, []runCase{
		{`print 42.to_s`, "42"},
		{`print 1.5.to_s`, "1.5"},
		{`print [1, 2].to_s`, "[1, 2]"},
		{`print({a: 1}.to_s)`, "{a: 1}"},
		{`print :sym.to_s`, "sym"},
		{`print (1..2).to_s`, "1..2"},
		{`print nil.to_s.inspect`, `""`},
		{`print true.to_s`, "true"},
		{`print String.to_s`, "String"},
		// A built-in value subclass renders as its value, not as an identity:
		// it inherits String#to_s rather than Kernel's.
		{`class MyStr < String; end
print MyStr.new("x").to_s`, "x"},
		{`class MyArr < Array; end
print MyArr.new([1]).to_s`, "[1]"},
		// A user-defined to_s still wins, for all three paths.
		{`class Q; def to_s; "CUSTOM"; end; end
print Q.new.to_s, " ", "#{Q.new}"`, "CUSTOM CUSTOM"},
	})
}

// TestThreadNestedClassesCarryTheirQualifiedName (#759, kind A).
//
// Thread::Mutex and three siblings answered their BARE name. In MRI the class
// has one name -- the qualified one -- and the top-level `Mutex` is a second
// constant pointing at the same class, so `Mutex.name` is "Thread::Mutex"
// however it was reached.
//
// This sits with #756 rather than apart from it because the two meet on one
// line of output: once Kernel#to_s builds its string from classOf(self).name
// instead of a hardcoded "#<Thread::Mutex>", a bare class name becomes visible
// as `#<Mutex:0x…>`. Fixing one without the other trades a missing address for
// a wrong name.
func TestThreadNestedClassesCarryTheirQualifiedName(t *testing.T) {
	checkCases(t, []runCase{
		{`p [Mutex.name, Queue.name, SizedQueue.name, ConditionVariable.name]`,
			`["Thread::Mutex", "Thread::Queue", "Thread::SizedQueue", "Thread::ConditionVariable"]` + "\n"},
		{`p [Thread::Mutex.name, Thread::Queue.name]`,
			`["Thread::Mutex", "Thread::Queue"]` + "\n"},
		// One class, two constants -- the thing that makes a single qualified
		// name correct rather than a lie about where it lives.
		{`p [Mutex.equal?(Thread::Mutex), Queue.equal?(Thread::Queue)]`, "[true, true]\n"},
		// They still work, which a rename could plausibly have broken.
		{`q = Queue.new; q.push 1; p q.pop`, "1\n"},
		{`m = Mutex.new; m.lock; m.unlock; p m.locked?`, "false\n"},
		{`sq = SizedQueue.new(1); sq.push 7; p [sq.max, sq.pop]`, "[1, 7]\n"},
	})
}
