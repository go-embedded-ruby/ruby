// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// packBERArg coerces a pack 'w' element to an arbitrary-precision integer: an
// Integer or Bignum directly, a Float truncated toward zero, anything else via
// #to_int (TypeError when it does not respond). The value may be a Bignum, since
// BER compression encodes integers of any size.
func (vm *VM) packBERArg(v object.Value) *big.Int {
	if z, ok := object.BigOf(v); ok {
		return z
	}
	if f, ok := v.(object.Float); ok {
		z, _ := big.NewFloat(float64(f)).Int(nil)
		return z
	}
	if vm.respondsToDynamic(v, "to_int") {
		return vm.packBERArg(vm.send(v, "to_int", nil, nil))
	}
	raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(v))
	return nil
}

// packBER appends the BER-compressed encoding of a non-negative integer: its
// base-128 digits, most significant first, with the high bit set on every byte
// but the last. Zero encodes as a single 0x00 byte.
func packBER(out []byte, z *big.Int) []byte {
	if z.Sign() == 0 {
		return append(out, 0)
	}
	n := new(big.Int).Set(z)
	mask := big.NewInt(0x7f)
	tmp := new(big.Int)
	var groups []byte
	for n.Sign() > 0 {
		groups = append(groups, byte(tmp.And(n, mask).Int64()))
		n.Rsh(n, 7)
	}
	for i := len(groups) - 1; i >= 0; i-- {
		b := groups[i]
		if i != 0 {
			b |= 0x80
		}
		out = append(out, b)
	}
	return out
}

// berDecodeAt decodes one BER-compressed integer from data starting at pos,
// reading base-128 groups (7 bits each, most significant first) until a byte
// with the high bit clear, and returns the value and the index just past it.
func berDecodeAt(data []byte, pos int) (*big.Int, int) {
	z := new(big.Int)
	low := new(big.Int)
	for pos < len(data) {
		b := data[pos]
		pos++
		z.Lsh(z, 7)
		z.Or(z, low.SetInt64(int64(b&0x7f)))
		if b&0x80 == 0 {
			break
		}
	}
	return z, pos
}
