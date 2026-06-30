# matrix module benchmark: multiply / determinant / inverse on small matrices,
# repeated. rbgo binds this to go-ruby-matrix. Deterministic: prints a checksum
# of the integer trace of accumulated products.
require "matrix"

N = (ENV["N"] || "3000").to_i

a = Matrix[[2, 1, 0], [1, 3, 1], [0, 1, 2]]
b = Matrix[[1, 0, 2], [0, 1, 1], [3, 1, 0]]

acc = 0
N.times do
  c = a * b
  acc += c.trace
  acc += a.determinant
  acc += (a * a).trace
end
puts acc
