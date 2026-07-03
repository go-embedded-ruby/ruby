// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	resolv "github.com/go-ruby-resolv/resolv"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestResolvBoxValue covers the resolvBox object.Value methods: it is an internal
// holder (never surfaced to Ruby), so its ToS/Inspect/Truthy are exercised
// directly here.
func TestResolvBoxValue(t *testing.T) {
	b := &resolvBox{}
	if b.ToS() != "#<Resolv native>" || b.Inspect() != "#<Resolv native>" || !b.Truthy() {
		t.Errorf("resolvBox value methods: ToS=%q Inspect=%q Truthy=%v",
			b.ToS(), b.Inspect(), b.Truthy())
	}
}

// TestResolvAccessorFallbacks covers the defensive fallbacks in the resolv
// helpers — the arms taken when the backing native ivar is missing (a never-in-
// practice path, since every wired object is constructed with its box).
func TestResolvAccessorFallbacks(t *testing.T) {
	// An object with no @name box yields the zero Name.
	bare := &RObject{class: nil, ivars: map[string]object.Value{}}
	if n := resolvNameOf(bare); len(n.Labels) != 0 {
		t.Errorf("resolvNameOf(bare) = %#v, want zero Name", n)
	}
	// An object with no @rec box yields a nil Resource.
	if r := recordOf(bare); r != nil {
		t.Errorf("recordOf(bare) = %#v, want nil", r)
	}
}

// TestResolvVMFallbacks covers the VM-scoped fallbacks: a Message/Hosts whose box
// is absent, an unmodelled record TYPE, and a non-class type argument.
func TestResolvVMFallbacks(t *testing.T) {
	vm := New(nil)

	// recordClass falls back to the A class for a TYPE that is not modelled.
	byType := vm.recordClassesByType()
	aCls := byType[resolv.TypeA]
	if got := vm.recordClass(byType, 0xFFFF); got != aCls {
		t.Errorf("recordClass(unmodelled) = %v, want the A class", got)
	}

	// resolvTypeArg returns A for a non-class argument and for an unknown class.
	if got := resolvTypeArg(vm, object.NewString("not a class")); got != resolv.TypeA {
		t.Errorf("resolvTypeArg(String) = %d, want TypeA", got)
	}
	if got := resolvTypeArg(vm, vm.dnsNameClass()); got != resolv.TypeA {
		t.Errorf("resolvTypeArg(unknown class) = %d, want TypeA", got)
	}

	// A record argument that is not a TXT-with-strings still answers "" via the
	// data accessor's empty-strings guard.
	txt := newRecord(byType[resolv.TypeTXT], &resolv.TXT{})
	dataFn := byType[resolv.TypeTXT].methods["data"].native
	if got := dataFn(vm, txt, nil, nil); object.Kind[*object.String](got).Str() != "" {
		t.Errorf("empty TXT data = %q, want \"\"", object.Kind[*object.String](got).Str())
	}
}

// TestResolvMessageBoxAbsent covers the Message/Hosts accessor fallbacks when the
// native box is missing: the binding answers from a fresh empty value rather than
// panicking. The fallbacks are reached by handing a bare RObject of the right
// class to the (otherwise box-backed) native methods.
func TestResolvMessageBoxAbsent(t *testing.T) {
	vm := New(nil)
	dns := object.Kind[*RClass](object.Kind[*RClass](vm.consts["Resolv"]).consts["DNS"])
	msgCls := object.Kind[*RClass](dns.consts["Message"])
	hostsCls := object.Kind[*RClass](object.Kind[*RClass](vm.consts["Resolv"]).consts["Hosts"])

	// Message#id off a box-less object falls back to a fresh Message (id 0).
	bareMsg := &RObject{class: msgCls, ivars: map[string]object.Value{}}
	idFn := msgCls.methods["id"].native
	if got := idFn(vm, bareMsg, nil, nil); object.AsInteger(got) != 0 {
		t.Errorf("box-less Message#id = %v, want 0", got)
	}

	// Hosts#getaddresses off a box-less object falls back to an empty table ([]).
	bareHosts := &RObject{class: hostsCls, ivars: map[string]object.Value{}}
	gaFn := hostsCls.methods["getaddresses"].native
	got := gaFn(vm, bareHosts, []object.Value{object.NewString("x")}, nil)
	if arr, ok := object.KindOK[*object.Array](got); !ok || len(arr.Elems) != 0 {
		t.Errorf("box-less Hosts#getaddresses = %#v, want []", got)
	}
}
