// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	httprb "github.com/go-ruby-http/http"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestHTTPrbValueProtocol covers the ToS / Inspect / Truthy arms of every http.rb
// wrapper — the object-protocol methods a Ruby-level print/inspect reaches.
func TestHTTPrbValueProtocol(t *testing.T) {
	vals := []struct {
		v    object.Value
		want string
	}{
		{&HTTPrbClient{}, "#<HTTP::Client>"},
		{&HTTPrbResponse{}, "#<HTTP::Response>"},
		{&HTTPrbStatus{}, "#<HTTP::Response::Status>"},
	}
	for _, c := range vals {
		if c.v.ToS() != c.want || c.v.Inspect() != c.want || !c.v.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c.v, c.v.ToS(), c.v.Inspect(), c.v.Truthy())
		}
	}
}

// TestHTTPrbNameArm covers httprbName's String arm (the Symbol arm is exercised
// from Ruby by the chainable accept(:json) DSL).
func TestHTTPrbNameArm(t *testing.T) {
	if got := httprbName(object.NewString("json")); got != "json" {
		t.Errorf("string arm got=%q", got)
	}
	if got := httprbName(object.Symbol("json")); got != "json" {
		t.Errorf("symbol arm got=%q", got)
	}
}

// TestHTTPrbVerbUnknown covers httprbVerb's default arm and httprbApplyChain's
// fallback arm — both unreachable from Ruby because the name always comes from
// rbgo's fixed verb/chainable sets.
func TestHTTPrbVerbUnknown(t *testing.T) {
	c := httprb.NewClient()
	resp, err := httprbVerb(c, "bogus", "/x", nil)
	if resp != nil || err != nil {
		t.Errorf("httprbVerb bogus got resp=%v err=%v", resp, err)
	}
	base := &HTTPrbClient{c}
	if got := httprbApplyChain(base, "bogus", nil); got != base {
		t.Errorf("httprbApplyChain fallback got=%v", got)
	}
}

// TestHTTPrbErrorResponseValue covers both arms of httprbErrorResponseValue: a
// response error yields the {status:, headers:, body:} Hash; a transport/parse
// error (nil Response) yields nil. The nil arm is also reached at runtime; the
// Response arm is Go-only because this http.rb version raises no response-carrying
// error from the verb helpers.
func TestHTTPrbErrorResponseValue(t *testing.T) {
	if got := httprbErrorResponseValue(&httprb.Error{Kind: httprb.KindError}); got != object.NilV {
		t.Errorf("nil-response arm got=%v", got)
	}
	resp := httprb.NewResponse(404, httprb.NewHeaders(httprb.KV{Key: "Content-Type", Val: "application/json"}), "boom", "1.1", "http://x/y")
	got := httprbErrorResponseValue(&httprb.Error{Kind: httprb.KindResponseError, Response: resp})
	h, ok := got.(*object.Hash)
	if !ok {
		t.Fatalf("response arm got %T", got)
	}
	if v, _ := h.Get(object.Symbol("status")); v != object.Integer(404) {
		t.Errorf("status got=%v", v)
	}
	if v, _ := h.Get(object.Symbol("body")); v.ToS() != "boom" {
		t.Errorf("body got=%v", v)
	}
	hdrs, _ := h.Get(object.Symbol("headers"))
	hh := hdrs.(*object.Hash)
	if v, _ := hh.Get(object.NewString("Content-Type")); v.ToS() != "application/json" {
		t.Errorf("headers got=%v", hdrs)
	}
}

// TestHTTPrbBasicAuthArgs covers httprbBasicAuthArgs, including the absent-key
// defaults.
func TestHTTPrbBasicAuthArgs(t *testing.T) {
	h := object.NewHash()
	h.Set(object.Symbol("user"), object.NewString("u"))
	h.Set(object.Symbol("pass"), object.NewString("p"))
	if u, p := httprbBasicAuthArgs(h); u != "u" || p != "p" {
		t.Errorf("got u=%q p=%q", u, p)
	}
	if u, p := httprbBasicAuthArgs(object.NewHash()); u != "" || p != "" {
		t.Errorf("empty got u=%q p=%q", u, p)
	}
}
