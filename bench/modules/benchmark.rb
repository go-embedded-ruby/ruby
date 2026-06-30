# benchmark module benchmark (meta): drive Benchmark.measure over a fixed CPU
# kernel many times and accumulate the integer results of the measured work, so
# the output is deterministic (we checksum the WORK, not the timing). rbgo binds
# this to go-ruby-benchmark. This exercises Benchmark's Tms bookkeeping +
# realtime/measure machinery around each block. Deterministic checksum printed.
require "benchmark"

N = (ENV["N"] || "20000").to_i

acc = 0
N.times do |n|
  tms = Benchmark.measure do
    s = 0
    100.times { |i| s += (i * n) % 97 }
    acc += s
  end
  # touch the Tms public API deterministically (no wall-clock value used)
  acc += 1 if tms.is_a?(Benchmark::Tms)
end
puts acc
