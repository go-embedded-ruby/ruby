// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestDefineMethodTransplantFromBasicObject exercises the BasicObject arm of
// checkTransplantBindable, which nothing in the suite reached: the coverage
// ratchet named it once the Kernel re-homing removed the Object arm beside it,
// and the rounded total could not have.
//
// The arm is on the define_method(:name, a_method) transplant path, NOT on
// UnboundMethod#bind — a distinction worth writing down, because a test written
// against #bind passes, asserts real behaviour, and leaves this block untouched.
//
// It is a real behaviour, not a reachability exercise: a method owned by
// BasicObject transplants onto ANY class, because every class descends from it.
// Both lines below are the byte-for-byte output of ruby 4.0.5.
func TestDefineMethodTransplantFromBasicObject(t *testing.T) {
	src := `class T1; end
T1.send(:define_method, :snd, BasicObject.instance_method(:__send__))
p T1.new.snd(:class)
p T1.instance_method(:snd).owner
`
	const want = "T1\nT1\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestDefineMethodTransplantUnrelatedOwnerRaises is the other side of the same
// guard: an owner that is neither BasicObject nor an ancestor of the target is
// still refused, so the permissive arm above cannot be mistaken for the check
// having been removed.
func TestDefineMethodTransplantUnrelatedOwnerRaises(t *testing.T) {
	src := `class T2; end
class T3; def only_t3; 7; end; end
begin
  T2.send(:define_method, :x, T3.instance_method(:only_t3))
rescue TypeError => e
  puts e.message
end
`
	const want = "bind argument must be a subclass of T3\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
