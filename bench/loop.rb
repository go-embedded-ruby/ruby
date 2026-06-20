# Tight integer loop (arithmetic + local assignment), in a method so `rbgo build`
# can lower it to an unboxed int64 kernel.
def sum_to(n)
  s = 0
  i = 0
  while i < n
    s += i
    i += 1
  end
  s
end
puts sum_to(20_000_000)
