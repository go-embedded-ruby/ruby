// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestExconValueProtocol covers the ToS / Inspect / Truthy arms of the Excon
// wrappers.
func TestExconValueProtocol(t *testing.T) {
	vals := []struct {
		v    object.Value
		want string
	}{
		{&ExconConnection{}, "#<Excon::Connection>"},
		{&ExconResponse{}, "#<Excon::Response>"},
	}
	for _, c := range vals {
		if c.v.ToS() != c.want || c.v.Inspect() != c.want || !c.v.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c.v, c.v.ToS(), c.v.Inspect(), c.v.Truthy())
		}
	}
}

// TestExconNameArm covers exconName's String arm (the Symbol arm is exercised from
// Ruby by the :method option).
func TestExconNameArm(t *testing.T) {
	if got := exconName(object.NewString("get")); got != "get" {
		t.Errorf("string arm got=%q", got)
	}
	if got := exconName(object.Symbol("get")); got != "get" {
		t.Errorf("symbol arm got=%q", got)
	}
}

// TestExconSimpleName covers exconSimpleName, including the no-"::" fallback (a
// bare name returned unchanged).
func TestExconSimpleName(t *testing.T) {
	if got := exconSimpleName("Excon::Error::NotFound"); got != "NotFound" {
		t.Errorf("nested got=%q", got)
	}
	if got := exconSimpleName("Error"); got != "Error" {
		t.Errorf("bare got=%q", got)
	}
}

// TestExconOptionsNil covers rubyHashToExconOptions with an absent Hash (the zero
// Options) and the empty-args path of exconNewArgs (no URL, no options).
func TestExconOptionsNil(t *testing.T) {
	if o := rubyHashToExconOptions(nil); o.Method != "" || o.Path != "" || o.Query != nil || o.Headers != nil || o.Expects != nil {
		t.Errorf("nil hash got=%+v", o)
	}
	if url, o := exconNewArgs(nil); url != "" || o.Method != "" || o.Expects != nil {
		t.Errorf("empty args got url=%q o=%+v", url, o)
	}
}

// TestExconExpectsArms covers both exconExpects arms directly: a single Integer
// and an Array of Integers.
func TestExconExpectsArms(t *testing.T) {
	if got := exconExpects(object.Integer(200)); len(got) != 1 || got[0] != 200 {
		t.Errorf("scalar arm got=%v", got)
	}
	arr := object.NewArrayFromSlice([]object.Value{object.Integer(200), object.Integer(201)})
	if got := exconExpects(arr); len(got) != 2 || got[0] != 200 || got[1] != 201 {
		t.Errorf("array arm got=%v", got)
	}
}
