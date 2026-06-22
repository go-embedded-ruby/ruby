# Mandelbrot (benchmarks-game style): float-heavy compute over a grid.
# Counts the points inside the set for a given resolution (a stable checksum
# that exercises Float arithmetic, loops and comparisons).
def mandelbrot(size)
  inside = 0
  y = 0
  while y < size
    ci = 2.0 * y / size - 1.0
    x = 0
    while x < size
      cr = 2.0 * x / size - 1.5
      zr = 0.0
      zi = 0.0
      i = 0
      escaped = false
      while i < 50
        zr2 = zr * zr
        zi2 = zi * zi
        if zr2 + zi2 > 4.0
          escaped = true
          break
        end
        zi = 2.0 * zr * zi + ci
        zr = zr2 - zi2 + cr
        i += 1
      end
      inside += 1 unless escaped
      x += 1
    end
    y += 1
  end
  inside
end

puts mandelbrot(600)
