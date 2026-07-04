// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestPbToRubyDefault covers pbToRuby's defensive default arm: the go-ruby-protobuf
// value model never produces a Go type outside the handled set, so the arm is
// unreachable from Ruby; this white-box call (with a plain int, which is not in
// the model) exercises it and pins its nil result.
func TestPbToRubyDefault(t *testing.T) {
	vm := New(io.Discard)
	if got := vm.pbToRuby(int(5)); !object.IsNil(got) {
		t.Fatalf("pbToRuby(int) = %#v, want nil", got)
	}
}
