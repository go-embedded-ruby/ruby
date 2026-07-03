package vm

import (
	cryptorand "crypto/rand"

	"github.com/go-embedded-ruby/ruby/internal/object"
	securerandom "github.com/go-ruby-securerandom/securerandom"
)

// registerSecureRandom installs the SecureRandom module (require "securerandom"),
// a thin binding over github.com/go-ruby-securerandom/securerandom. That library
// supplies the MRI 4.0.5-compatible formatting (SIMD hex/base64, UUID layout,
// the alphanumeric/random_number distributions); rbgo supplies the entropy via
// the crypto/rand seam below, so the values are cryptographically random and
// callers/tests assert on format and range, not on exact bytes.
func (vm *VM) registerSecureRandom() {
	mod := newClass("SecureRandom", nil)
	mod.isModule = true
	vm.consts["SecureRandom"] = object.Wrap(mod)
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// One generator bound to rbgo's crypto/rand seam (secureRandRead). Routing
	// through the seam keeps tmpdir/openssl and the deterministic tests in sync:
	// swapping secureRandRead reseeds this generator too.
	gen := securerandom.New(seamSource{})

	def("random_bytes", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// Random bytes are binary, not UTF-8: tag ASCII-8BIT so length == bytesize.
		return object.Wrap(object.NewStringBytesEnc(gen.RandomBytes(countArg(args, 16)), "ASCII-8BIT"))
	})
	def("hex", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(gen.Hex(countArg(args, 16))))
	})
	def("base64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(gen.Base64(countArg(args, 16))))
	})
	def("urlsafe_base64", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// MRI defaults to no padding; an explicit truthy second argument keeps it.
		padding := len(args) > 1 && args[1].Truthy()
		return object.Wrap(object.NewString(gen.UrlsafeBase64(countArg(args, 16), padding)))
	})
	def("alphanumeric", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(gen.Alphanumeric(countArg(args, 16))))
	})
	def("uuid", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(gen.Uuid()))
	})
	def("uuid_v7", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(gen.UuidV7()))
	})
	def("random_number", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// The Integer-vs-Float dispatch lives here: a positive Ruby Integer -> an
		// Integer in [0, n) (RandomInt); a positive Float -> a Float in [0, n)
		// (RandomFloat scaled); no argument or a non-positive bound -> a Float in
		// [0, 1), exactly as MRI does for a non-positive bound.
		if len(args) > 0 {
			{
				__sw152 := args[0]
				switch {
				case object.IsInt(__sw152):
					n := object.AsInteger(__sw152)
					_ = n
					if n > 0 {
						return object.IntValue(gen.RandomInt(int64(n)))
					}
				case object.IsFloat(__sw152):
					n := object.AsFloatV(__sw152)
					_ = n
					if n > 0 {
						return object.FloatValue(float64(object.Float(float64(n) * gen.RandomFloat())))
					}
				}
			}
		}
		return object.FloatValue(float64(object.Float(gen.RandomFloat())))
	})
}

// seamSource adapts rbgo's secureRandRead seam to securerandom.RandSource so the
// library draws its entropy through the same crypto/rand seam the rest of the VM
// uses (tmpdir token generation, OpenSSL random bytes), and so the deterministic
// tests that swap secureRandRead reach the library too.
type seamSource struct{}

func (seamSource) Read(p []byte) (int, error) { return secureRandRead(p) }

// countArg returns the first integer argument, or def when it is absent or nil.
func countArg(args []object.Value, def int) int {
	if len(args) == 0 {
		return def
	}
	if _, isNil := object.AsNilOK(args[0]); isNil {
		return def
	}
	return int(intArg(args[0]))
}

// secureRandRead is the crypto/rand.Read entry point behind a seam so tests can
// exercise the (otherwise unreachable) failure path, and so the shared consumers
// (tmpdir, openssl) and the SecureRandom binding all draw from one swappable
// source.
var secureRandRead = cryptorand.Read

// secureBytes returns n cryptographically random bytes (crypto/rand never fails
// on the platforms we target; a failure is a broken environment, so it panics).
// It is the shared low-level entropy helper used by tmpdir and openssl.
func secureBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := secureRandRead(b); err != nil {
		panic(err)
	}
	return b
}
