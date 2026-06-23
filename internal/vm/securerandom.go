package vm

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	binpkg "encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerSecureRandom installs the SecureRandom module (require "securerandom"),
// a binding of Go's crypto/rand with MRI-compatible method shapes. Values are
// cryptographically random, so callers/tests assert on format and range, not on
// exact bytes.
func (vm *VM) registerSecureRandom() {
	mod := newClass("SecureRandom", nil)
	mod.isModule = true
	vm.consts["SecureRandom"] = mod
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	def("random_bytes", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(string(secureBytes(countArg(args, 16))))
	})
	def("hex", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(hex.EncodeToString(secureBytes(countArg(args, 16))))
	})
	def("base64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(base64.StdEncoding.EncodeToString(secureBytes(countArg(args, 16))))
	})
	def("urlsafe_base64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// MRI defaults to no padding; an explicit truthy second argument keeps it.
		enc := base64.RawURLEncoding
		if len(args) > 1 && args[1].Truthy() {
			enc = base64.URLEncoding
		}
		return object.NewString(enc.EncodeToString(secureBytes(countArg(args, 16))))
	})
	def("alphanumeric", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
		n := countArg(args, 16)
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteByte(alphabet[secureIndex(len(alphabet))])
		}
		return object.NewString(b.String())
	})
	def("uuid", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		b := secureBytes(16)
		b[6] = (b[6] & 0x0f) | 0x40 // version 4
		b[8] = (b[8] & 0x3f) | 0x80 // variant 10
		return object.NewString(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
	})
	def("random_number", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// No argument or 0 -> a Float in [0, 1); a positive Integer -> an Integer in
		// [0, n); a positive Float -> a Float in [0, n). Anything else falls back to
		// the [0, 1) Float, as MRI does for a non-positive bound.
		if len(args) > 0 {
			switch n := args[0].(type) {
			case object.Integer:
				if n > 0 {
					return object.Integer(secureInt(int64(n)))
				}
			case object.Float:
				if n > 0 {
					return object.Float(float64(n) * secureFloat())
				}
			}
		}
		return object.Float(secureFloat())
	})
}

// countArg returns the first integer argument, or def when it is absent or nil.
func countArg(args []object.Value, def int) int {
	if len(args) == 0 {
		return def
	}
	if _, isNil := args[0].(object.Nil); isNil {
		return def
	}
	return int(intArg(args[0]))
}

// secureRandRead / secureRandInt are the crypto/rand entry points behind a seam
// so tests can exercise the (otherwise unreachable) failure paths.
var (
	secureRandRead = cryptorand.Read
	secureRandInt  = cryptorand.Int
)

// secureBytes returns n cryptographically random bytes (crypto/rand never fails
// on the platforms we target; a failure is a broken environment, so it panics).
func secureBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := secureRandRead(b); err != nil {
		panic(err)
	}
	return b
}

// secureFloat returns a random float64 in [0, 1).
func secureFloat() float64 {
	return float64(binpkg.BigEndian.Uint64(secureBytes(8))>>11) / (1 << 53)
}

// secureInt returns a random int64 in [0, n) for n > 0.
func secureInt(n int64) int64 {
	v, err := secureRandInt(cryptorand.Reader, big.NewInt(n))
	if err != nil {
		panic(err)
	}
	return v.Int64()
}

// secureIndex returns a random index in [0, n) for n > 0.
func secureIndex(n int) int { return int(secureInt(int64(n))) }
