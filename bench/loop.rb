# Tight integer loop (arithmetic + local assignment).
s = 0
i = 0
while i < 20_000_000
  s += i
  i += 1
end
puts s
