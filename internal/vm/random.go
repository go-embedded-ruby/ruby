package vm

import (
	crand "crypto/rand"

	"github.com/go-embedded-ruby/ruby/internal/object"
	securerandom "github.com/go-ruby-securerandom/securerandom"
)

// randomSeed draws a non-deterministic seed for Random.new / srand with no
// argument, matching MRI's entropy-seeded default.
func randomSeed() int64 {
	var b [8]byte
	_, _ = crand.Read(b[:]) // on the rare error path b stays zero — a valid seed
	var s uint64
	for i := 0; i < 8; i++ {
		s |= uint64(b[i]) << (8 * uint(i))
	}
	return int64(s & 0x7fffffffffffffff)
}

// RandomObj is Ruby's Random: an MT19937 generator seeded exactly as MRI does
// (init_by_array over the seed's 32-bit little-endian words), so seeded output
// matches MRI bit for bit.
type RandomObj struct {
	mt   [624]uint32
	mti  int
	seed int64
	// fmtr formats this generator's MT19937 byte stream through
	// go-ruby-securerandom (hex/base64/urlsafe_base64/uuid/uuid_v7), so a seeded
	// Random reproduces MRI's Random::Formatter output byte for byte.
	fmtr *securerandom.SecureRandom
}

func (r *RandomObj) ToS() string     { return "#<Random>" }
func (r *RandomObj) Inspect() string { return r.ToS() }
func (r *RandomObj) Truthy() bool    { return true }

func newRandom(seed int64) *RandomObj {
	r := &RandomObj{seed: seed}
	key := seedKey(seed)
	if len(key) == 1 {
		r.initGenrand(key[0]) // MRI seeds a single-word seed with init_genrand
	} else {
		r.initByArray(key)
	}
	r.fmtr = securerandom.New(randomByteSource{r})
	return r
}

// randomByteSource feeds a RandomObj's MT19937 byte stream into the
// Random::Formatter helpers. Each formatter method issues one random_bytes draw,
// so a single Read per call keeps the word alignment identical to MRI's
// fill_random_bytes and the formatted output matches MRI bit for bit.
type randomByteSource struct{ r *RandomObj }

func (s randomByteSource) Read(p []byte) (int, error) {
	s.r.fillBytes(p)
	return len(p), nil
}

// fillBytes writes len(p) random bytes, taking each MT19937 32-bit word
// little-endian and discarding the unused tail of the final word — MRI's
// Random#bytes / fill_random_bytes layout.
func (r *RandomObj) fillBytes(p []byte) {
	for i := 0; i < len(p); i += 4 {
		w := r.genrandInt32()
		for k := 0; k < 4 && i+k < len(p); k++ {
			p[i+k] = byte(w >> (8 * uint(k)))
		}
	}
}

// equalState reports whether o is a Random in exactly the same generator state
// (the full MT vector, its index, and the seed), so future output is identical —
// MRI's Random#== semantics.
func (r *RandomObj) equalState(o *RandomObj) bool {
	return r.mt == o.mt && r.mti == o.mti && r.seed == o.seed
}

// seedKey turns a seed into the 32-bit little-endian word array MRI feeds to
// init_by_array (|seed|; zero yields a single zero word).
func seedKey(seed int64) []uint32 {
	s := uint64(seed)
	if seed < 0 {
		s = uint64(-seed)
	}
	if s == 0 {
		return []uint32{0}
	}
	var key []uint32
	for s > 0 {
		key = append(key, uint32(s))
		s >>= 32
	}
	return key
}

func (r *RandomObj) initGenrand(s uint32) {
	r.mt[0] = s
	for i := 1; i < 624; i++ {
		r.mt[i] = 1812433253*(r.mt[i-1]^(r.mt[i-1]>>30)) + uint32(i)
	}
	r.mti = 624
}

func (r *RandomObj) initByArray(key []uint32) {
	r.initGenrand(19650218)
	i, j := 1, 0
	// k = max(624, len(key)); our seeds are int64, so len(key) <= 2 and 624 wins.
	k := 624
	for ; k > 0; k-- {
		r.mt[i] = (r.mt[i] ^ ((r.mt[i-1] ^ (r.mt[i-1] >> 30)) * 1664525)) + key[j] + uint32(j)
		i++
		j++
		if i >= 624 {
			r.mt[0] = r.mt[623]
			i = 1
		}
		if j >= len(key) {
			j = 0
		}
	}
	for k = 623; k > 0; k-- {
		r.mt[i] = (r.mt[i] ^ ((r.mt[i-1] ^ (r.mt[i-1] >> 30)) * 1566083941)) - uint32(i)
		i++
		if i >= 624 {
			r.mt[0] = r.mt[623]
			i = 1
		}
	}
	r.mt[0] = 0x80000000
}

func (r *RandomObj) genrandInt32() uint32 {
	if r.mti >= 624 {
		for i := 0; i < 624; i++ {
			y := (r.mt[i] & 0x80000000) | (r.mt[(i+1)%624] & 0x7fffffff)
			r.mt[i] = r.mt[(i+397)%624] ^ (y >> 1)
			if y&1 != 0 {
				r.mt[i] ^= 0x9908b0df
			}
		}
		r.mti = 0
	}
	y := r.mt[r.mti]
	r.mti++
	y ^= y >> 11
	y ^= (y << 7) & 0x9d2c5680
	y ^= (y << 15) & 0xefc60000
	y ^= y >> 18
	return y
}

// res53 is MRI's genrand_real: a 53-bit float in [0, 1).
func (r *RandomObj) res53() float64 {
	a := r.genrandInt32() >> 5
	b := r.genrandInt32() >> 6
	return (float64(a)*67108864.0 + float64(b)) / 9007199254740992.0
}

// limitedRand returns a uniform integer in [0, limit] (inclusive) using MRI's
// mask-and-reject scheme: the value is assembled high 32-bit word first, each
// word masked by the corresponding slice of the bit mask, retrying while the
// result exceeds limit. A 32-bit limit consumes one genrand_int32 per attempt.
func (r *RandomObj) limitedRand(limit uint64) uint64 {
	if limit == 0 {
		return 0
	}
	mask := makeMask64(limit)
	for {
		val := uint64(0)
		for i := 1; i >= 0; i-- {
			if m := (mask >> (uint(i) * 32)) & 0xffffffff; m != 0 {
				val |= (uint64(r.genrandInt32()) & m) << (uint(i) * 32)
			}
		}
		if val <= limit {
			return val
		}
	}
}

func makeMask64(x uint64) uint64 {
	x |= x >> 1
	x |= x >> 2
	x |= x >> 4
	x |= x >> 8
	x |= x >> 16
	x |= x >> 32
	return x
}

// randValue implements Random#rand: no/zero/float argument gives a float in
// [0, n) (n=1 by default); a positive integer gives an integer in [0, n); a
// range gives a value within it.
func (vm *VM) randValue(r *RandomObj, args []object.Value) object.Value {
	if len(args) == 0 {
		return object.Float(r.res53())
	}
	switch a := args[0].(type) {
	case object.Integer:
		if a <= 0 { // Random#rand requires a positive integer
			raise("ArgumentError", "invalid argument - %d", int64(a))
		}
		return object.IntValue(int64(r.limitedRand(uint64(a) - 1)))
	case object.Float:
		if a < 0 {
			raise("ArgumentError", "invalid argument - %s", a.Inspect())
		}
		if a == 0 {
			return object.Float(r.res53())
		}
		return object.Float(r.res53() * float64(a))
	case *object.Range:
		return vm.randRange(r, a)
	}
	raise("ArgumentError", "invalid argument - %s", args[0].Inspect())
	return object.NilV
}

// kernelRandValue implements Kernel#rand: a numeric argument is truncated to an
// integer and its magnitude bounds an integer result ([0, |n|)); zero or no
// argument gives a float in [0, 1); a range is honoured as for Random#rand.
func (vm *VM) kernelRandValue(r *RandomObj, args []object.Value) object.Value {
	if len(args) == 0 {
		return object.Float(r.res53())
	}
	var n int64
	switch a := args[0].(type) {
	case object.Integer:
		n = int64(a)
	case object.Float:
		n = int64(a) // truncates toward zero, like Float#to_i
	case *object.Range:
		return vm.randRange(r, a)
	default:
		raise("ArgumentError", "invalid argument - %s", args[0].Inspect())
	}
	if n < 0 {
		n = -n
	}
	if n == 0 {
		return object.Float(r.res53())
	}
	return object.IntValue(int64(r.limitedRand(uint64(n) - 1)))
}

// randRange implements Random#rand(a..b) for integer or float ranges.
func (vm *VM) randRange(r *RandomObj, rg *object.Range) object.Value {
	lo, lok := rg.Lo.(object.Integer)
	hi, hok := rg.Hi.(object.Integer)
	if lok && hok {
		span := int64(hi) - int64(lo)
		if !rg.Exclusive {
			span++
		}
		if span <= 0 {
			raise("ArgumentError", "invalid argument - %s", rg.Inspect())
		}
		return object.IntValue(int64(lo) + int64(r.limitedRand(uint64(span)-1)))
	}
	flo, fok1 := toFloat(rg.Lo)
	fhi, fok2 := toFloat(rg.Hi)
	if !fok1 || !fok2 || fhi < flo {
		raise("ArgumentError", "invalid argument - %s", rg.Inspect())
	}
	return object.Float(flo + r.res53()*(fhi-flo))
}

// randomNumberValue implements Random::Formatter#random_number: it mirrors
// Random#rand for a positive Integer, a positive Float (scaled res53), or a
// Range, but a non-positive / non-numeric argument (n <= 0) yields a Float in
// [0, 1) instead of raising. A non-numeric, non-Range argument is an
// ArgumentError, as in MRI.
func (vm *VM) randomNumberValue(r *RandomObj, args []object.Value) object.Value {
	if len(args) == 0 {
		return object.Float(r.res53())
	}
	switch a := args[0].(type) {
	case object.Integer:
		if a > 0 {
			return object.IntValue(int64(r.limitedRand(uint64(a) - 1)))
		}
		return object.Float(r.res53())
	case object.Float:
		if a > 0 {
			return object.Float(r.res53() * float64(a))
		}
		return object.Float(r.res53())
	case *object.Range:
		return vm.randRange(r, a)
	}
	raise("ArgumentError", "invalid argument - %s", args[0].Inspect())
	return object.NilV
}

// alnumChars is MRI's Random::Formatter::ALPHANUMERIC source: A-Z, then a-z,
// then 0-9, each as a one-character string.
var alnumChars = buildAlnum()

func buildAlnum() []string {
	out := make([]string, 0, 62)
	for c := byte('A'); c <= 'Z'; c++ {
		out = append(out, string(c))
	}
	for c := byte('a'); c <= 'z'; c++ {
		out = append(out, string(c))
	}
	for c := byte('0'); c <= '9'; c++ {
		out = append(out, string(c))
	}
	return out
}

// chooseAlnum reproduces MRI's Random::Formatter#choose over this generator's
// MT19937 stream: it batches m base-size digits per random_number(limit) draw
// (limit the largest power of size that fits in 0x100000000), emitting the
// least-significant digit first, then a final partial batch — so a seeded
// Random#alphanumeric matches MRI. A single-element source has no batch large
// enough (MRI would loop forever); we return n copies of that element, the limit
// of MRI's intent, and an empty source yields "".
func (r *RandomObj) chooseAlnum(source []string, n int) string {
	size := len(source)
	if n <= 0 || size == 0 {
		return ""
	}
	if size == 1 {
		var out []byte
		for i := 0; i < n; i++ {
			out = append(out, source[0]...)
		}
		return string(out)
	}
	limit := int64(size)
	m := 1
	for limit*int64(size) <= 0x100000000 {
		limit *= int64(size)
		m++
	}
	var out []byte
	emit := func(count int) {
		rs := int64(r.limitedRand(uint64(limit) - 1))
		for i := 0; i < count; i++ {
			out = append(out, source[rs%int64(size)]...)
			rs /= int64(size)
		}
	}
	for m <= n {
		emit(m)
		n -= m
	}
	if n > 0 {
		emit(n)
	}
	return string(out)
}

// countArgOr returns the first positional integer of args (default def), skipping
// a trailing keyword Hash and treating an explicit nil as absent — the arity of
// Random::Formatter's byte-count methods (hex/base64/random_bytes/…).
func countArgOr(args []object.Value, def int) int {
	if len(args) > 0 {
		if _, isHash := args[len(args)-1].(*object.Hash); isHash {
			args = args[:len(args)-1]
		}
	}
	return countArg(args, def)
}

// charsKwarg returns the alphanumeric source: the chars: keyword Array when
// present (its elements as strings), else the default A-Za-z0-9 alphabet. A
// non-Array chars: value is a TypeError, as in MRI.
func charsKwarg(args []object.Value) []string {
	if len(args) > 0 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			if v, found := h.Get(object.Symbol("chars")); found {
				arr, ok := v.(*object.Array)
				if !ok {
					raise("TypeError", "no implicit conversion of %s into Array", v.Inspect())
				}
				out := make([]string, len(arr.Elems))
				for i, e := range arr.Elems {
					out[i] = e.ToS()
				}
				return out
			}
		}
	}
	return alnumChars
}

func (vm *VM) registerRandom() {
	cRandom := newClass("Random", vm.cObject)
	vm.consts["Random"] = cRandom
	vm.defaultRandom = newRandom(randomSeed())

	cRandom.smethods["new"] = &Method{name: "new", owner: cRandom, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		seed := randomSeed()
		if len(args) > 0 {
			seed = intArg(args[0])
		}
		return newRandom(seed)
	}}

	cRandom.define("rand", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.randValue(self.(*RandomObj), args)
	})
	cRandom.define("seed", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self.(*RandomObj).seed)
	})
	cRandom.define("bytes", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := self.(*RandomObj)
		b := make([]byte, int(intArg(args[0])))
		r.fillBytes(b)
		return object.NewStringBytes(b)
	})
	cRandom.define("==", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := args[0].(*RandomObj)
		return object.Bool(ok && self.(*RandomObj).equalState(o))
	})

	// Random::Formatter (require "random/formatter"): the byte-count methods draw
	// this generator's MT19937 stream through fmtr so seeded output matches MRI.
	cRandom.define("random_bytes", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewStringBytesEnc(self.(*RandomObj).fmtr.RandomBytes(countArgOr(args, 16)), "ASCII-8BIT")
	})
	cRandom.define("hex", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*RandomObj).fmtr.Hex(countArgOr(args, 16)))
	})
	cRandom.define("base64", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*RandomObj).fmtr.Base64(countArgOr(args, 16)))
	})
	cRandom.define("urlsafe_base64", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		padding := len(args) > 1 && args[1].Truthy()
		return object.NewString(self.(*RandomObj).fmtr.UrlsafeBase64(countArgOr(args, 16), padding))
	})
	uuidV4 := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*RandomObj).fmtr.Uuid())
	}
	cRandom.define("uuid", uuidV4)
	cRandom.define("uuid_v4", uuidV4)
	cRandom.define("uuid_v7", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*RandomObj).fmtr.UuidV7())
	})
	cRandom.define("alphanumeric", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*RandomObj).chooseAlnum(charsKwarg(args), countArgOr(args, 16)))
	})
	cRandom.define("random_number", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.randomNumberValue(self.(*RandomObj), args)
	})

	// Class-side singleton methods operate on the process-wide default generator,
	// exactly as Kernel#rand/#srand do (they share vm.defaultRandom).
	cRandom.smethods["rand"] = &Method{name: "rand", owner: cRandom, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.randValue(vm.defaultRandom, args)
	}}
	cRandom.smethods["bytes"] = &Method{name: "bytes", owner: cRandom, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		b := make([]byte, int(intArg(args[0])))
		vm.defaultRandom.fillBytes(b)
		return object.NewStringBytesEnc(b, "ASCII-8BIT")
	}}
	cRandom.smethods["random_number"] = &Method{name: "random_number", owner: cRandom, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.randomNumberValue(vm.defaultRandom, args)
	}}
	cRandom.smethods["new_seed"] = &Method{name: "new_seed", owner: cRandom, native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(randomSeed())
	}}
	cRandom.smethods["srand"] = &Method{name: "srand", owner: cRandom, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.reseedDefault(args)
	}}

	// Kernel#rand / #srand operate on a process-wide default generator.
	vm.cObject.define("rand", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.kernelRandValue(vm.defaultRandom, args)
	})
	vm.cObject.define("srand", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.reseedDefault(args)
	})
}

// reseedDefault reseeds the process-wide default generator (a given seed, else a
// fresh entropy seed) and returns the previous seed, the shared behaviour of
// Kernel#srand and Random.srand.
func (vm *VM) reseedDefault(args []object.Value) object.Value {
	prev := vm.defaultRandom.seed
	seed := randomSeed()
	if len(args) > 0 {
		seed = intArg(args[0])
	}
	vm.defaultRandom = newRandom(seed)
	return object.IntValue(prev)
}
