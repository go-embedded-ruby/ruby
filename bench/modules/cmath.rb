# cmath module benchmark: complex transcendental functions (sqrt/exp/log/sin)
# over a sweep of inputs, repeated. rbgo binds this to go-ruby-cmath.
# Deterministic: prints an integer checksum of accumulated magnitudes.
require "cmath"

N = (ENV["N"] || "30000").to_i

acc = 0
N.times do |i|
  z = CMath.sqrt(Complex(-4, 3))
  z += CMath.exp(Complex(0.5, 1.0))
  z += CMath.log(Complex(2, -1))
  z += CMath.sin(Complex(1, 1))
  acc += (z.abs * 1000).to_i
end
puts acc
