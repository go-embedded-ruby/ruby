# Block / proc invocation: call a captured Proc many times.
adder = ->(a, b) { a + b }
acc = 0
i = 0
while i < 5_000_000
  acc = adder.call(acc, i)
  i += 1
end
puts acc
