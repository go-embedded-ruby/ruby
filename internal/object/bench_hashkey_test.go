package object

import (
	"strings"
	"testing"
)

// BenchmarkHashIntKeyGetSet exercises the immediate-key hot path with Integer
// keys: hashKey must not re-box the incoming key on either Set or Get.
func BenchmarkHashIntKeyGetSet(b *testing.B) {
	h := NewHash()
	keys := make([]Value, 64)
	for i := range keys {
		keys[i] = IntValue(int64(Integer(int64(i))))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := keys[i&63]
		h.Set(k, IntValue(int64(Integer(int64(i)))))
		_, _ = h.Get(k)
	}
}

// BenchmarkSymRawBox boxes a symbol name the old way (a plain Symbol->Value
// conversion), allocating via convTstring every time. BenchmarkSymValInterned is
// the same workload routed through the intern table: allocation-free after the
// first call. Together they measure Lever B on the method-name / keyword
// dispatch pattern (box the same symbol name per operation).
// symBenchName is a runtime-materialised name (not a compile-time constant, so
// the boxes are not statically hoisted): each element is built by strings.Repeat
// so Symbol(name) genuinely runs convTstring in BenchmarkSymRawBox.
var symBenchName = strings.Repeat("dispatch", 1)

func BenchmarkSymRawBox(b *testing.B) {
	name := symBenchName
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = SymVal(string(Symbol(name)))
	}
}

func BenchmarkSymValInterned(b *testing.B) {
	name := symBenchName
	SymVal(name) // prime
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = SymVal(name)
	}
}

// BenchmarkHashSymKeyGetSet is the Symbol-keyed twin of the above.
func BenchmarkHashSymKeyGetSet(b *testing.B) {
	h := NewHash()
	keys := make([]Value, 64)
	names := []string{"alpha", "beta", "gamma", "delta"}
	for i := range keys {
		keys[i] = SymVal(string(Symbol(names[i%len(names)] + string(rune('a'+i%26)))))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := keys[i&63]
		h.Set(k, IntValue(int64(Integer(int64(i)))))
		_, _ = h.Get(k)
	}
}
