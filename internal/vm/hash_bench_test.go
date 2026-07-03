package vm_test

import "testing"

// BenchmarkWordcount reproduces the profiling workload (bench/README.md's
// wordcount row): split a corpus into words and count occurrences in a Hash. The
// corpus is 450k words drawn from 9 distinct keys, so the run is dominated by
// String-key Hash Get+Set churn — exactly the path the strVals fast path targets.
func BenchmarkWordcount(b *testing.B) {
	// 9 distinct words repeated to ~450k tokens (50000 * 9), joined by spaces once
	// at Ruby level so the split cost is realistic but the hot loop is the counter.
	src := `words = (["the","quick","brown","fox","jumps","over","lazy","dog","again"] * 50000)
counts = Hash.new(0)
words.each { |w| counts[w] += 1 }
counts.values.sum`
	benchProgram(b, src)
}

// BenchmarkHashStringChurn isolates the String-key Get/Set hot path: it repeatedly
// reads and overwrites the same small set of String keys (the wordcount inner
// loop shape), so it measures the per-lookup []byte->string copy the fast path
// removes without the surrounding split/each machinery.
func BenchmarkHashStringChurn(b *testing.B) {
	src := `h = {}
keys = ["alpha","beta","gamma","delta","epsilon","zeta","eta","theta"]
i = 0
while i < 40000
  keys.each { |k| h[k] = (h[k] || 0) + 1 }
  i += 1
end
h.size`
	benchProgram(b, src)
}
