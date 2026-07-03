package vm

import (
	crand "crypto/rand"

	"github.com/go-embedded-ruby/ruby/internal/object"
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
	return r
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
		return object.FloatValue(float64(object.Float(r.res53())))
	}
	{
		__sw127 := args[0]
		switch {
		case object.IsInt(__sw127):
			a := object.AsInteger(__sw127)
			_ = a
			if a <= 0 { // Random#rand requires a positive integer
				raise("ArgumentError", "invalid argument - %d", int64(a))
			}
			return object.IntValue(int64(r.limitedRand(uint64(a) - 1)))
		case object.IsFloat(__sw127):
			a := object.AsFloatV(__sw127)
			_ = a
			if a < 0 {
				raise("ArgumentError", "invalid argument - %s", a.Inspect())
			}
			if a == 0 {
				return object.FloatValue(float64(object.Float(r.res53())))
			}
			return object.FloatValue(float64(object.Float(r.res53() * float64(a))))
		case object.IsKind[*object.Range](__sw127):
			a := object.Kind[*object.Range](__sw127)
			_ = a
			return vm.randRange(r, a)
		}
	}
	raise("ArgumentError", "invalid argument - %s", args[0].Inspect())
	return object.NilVal()
}

// kernelRandValue implements Kernel#rand: a numeric argument is truncated to an
// integer and its magnitude bounds an integer result ([0, |n|)); zero or no
// argument gives a float in [0, 1); a range is honoured as for Random#rand.
func (vm *VM) kernelRandValue(r *RandomObj, args []object.Value) object.Value {
	if len(args) == 0 {
		return object.FloatValue(float64(object.Float(r.res53())))
	}
	var n int64
	{
		__sw128 := args[0]
		switch {
		case object.IsInt(__sw128):
			a := object.AsInteger(__sw128)
			_ = a
			n = int64(a)
		case object.IsFloat(__sw128):
			a := object.AsFloatV(__sw128)
			_ = a
			n = int64(a)
		case object.IsKind[*object.Range](__sw128):
			a := object.Kind[*object.Range](__sw128)
			_ = a
			return vm.randRange(r, a)
		default:
			a := __sw128
			_ = a
			raise("ArgumentError", "invalid argument - %s", args[0].Inspect())
		}
	}
	if n < 0 {
		n = -n
	}
	if n == 0 {
		return object.FloatValue(float64(object.Float(r.res53())))
	}
	return object.IntValue(int64(r.limitedRand(uint64(n) - 1)))
}

// randRange implements Random#rand(a..b) for integer or float ranges.
func (vm *VM) randRange(r *RandomObj, rg *object.Range) object.Value {
	lo, lok := object.AsIntegerOK(rg.Lo)
	hi, hok := object.AsIntegerOK(rg.Hi)
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
	return object.FloatValue(float64(object.Float(flo + r.res53()*(fhi-flo))))
}

func (vm *VM) registerRandom() {
	cRandom := newClass("Random", vm.cObject)
	vm.consts["Random"] = object.Wrap(cRandom)
	vm.defaultRandom = newRandom(randomSeed())

	cRandom.smethods["new"] = &Method{name: "new", owner: cRandom, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		seed := randomSeed()
		if len(args) > 0 {
			seed = intArg(args[0])
		}
		return object.Wrap(newRandom(seed))
	}}

	cRandom.define("rand", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.randValue(object.Kind[*RandomObj](self), args)
	})
	cRandom.define("seed", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(object.Kind[*RandomObj](self).seed)
	})
	cRandom.define("bytes", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := object.Kind[*RandomObj](self)
		n := int(intArg(args[0]))
		b := make([]byte, n)
		for i := 0; i < n; i += 4 {
			w := r.genrandInt32()
			for k := 0; k < 4 && i+k < n; k++ {
				b[i+k] = byte(w >> (8 * uint(k)))
			}
		}
		return object.Wrap(object.NewStringBytes(b))
	})

	// Kernel#rand / #srand operate on a process-wide default generator.
	vm.cObject.define("rand", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.kernelRandValue(vm.defaultRandom, args)
	})
	vm.cObject.define("srand", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		prev := vm.defaultRandom.seed
		seed := randomSeed()
		if len(args) > 0 {
			seed = intArg(args[0])
		}
		vm.defaultRandom = newRandom(seed)
		return object.IntValue(prev)
	})
}
