// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestSizedQueueDisplayMarker covers the SizedQueue arm of RQueue.ToS/Inspect,
// which the differential suite cannot reach (the string form has no address).
func TestSizedQueueDisplayMarker(t *testing.T) {
	q := &RQueue{max: 1}
	if q.ToS() != "#<Thread::SizedQueue>" || q.Inspect() != "#<Thread::SizedQueue>" {
		t.Errorf("SizedQueue display = %q / %q", q.ToS(), q.Inspect())
	}
}

// TestClassOfReceiverFallback covers the branch taken when a native `new` runs
// with a non-class receiver: the default class is used.
func TestClassOfReceiverFallback(t *testing.T) {
	def := newClass("Queue", nil)
	if got := classOfReceiver(object.NilVal(), def); got != def {
		t.Errorf("classOfReceiver fallback = %v, want default", got)
	}
	other := newClass("SizedQueue", def)
	if got := classOfReceiver(other, def); got != other {
		t.Errorf("classOfReceiver(class) = %v, want the receiver class", got)
	}
}

// TestClassOfBareQueue covers classOf for an RQueue with no class stamped (a
// zero-value struct never produced by Ruby code): it reports Queue.
func TestClassOfBareQueue(t *testing.T) {
	vm := New(io.Discard)
	if got := vm.classOf(&RQueue{}); got != vm.consts["Queue"].(*RClass) {
		t.Errorf("classOf(&RQueue{}) = %v, want Queue", got)
	}
}
