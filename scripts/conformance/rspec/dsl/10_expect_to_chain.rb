# The expect(x).to(matcher) fluent API: expect returns a target wrapping the
# value, #to runs matcher.matches? and raises on failure. Exercise the full
# fluent chain + a custom raise/rescue, the spine of rspec-expectations.
class Target
  def initialize(value); @value = value; end
  def to(matcher)
    raise "expected #{matcher.desc}, got #{@value.inspect}" unless matcher.matches?(@value)
    true
  end
end

class EqMatcher
  def initialize(expected); @expected = expected; end
  def matches?(actual); actual == @expected; end
  def desc; "eq #{@expected.inspect}"; end
end

def expect(v); Target.new(v); end
def eq(v); EqMatcher.new(v); end

puts expect(2 + 2).to(eq(4))   # true

begin
  expect(1).to(eq(2))
rescue => err
  puts "caught: #{err.message}"
end
