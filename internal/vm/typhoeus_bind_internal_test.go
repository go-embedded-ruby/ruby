// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"
	"time"

	typhoeus "github.com/go-ruby-typhoeus/typhoeus"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestTyphoeusValueProtocol covers the ToS / Inspect / Truthy arms of the Typhoeus
// wrappers.
func TestTyphoeusValueProtocol(t *testing.T) {
	vals := []struct {
		v    object.Value
		want string
	}{
		{&TyphoeusRequest{}, "#<Typhoeus::Request>"},
		{&TyphoeusResponse{}, "#<Typhoeus::Response>"},
		{&TyphoeusHydra{}, "#<Typhoeus::Hydra>"},
	}
	for _, c := range vals {
		if c.v.ToS() != c.want || c.v.Inspect() != c.want || !c.v.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c.v, c.v.ToS(), c.v.Inspect(), c.v.Truthy())
		}
	}
}

// TestTyphoeusNameArm covers typhoeusName's String arm (the Symbol arm is
// exercised from Ruby by the method: option).
func TestTyphoeusNameArm(t *testing.T) {
	if got := typhoeusName(object.NewString("post")); got != "post" {
		t.Errorf("string arm got=%q", got)
	}
	if got := typhoeusName(object.Symbol("post")); got != "post" {
		t.Errorf("symbol arm got=%q", got)
	}
}

// TestTyphoeusMethod covers the verb upper-caser, including an already-upper input
// (the non-lower byte arm).
func TestTyphoeusMethod(t *testing.T) {
	if got := typhoeusMethod("get"); got != "GET" {
		t.Errorf("lower got=%q", got)
	}
	if got := typhoeusMethod("PoSt"); got != "POST" {
		t.Errorf("mixed got=%q", got)
	}
}

// TestTyphoeusOptionsArms covers rubyHashToTyphoeusOptions with an absent Hash
// (zero Options), the full option set, and typhoeusBody's three arms (String /
// Hash form / JSON-ish fallback).
func TestTyphoeusOptionsArms(t *testing.T) {
	if o := rubyHashToTyphoeusOptions(nil); o.Params != nil || o.Headers != nil || o.Body != nil {
		t.Errorf("nil hash got=%+v", o)
	}
	h := object.NewHash()
	h.Set(object.Symbol("timeout"), object.Integer(3))
	h.Set(object.Symbol("followlocation"), object.Bool(true))
	h.Set(object.Symbol("maxredirs"), object.Integer(2))
	h.Set(object.Symbol("userpwd"), object.NewString("u:p"))
	o := rubyHashToTyphoeusOptions(h)
	if o.Timeout != 3*time.Second || !o.FollowLocation || o.MaxRedirects != 2 || o.UserPwd != "u:p" {
		t.Errorf("full opts got=%+v", o)
	}

	if got := typhoeusBody(object.NewString("raw")); got != "raw" {
		t.Errorf("string body got=%v", got)
	}
	fh := object.NewHash()
	fh.Set(object.NewString("a"), object.NewString("1"))
	if got, ok := typhoeusBody(fh).(*typhoeus.Params); !ok || got.Encode() != "a=1" {
		t.Errorf("hash body got=%v", typhoeusBody(fh))
	}
	if got := typhoeusBody(object.Integer(5)); got != int64(5) {
		t.Errorf("fallback body got=%v", got)
	}
}

// TestTyphoeusResponseOrNil covers both arms of typhoeusResponseOrNil.
func TestTyphoeusResponseOrNil(t *testing.T) {
	if got := typhoeusResponseOrNil(nil); got != object.NilV {
		t.Errorf("nil arm got=%v", got)
	}
	if got := typhoeusResponseOrNil(&typhoeus.Response{Code: 200}); got.ToS() != "#<Typhoeus::Response>" {
		t.Errorf("wrap arm got=%v", got)
	}
}
