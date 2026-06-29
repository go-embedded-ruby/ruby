# format module benchmark: sprintf / % with mixed conversions in a tight loop.
# rbgo binds this to the go-ruby-format pure-Go formatter. Deterministic.
N = (ENV["N"] || "400000").to_i

acc = 0
N.times do |i|
  s = sprintf("%05d|%-8s|%8.3f|%x|%o|%+d|%e",
              i, "row#{i % 97}", i * 1.25, i, i, i - (N / 2), i * 0.001)
  acc += s.length
end
puts acc
