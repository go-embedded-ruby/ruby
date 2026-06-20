package vm

// Unboxed-integer primitives for AOT level-3 method kernels (see internal/aot).
// A kernel runs the whole method on int64; when an operation would leave the
// int64 domain — signed overflow (Ruby promotes to Bignum) or division by zero
// (Ruby raises) — the primitive panics aotDeopt, which the kernel's boxed
// wrapper recovers, re-running the call through the sound level-1 body. The
// overflow conditions mirror intOp exactly, so a kernel never disagrees with the
// interpreter: it either returns the identical int64 or deopts.

// aotDeopt is the sentinel a kernel panics to abandon the unboxed fast path.
type aotDeopt struct{}

func aotAdd(a, b int64) int64 {
	if c := a + b; (c >= a) == (b >= 0) {
		return c
	}
	panic(aotDeopt{})
}

func aotSub(a, b int64) int64 {
	if c := a - b; (c <= a) == (b >= 0) {
		return c
	}
	panic(aotDeopt{})
}

func aotMul(a, b int64) int64 {
	if c := a * b; a == 0 || (c/a == b && !(a == -1 && b == minInt64)) {
		return c
	}
	panic(aotDeopt{})
}

func aotDiv(a, b int64) int64 {
	if b == 0 {
		panic(aotDeopt{}) // deopt: level-1 raises ZeroDivisionError identically
	}
	return floorDiv(a, b)
}

func aotMod(a, b int64) int64 {
	if b == 0 {
		panic(aotDeopt{})
	}
	return floorMod(a, b)
}

func aotNeg(a int64) int64 {
	if a == minInt64 { // -minInt64 overflows int64 → Bignum
		panic(aotDeopt{})
	}
	return -a
}
