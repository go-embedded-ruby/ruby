# RSpec be_within / range matchers use Comparable and ===.
# Exercise Comparable mixin + Range#=== matcher semantics.
class Version
  include Comparable
  attr_reader :n
  def initialize(n); @n = n; end
  def <=>(other); n <=> other.n; end
end

a = Version.new(1)
b = Version.new(2)
puts(a < b)        # true
puts(a == Version.new(1))  # true
puts([b, a].min.n) # 1

# Range#=== as a matcher (be_within(0.5).of(3) ~ (2.5..3.5) === x)
matcher = (2.5..3.5)
puts(matcher === 3.0)   # true
puts(matcher === 4.0)   # false
