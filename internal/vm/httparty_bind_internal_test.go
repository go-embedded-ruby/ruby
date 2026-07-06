// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"testing"

	httparty "github.com/go-ruby-httparty/httparty"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestHTTPartyValueProtocol covers the ToS / Inspect / Truthy arms of the
// HTTParty::Response wrapper, exercised directly for completeness.
func TestHTTPartyValueProtocol(t *testing.T) {
	v := &HTTPartyResponse{}
	if v.ToS() != "#<HTTParty::Response>" || v.Inspect() != "#<HTTParty::Response>" || !v.Truthy() {
		t.Errorf("ToS=%q Inspect=%q Truthy=%v", v.ToS(), v.Inspect(), v.Truthy())
	}
}

// TestHTTPartyDispatchUnknown covers httpartyDispatch's default arm, unreachable
// from Ruby because the verb name always comes from rbgo's fixed set.
func TestHTTPartyDispatchUnknown(t *testing.T) {
	resp, err := httpartyDispatch(httparty.NewClient(), "bogus", "/x", httparty.RequestOptions{})
	if resp != nil || err != nil {
		t.Errorf("httpartyDispatch bogus got resp=%v err=%v", resp, err)
	}
}

// TestHTTPartyNameArm covers httpartyName's String arm (the Symbol arm is
// exercised from Ruby via the format DSL / option).
func TestHTTPartyNameArm(t *testing.T) {
	if got := httpartyName(object.NewString("json")); got != "json" {
		t.Errorf("string arm got=%q", got)
	}
	if got := httpartyName(object.Symbol("xml")); got != "xml" {
		t.Errorf("symbol arm got=%q", got)
	}
}

// TestHTTPartyHeadersNil covers the nil-Headers guard in httpartyHeadersToRubyHash
// (a defensive arm the library's non-nil response headers never reach).
func TestHTTPartyHeadersNil(t *testing.T) {
	if h, ok := httpartyHeadersToRubyHash(nil).(*object.Hash); !ok || h.Len() != 0 {
		t.Errorf("nil headers got=%v", httpartyHeadersToRubyHash(nil))
	}
}

// TestHTTPartyRaiseUnknownKind covers httpartyRaise's fallback arm — an Error
// whose Kind has no registered Ruby class maps to HTTParty::Error — unreachable
// from Ruby because every library kind is registered.
func TestHTTPartyRaiseUnknownKind(t *testing.T) {
	vm := New(&bytes.Buffer{})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected httpartyRaise to panic")
		}
		re, ok := r.(RubyError)
		if !ok {
			t.Fatalf("expected RubyError, got %T", r)
		}
		if cls := vm.classOf(re.Obj); cls.name != "HTTParty::Error" {
			t.Errorf("unknown kind mapped to %q, want HTTParty::Error", cls.name)
		}
	}()
	vm.httpartyRaise(&httparty.Error{Kind: "HTTParty::Nonexistent", Message: "boom"})
}
