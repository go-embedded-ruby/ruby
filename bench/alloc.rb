# Object allocation + GC pressure: allocate many short-lived objects.
class Point
  attr_reader :x, :y

  def initialize(x, y)
    @x = x
    @y = y
  end

  def dist2
    @x * @x + @y * @y
  end
end

acc = 0
i = 0
while i < 3_000_000
  p = Point.new(i, i + 1)
  acc += p.dist2 % 7
  i += 1
end
puts acc
