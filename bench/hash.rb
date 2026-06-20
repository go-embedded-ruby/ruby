# Hash insertion + lookup.
h = {}
n = 1_000_000
i = 0
while i < n
  h[i % 1000] = i
  i += 1
end
acc = 0
i = 0
while i < n
  acc += h[i % 1000]
  i += 1
end
puts acc
