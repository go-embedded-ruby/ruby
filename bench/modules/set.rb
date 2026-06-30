# set module benchmark: build sets and run union/intersection/difference/subset
# algebra over N-element sets, repeated. rbgo binds this to go-ruby-set.
# Deterministic: prints an accumulated size checksum.
require "set"

N = (ENV["N"] || "4000").to_i

a = Set.new((0...500).to_a)
b = Set.new((250...750).to_a)

acc = 0
N.times do
  acc += (a | b).size
  acc += (a & b).size
  acc += (a - b).size
  acc += (a ^ b).size
  acc += 1 if a.subset?(a | b)
end
puts acc
