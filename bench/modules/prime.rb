# prime module benchmark: enumerate primes and factor integers, repeated.
# rbgo binds this to go-ruby-prime. Deterministic: prints a factor-sum checksum.
require "prime"

N = (ENV["N"] || "300").to_i

acc = 0
N.times do
  # sum of the first 100 primes
  Prime.each(541) { |p| acc += p }
  # prime factorisation of a fixed composite
  Prime.prime_division(2 * 3 * 3 * 7 * 11 * 13 * 17).each { |f, e| acc += f * e }
end
puts acc
