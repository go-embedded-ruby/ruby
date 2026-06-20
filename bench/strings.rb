# String building + scanning.
parts = []
100_000.times { |i| parts << "item#{i}" }
s = parts.join(",")
puts s.length
