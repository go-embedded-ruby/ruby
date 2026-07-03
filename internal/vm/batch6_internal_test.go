// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	sequel "github.com/go-ruby-sequel/sequel"

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

// TestSequelRubyValueMapped covers every sequelRubyValue branch (the executor
// value model) including the terminal default for an unmapped Go type.
func TestSequelRubyValueMapped(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "nil"},
		{true, "true"},
		{int64(5), "5"},
		{int(6), "6"},
		{3.5, "3.5"},
		{"txt", `"txt"`},
		{[]byte{0x41, 0x42}, `"AB"`},
		{int32(7), "nil"}, // unmapped -> nil
	}
	for _, c := range cases {
		if got := sequelRubyValue(c.in).Inspect(); got != c.want {
			t.Errorf("sequelRubyValue(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSequelValueDefault covers sequelValue's to_s fallback for a Ruby value type
// outside the mapped set (an arbitrary object stringifies).
func TestSequelValueDefault(t *testing.T) {
	// A Range is not in sequelValue's mapped set, so it falls through to ToS.
	r := &object.Range{Lo: object.IntValue(int64(object.Integer(1))), Hi: object.IntValue(int64(object.Integer(3)))}
	if got := sequelValue(object.Wrap(r)); got != r.ToS() {
		t.Errorf("sequelValue(Range) = %v, want its ToS %q", got, r.ToS())
	}
}

// TestSequelNameDefault covers sequelName's ToS fallback for a non-Symbol,
// non-String value.
func TestSequelNameDefault(t *testing.T) {
	if got := sequelName(object.IntValue(int64(object.Integer(42)))); got != "42" {
		t.Errorf("sequelName(42) = %q, want 42", got)
	}
}

// TestSequelValueArray covers sequelValue's Array branch (a top-level Array value
// -> an IN-list []sequel.Value), which the Hash-condition path does not reach.
func TestSequelValueArray(t *testing.T) {
	arr := &object.Array{Elems: []object.Value{object.IntValue(int64(object.Integer(1))), object.IntValue(int64(object.Integer(2)))}}
	v := sequelValue(object.Wrap(arr))
	vals, ok := v.([]sequel.Value)
	if !ok || len(vals) != 2 {
		t.Fatalf("sequelValue(Array) = %#v, want a 2-element []sequel.Value", v)
	}
}

// TestSequelValueHash covers sequelValue's Hash branch (a Hash value maps to an
// ordered AND-of-equalities condition), which the where() path reaches through
// sequelCond rather than sequelValue.
func TestSequelValueHash(t *testing.T) {
	h := object.NewHash()
	h.Set(object.SymVal(string(object.Symbol("a"))), object.IntValue(int64(object.Integer(1))))
	if _, ok := sequelValue(object.Wrap(h)).(sequel.Expr); !ok {
		t.Errorf("sequelValue(Hash) = %T, want a sequel.Expr", sequelValue(object.Wrap(h)))
	}
}

// TestSequelIndexColsSingle covers sequelIndexCols with a single (non-Array)
// column reference.
func TestSequelIndexColsSingle(t *testing.T) {
	cols := sequelIndexCols(object.SymVal(string(object.Symbol("name"))))
	if len(cols) != 1 || cols[0] != "name" {
		t.Errorf("sequelIndexCols(:name) = %v, want [name]", cols)
	}
}

// TestSequelCondArity covers sequelCond's empty-args guard.
func TestSequelCondArity(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("sequelCond(nil) did not raise")
		}
	}()
	sequelCond(nil)
}
