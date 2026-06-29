# bigdecimal module benchmark: a sequence of arbitrary-precision div/mul/round.
# rbgo binds this to go-ruby-bigdecimal (pure Go). Deterministic: prints a digest
# of the final value's string form.
require "bigdecimal"

N = (ENV["N"] || "160000").to_i

seven = BigDecimal("7")
three = BigDecimal("3")

acc = 0
N.times do |i|
  x = BigDecimal(i + 1)
  x = (x / seven).round(25)
  x = (x * three).round(25)
  x = (x / BigDecimal("1.1")).round(25)
  acc += x.to_s.length
end
puts acc
