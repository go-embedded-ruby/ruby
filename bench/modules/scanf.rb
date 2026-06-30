# scanf module benchmark: parse formatted records with String#scanf, repeated.
# rbgo binds this to go-ruby-scanf. Deterministic: prints a checksum of the sum
# of all parsed integer/float fields.
require "scanf"

N = (ENV["N"] || "8000").to_i

line = "42 3.14 hello 99 2.71 world 7 1.41 foo"

acc = 0
N.times do
  fields = line.scanf("%d %f %s %d %f %s %d %f %s")
  fields.each { |f| acc += f.to_i }
end
puts acc
