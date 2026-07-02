// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestRedisValueUnmapped covers redisValue's terminal default: a Go value of a
// type the RESP decoder never produces maps to nil. This exercises the
// defensive final return that the wire grammar cannot reach.
func TestRedisValueUnmapped(t *testing.T) {
	vm := New(nil)
	if got := vm.redisValue(int32(7)); got != object.NilV {
		t.Errorf("redisValue(int32) = %v, want nil", got)
	}
}

// TestPGValueTime covers pgValue's time.Time and array branches and the
// unexpected-type fallback, which a normal query stream reaches only for
// time/array columns.
func TestPGValueUnmapped(t *testing.T) {
	vm := New(nil)
	// An unexpected decoder type (never produced by the OID decoders) stringifies
	// to "" via the fallback.
	if got := vm.pgValue(struct{ x int }{}); got.ToS() != "" {
		t.Errorf("pgValue(struct) = %q, want empty", got.ToS())
	}
	// A String()-carrying value renders via pgSprint.
	if got := vm.pgValue(pgStringer{"hi"}); got.ToS() != "hi" {
		t.Errorf("pgValue(stringer) = %q, want hi", got.ToS())
	}
}

// pgStringer is a String()-carrying value for the pgSprint fallback path.
type pgStringer struct{ s string }

func (p pgStringer) String() string { return p.s }

// TestSequelRubyValueUnmapped covers sequelRubyValue's terminal default: an
// executor value of an unmapped type maps to nil.
func TestSequelRubyValueUnmapped(t *testing.T) {
	if got := sequelRubyValue(int32(7)); got != object.NilV {
		t.Errorf("sequelRubyValue(int32) = %v, want nil", got)
	}
}
