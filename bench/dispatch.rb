# Method dispatch: a tight loop of monomorphic method calls into a tiny object.
class Counter
  def initialize
    @n = 0
  end

  def bump(k)
    @n += k
  end

  def total
    @n
  end
end

c = Counter.new
i = 0
while i < 5_000_000
  c.bump(2)
  c.bump(1)
  i += 1
end
puts c.total
