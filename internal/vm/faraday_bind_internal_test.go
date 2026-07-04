// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"testing"

	faraday "github.com/go-ruby-faraday/faraday"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestFaradayValueProtocol covers the ToS / Inspect / Truthy arms of every
// Faraday wrapper — the object-protocol methods a Ruby-level print/inspect would
// reach, exercised directly for completeness.
func TestFaradayValueProtocol(t *testing.T) {
	vals := []struct {
		v    object.Value
		want string
	}{
		{&FaradayConnection{}, "#<Faraday::Connection>"},
		{&FaradayRequest{}, "#<Faraday::Request>"},
		{&FaradayResponse{}, "#<Faraday::Response>"},
		{&FaradayParams{}, "#<Faraday::Utils::ParamsHash>"},
		{&FaradayHeaders{}, "#<Faraday::Utils::Headers>"},
	}
	for _, c := range vals {
		if c.v.ToS() != c.want || c.v.Inspect() != c.want || !c.v.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c.v, c.v.ToS(), c.v.Inspect(), c.v.Truthy())
		}
	}
}

// TestRubyToGoValueGoOnly covers the rubyToGoValue arms a Ruby JSON body rarely
// reaches: a Bignum, a Symbol value, a nested nil, and the terminal fallback (an
// unmapped Ruby value degrades to its #to_s).
func TestRubyToGoValueGoOnly(t *testing.T) {
	if got := rubyToGoValue(&object.Bignum{I: big.NewInt(42)}); got.(*big.Int).Int64() != 42 {
		t.Errorf("bignum arm got=%v", got)
	}
	if got := rubyToGoValue(object.Symbol("sym")); got != "sym" {
		t.Errorf("symbol arm got=%v", got)
	}
	if got := rubyToGoValue(object.NilV); got != nil {
		t.Errorf("nil arm got=%v", got)
	}
	// An unmapped Ruby value (a Faraday wrapper) falls through to #to_s.
	if got := rubyToGoValue(&FaradayResponse{}); got != "#<Faraday::Response>" {
		t.Errorf("fallback arm got=%v", got)
	}
	// The scalar/container arms, for completeness of the mapper.
	if got := rubyToGoValue(object.Integer(7)); got != int64(7) {
		t.Errorf("int arm got=%v", got)
	}
	if got := rubyToGoValue(object.Float(2.5)); got != 2.5 {
		t.Errorf("float arm got=%v", got)
	}
	if got := rubyToGoValue(object.Bool(true)); got != true {
		t.Errorf("bool arm got=%v", got)
	}
	if got := rubyToGoValue(object.NewString("s")); got != "s" {
		t.Errorf("string arm got=%v", got)
	}
	arr := rubyToGoValue(object.NewArrayFromSlice([]object.Value{object.Integer(1)}))
	if s, ok := arr.([]any); !ok || len(s) != 1 || s[0] != int64(1) {
		t.Errorf("array arm got=%v", arr)
	}
	h := object.NewHash()
	h.Set(object.NewString("k"), object.Integer(9))
	m := rubyToGoValue(h)
	if mm, ok := m.(map[string]any); !ok || mm["k"] != int64(9) {
		t.Errorf("hash arm got=%v", m)
	}
}

// TestGoValueToRubyGoOnly covers the goValueToRuby terminal fallback (a Go type
// the library never yields — e.g. an int — degrades to its printed form) and the
// non-integral float arm.
func TestGoValueToRubyGoOnly(t *testing.T) {
	if got := goValueToRuby(int(7)); got.ToS() != "7" {
		t.Errorf("fallback arm got=%v", got)
	}
	if got := goValueToRuby(2.5); got != object.Float(2.5) {
		t.Errorf("float arm got=%v", got)
	}
	if got := goValueToRuby(float64(4)); got != object.Integer(4) {
		t.Errorf("integral-float arm got=%v", got)
	}
}

// TestFaradayVerbUnknown covers faradayVerb's default arm, unreachable from Ruby
// because the verb name always comes from rbgo's fixed sets.
func TestFaradayVerbUnknown(t *testing.T) {
	c := faraday.New(faraday.Options{})
	resp, err := faradayVerb(c, "bogus", "/x", nil, nil)
	if resp != nil || err != nil {
		t.Errorf("faradayVerb bogus got resp=%v err=%v", resp, err)
	}
}

// TestFaradayNameArm covers faradayName's String arm (the Symbol arm is exercised
// from Ruby by the middleware DSL).
func TestFaradayNameArm(t *testing.T) {
	if got := faradayName(object.NewString("json")); got != "json" {
		t.Errorf("string arm got=%q", got)
	}
	if got := faradayName(object.Symbol("json")); got != "json" {
		t.Errorf("symbol arm got=%q", got)
	}
}
