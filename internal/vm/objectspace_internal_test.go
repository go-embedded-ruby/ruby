// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"strings"
	"testing"
	"weak"
)

// TestCollectLiveDropsCollectedEntries exercises collectLive's compaction of weak
// entries whose referent is gone: a zero weak.Pointer resolves to nil, so it must
// be dropped from both the object and class registries while a live entry is kept
// and yielded.
func TestCollectLiveDropsCollectedEntries(t *testing.T) {
	vm := New(io.Discard)
	live := &RObject{class: vm.cObject}
	vm.liveObjs = append(vm.liveObjs[:0], weak.Pointer[RObject]{}, weak.Make(live))
	vm.liveClasses = append(vm.liveClasses[:0], weak.Pointer[RClass]{})

	out := vm.collectLive(nil)
	found := false
	for _, v := range out {
		if v == live {
			found = true
		}
	}
	if !found {
		t.Fatalf("collectLive did not yield the live object")
	}
	if len(vm.liveObjs) != 1 {
		t.Errorf("liveObjs not compacted: got %d, want 1", len(vm.liveObjs))
	}
	if len(vm.liveClasses) != 0 {
		t.Errorf("liveClasses not compacted: got %d, want 0", len(vm.liveClasses))
	}
}

// TestWeakMapGoValueMethods covers the object.Value interface methods on the weak
// map value types, which Ruby dispatch never reaches (WeakMap#inspect/#to_s are
// Ruby-level and truthiness is intrinsic).
func TestWeakMapGoValueMethods(t *testing.T) {
	wm := &WeakMapObj{}
	if !wm.Truthy() || !strings.Contains(wm.ToS(), "WeakMap") || wm.Inspect() != wm.ToS() {
		t.Errorf("WeakMapObj value methods: ToS=%q Inspect=%q Truthy=%v", wm.ToS(), wm.Inspect(), wm.Truthy())
	}
	km := &WeakKeyMapObj{}
	if !km.Truthy() || !strings.Contains(km.ToS(), "WeakKeyMap") || km.Inspect() != km.ToS() {
		t.Errorf("WeakKeyMapObj value methods: ToS=%q Inspect=%q Truthy=%v", km.ToS(), km.Inspect(), km.Truthy())
	}
}
