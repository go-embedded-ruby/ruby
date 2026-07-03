package object

import "testing"

// BenchmarkHashIntKeyGetSet exercises the immediate-key hot path with Integer
// keys: hashKey must not re-box the incoming key on either Set or Get.
func BenchmarkHashIntKeyGetSet(b *testing.B) {
	h := NewHash()
	keys := make([]Value, 64)
	for i := range keys {
		keys[i] = Integer(int64(i))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := keys[i&63]
		h.Set(k, Integer(int64(i)))
		_, _ = h.Get(k)
	}
}

// BenchmarkHashSymKeyGetSet is the Symbol-keyed twin of the above.
func BenchmarkHashSymKeyGetSet(b *testing.B) {
	h := NewHash()
	keys := make([]Value, 64)
	names := []string{"alpha", "beta", "gamma", "delta"}
	for i := range keys {
		keys[i] = Symbol(names[i%len(names)] + string(rune('a'+i%26)))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := keys[i&63]
		h.Set(k, Integer(int64(i)))
		_, _ = h.Get(k)
	}
}
