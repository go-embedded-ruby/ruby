# rational benchmark: exact rational arithmetic (add/mul, gcd reduction) in a
# tight loop. Rational is a core Ruby numeric; rbgo runs it on its pure-Go
# numeric tower. Deterministic: prints integer numerator+denominator checksum.

N = (ENV["N"] || "60000").to_i

acc = 0
N.times do |i|
  r = Rational(1, 2) + Rational(1, 3) + Rational(1, 6)
  s = Rational(3, 4) * Rational(2, 9)
  acc += r.numerator + r.denominator
  acc += s.numerator + s.denominator
end
puts acc
