package object

// Small integers are interned: boxing one into a Value reuses a shared,
// immutable box instead of allocating a fresh one. Integers are value types
// (Ruby fixnums are identity-equal — 1.equal?(1) — so sharing changes no
// semantics); the win is purely that the hot arithmetic path stops allocating an
// interface box per result for the common case of small numbers (loop counters,
// indices, recursion arguments, small sums).
const (
	smallIntMin = -256
	smallIntMax = 1024
)

var smallInts = func() [smallIntMax - smallIntMin + 1]Value {
	var a [smallIntMax - smallIntMin + 1]Value
	for i := range a {
		a[i] = Value{tag: TagInt, i: int64(i) + smallIntMin}
	}
	return a
}()

// IntValue boxes v as a Value, reusing an interned box when v is small so no
// allocation occurs. Use it on hot paths that produce integers; for a value
// outside the cached range it is exactly object.Integer(v).
func IntValue(v int64) Value {
	if v >= smallIntMin && v <= smallIntMax {
		return smallInts[v-smallIntMin]
	}
	return Value{tag: TagInt, i: v}
}
