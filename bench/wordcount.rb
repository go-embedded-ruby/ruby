# Mixed: string split, hash counting, sum.
text = (["the quick brown fox jumps over the lazy dog"] * 50_000).join(" ")
counts = {}
text.split(" ").each { |w| counts[w] = (counts[w] || 0) + 1 }
puts counts.values.sum
