# complex benchmark: complex arithmetic (mul/abs/conjugate) in a tight loop.
# Complex is a core Ruby numeric; rbgo runs it on its own pure-Go numeric tower.
# Deterministic: prints an integer checksum of accumulated real parts.

N = (ENV["N"] || "200000").to_i

acc = 0
z = Complex(1, 2)
w = Complex(3, -1)
N.times do |i|
  c = z * w
  acc += c.real.to_i
  acc += (c * c.conjugate).real.to_i
  acc += c.abs2.to_i
end
puts acc
