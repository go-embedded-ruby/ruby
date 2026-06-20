# Array map / select / reduce pipeline.
sum = 0
500.times do
  sum += (1..2000).to_a.map { |x| x * 2 }.select { |x| x % 3 == 0 }.reduce(0) { |a, b| a + b }
end
puts sum
