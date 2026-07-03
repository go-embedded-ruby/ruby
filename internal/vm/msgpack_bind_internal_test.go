// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"math/big"
	"testing"
	stdtime "time"

	msgpack "github.com/go-ruby-msgpack/msgpack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestMsgpackToBridge covers the Go-only arms of toMsgpack the Ruby round-trip
// tests do not reach directly: a plain Go nil, the object.Nil singleton, a
// Bignum, and the default (unmapped) fall-through.
func TestMsgpackToBridge(t *testing.T) {
	if toMsgpack(object.NilVal()) != nil {
		t.Error("go-nil should map to nil")
	}
	if toMsgpack(object.NilVal()) != nil {
		t.Error("object.NilV should map to nil")
	}
	big30 := object.NormInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil))
	if v, ok := toMsgpack(big30).(*big.Int); !ok || v.Sign() <= 0 {
		t.Errorf("bignum -> %T", toMsgpack(big30))
	}
	// A Symbol maps to msgpack.Symbol.
	if v, ok := toMsgpack(object.SymVal(string(object.Symbol("s")))).(msgpack.Symbol); !ok || string(v) != "s" {
		t.Errorf("symbol -> %T", toMsgpack(object.SymVal(string(object.Symbol("s")))))
	}
	// An unmapped value (a Proc) is returned as-is for the library to reject.
	if _, ok := toMsgpack(object.Wrap(&Proc{})).(*Proc); !ok {
		t.Errorf("unmapped -> %T", toMsgpack(object.Wrap(&Proc{})))
	}
}

// TestMsgpackFromBridge covers the Go-only arms of fromMsgpack: an int64/uint64
// (both the in-range and the Bignum-promoting halves), a *big.Int, a Bin, a
// Time, and the *Ext raise.
func TestMsgpackFromBridge(t *testing.T) {
	vm := New(nil)
	if v := fromMsgpack(vm, int64(5)); v != object.IntValue(int64(object.Integer(5))) {
		t.Errorf("int64 -> %v", v)
	}
	// A uint64 within int64 range stays an Integer.
	if v := fromMsgpack(vm, uint64(7)); v != object.IntValue(int64(object.Integer(7))) {
		t.Errorf("small uint64 -> %v", v)
	}
	// A uint64 above int64's max promotes to a Bignum-valued Integer.
	big := fromMsgpack(vm, uint64(math.MaxUint64))
	if bn, ok := object.KindOK[*object.Bignum](big); !ok || bn.I.Sign() <= 0 {
		t.Errorf("large uint64 -> %T", big)
	}
	// A *big.Int maps through NormInt.
	if v := fromMsgpack(vm, big1e30()); object.IsNil(v) {
		t.Error("big.Int -> nil")
	}
	// A Bin maps to an ASCII-8BIT String.
	s, ok := object.KindOK[*object.String](fromMsgpack(vm, msgpack.Bin{0xff, 0x00}))
	if !ok || !s.IsBinary() || s.Len() != 2 {
		t.Errorf("bin -> %#v", fromMsgpack(vm, msgpack.Bin{0xff, 0x00}))
	}
	// A time.Time maps to a Ruby Time carrying the same instant.
	tm, ok := object.KindOK[*Time](fromMsgpack(vm, stdtime.Unix(100, 0)))
	if !ok || tm.t.ToUnix() != 100 {
		t.Errorf("time -> %#v", fromMsgpack(vm, stdtime.Unix(100, 0)))
	}
	// An unsupported extension raises ArgumentError.
	re := rubyErr(t, func() { fromMsgpack(vm, &msgpack.Ext{Type: 9, Payload: []byte{1}}) })
	if re.Class != "ArgumentError" {
		t.Errorf("ext raise class=%q", re.Class)
	}
}

// TestMsgpackFromDefault covers fromMsgpack's defensive default arm — a value
// the unpacker never actually produces maps to nil rather than panicking.
func TestMsgpackFromDefault(t *testing.T) {
	if v := fromMsgpack(New(nil), struct{}{}); !object.IsNil(v) {
		t.Errorf("unmodelled -> %v", v)
	}
}

// TestMsgpackBytesArgNonString covers msgpackBytesArg's to_s branch for a
// non-String argument.
func TestMsgpackBytesArgNonString(t *testing.T) {
	if got := msgpackBytesArg(object.IntValue(int64(object.Integer(1)))); string(got) != "1" {
		t.Errorf("non-string arg -> %q", got)
	}
}

func big1e30() *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil) }
