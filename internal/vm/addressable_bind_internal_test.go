// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	addressable "github.com/go-ruby-addressable/addressable"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestAddressableWrapperMethods covers the ToS / Truthy arms of the URI and
// Template wrappers (the Ruby surface routes through explicit to_s methods, so
// the value-protocol arms need a direct call).
func TestAddressableWrapperMethods(t *testing.T) {
	u := &AddressableURI{u: addressable.Parse("http://example.com/")}
	if u.ToS() == "" || !u.Truthy() || u.Inspect() == "" {
		t.Errorf("URI methods: %q %v %q", u.ToS(), u.Truthy(), u.Inspect())
	}
	tw := &AddressableTemplate{t: addressable.NewTemplate("http://example.com/{n}")}
	if tw.ToS() == "" || !tw.Truthy() || tw.Inspect() == "" {
		t.Errorf("Template methods: %q %v %q", tw.ToS(), tw.Truthy(), tw.Inspect())
	}
}

// TestAnyToRubyDefault covers anyToRuby's default arm — a value that is neither a
// string nor a []string (Template#extract never produces one, so the arm is
// exercised directly).
func TestAnyToRubyDefault(t *testing.T) {
	if v := anyToRuby(42); v != object.NilV {
		t.Errorf("anyToRuby(int) -> %v, want nil", v)
	}
}
