// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	msgpack "github.com/go-ruby-msgpack/msgpack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value
// and the VM's Time shell) and the interpreter-independent value model of
// github.com/go-ruby-msgpack/msgpack. The packer and unpacker themselves live in
// that library; rbgo only translates its values to and from the library's `any`
// model around a single msgpack.Pack / msgpack.Unpack call, so the gem-faithful
// wire format the MessagePack module relies on is preserved by construction.

// msgpackPack serialises a Ruby value to MessagePack bytes by mapping it into the
// library value model and calling msgpack.Pack. A value with no MessagePack
// representation raises a Ruby ArgumentError carrying the library's message,
// matching the gem's MessagePack::PackError family.
func msgpackPack(v object.Value) []byte {
	out, err := msgpack.Pack(toMsgpack(v))
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	return out
}

// msgpackUnpack parses MessagePack bytes into a Ruby value by calling
// msgpack.Unpack and mapping the result back into the rbgo object graph. Malformed
// bytes raise a Ruby ArgumentError (the gem's MessagePack::UnpackError family).
func msgpackUnpack(vm *VM, b []byte) object.Value {
	v, err := msgpack.Unpack(b)
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	return fromMsgpack(vm, v)
}

// --- rbgo value -> library value (for Pack) --------------------------------

// toMsgpack maps a Ruby value to the go-ruby-msgpack value model. A binary
// (ASCII-8BIT) String maps to msgpack.Bin (the bin family), a UTF-8 String to a
// Go string (the str family), a Symbol to msgpack.Symbol (packed as a str by the
// gem default), a Time to a Go time.Time (the reserved ext -1), and Array / Hash
// recurse. An unmapped value is returned as-is so the library raises the pack
// error msgpackPack turns into a Ruby ArgumentError.
func toMsgpack(v object.Value) msgpack.Value {
	switch n := v.(type) {
	case nil:
		return nil
	case object.Nil:
		return nil
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int64(n)
	case *object.Bignum:
		return n.I
	case object.Float:
		return float64(n)
	case *object.String:
		if n.IsBinary() {
			return msgpack.Bin(n.Bytes())
		}
		return n.Str()
	case object.Symbol:
		return msgpack.Symbol(string(n))
	case *object.Array:
		out := make([]msgpack.Value, len(n.Elems))
		for i, el := range n.Elems {
			out[i] = toMsgpack(el)
		}
		return out
	case *object.Hash:
		m := msgpack.NewMap()
		for _, k := range n.Keys {
			val, _ := n.Get(k)
			m.Set(toMsgpack(k), toMsgpack(val))
		}
		return m
	case *Time:
		return stdtime.Unix(n.t.ToUnix(), 0).UTC()
	}
	// An unmapped value: hand it to the library, which returns the pack error
	// msgpackPack turns into a Ruby ArgumentError.
	return v
}

// --- library value -> rbgo value (for Unpack) ------------------------------

// fromMsgpack maps a value produced by msgpack.Unpack back into the rbgo object
// graph. The bin family (msgpack.Bin) becomes an ASCII-8BIT String, the str
// family a UTF-8 String, an ext -1 time.Time a Ruby Time, and *Map / []any
// recurse. Any other extension (*Ext) is not modelled by rbgo and raises.
func fromMsgpack(vm *VM, v msgpack.Value) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(n)
	case int64:
		return object.IntValue(n)
	case uint64:
		if n <= 1<<63-1 {
			return object.IntValue(int64(n))
		}
		return object.NormInt(new(big.Int).SetUint64(n))
	case *big.Int:
		return object.NormInt(n)
	case float64:
		return object.Float(n)
	case string:
		return object.NewString(n)
	case msgpack.Bin:
		return object.NewStringBytesEnc([]byte(n), "ASCII-8BIT")
	case []msgpack.Value:
		arr := &object.Array{Elems: make([]object.Value, len(n))}
		for i, el := range n {
			arr.Elems[i] = fromMsgpack(vm, el)
		}
		return arr
	case *msgpack.Map:
		h := object.NewHash()
		for _, p := range n.Pairs() {
			h.Set(fromMsgpack(vm, p.Key), fromMsgpack(vm, p.Val))
		}
		return h
	case stdtime.Time:
		return &Time{t: gotime.FromUnix(n.Unix())}
	case *msgpack.Ext:
		raise("ArgumentError", "unsupported MessagePack extension type %d", n.Type)
	}
	// The unpacker only ever produces the cases above; anything else is nil.
	return object.NilV
}
