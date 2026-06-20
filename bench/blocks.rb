# Block iteration via Integer#times.
t = 0
10_000_000.times { |i| t += i }
puts t
