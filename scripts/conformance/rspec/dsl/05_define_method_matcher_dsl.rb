# rspec-expectations' matcher DSL: RSpec::Matchers.define(:name) { match { ... } }.
# Reproduce a matcher-builder that captures a match block via define_method and
# evaluates it with instance_exec against the matcher instance.
class Matcher
  def initialize(expected, match_block)
    @expected = expected
    @match_block = match_block
  end
  def matches?(actual)
    @actual = actual
    instance_exec(actual, &@match_block)
  end
  attr_reader :expected
end

class MatcherDSL
  def self.define(name, &body)
    define_method(name) do |expected|
      match_block = nil
      builder = Object.new
      builder.define_singleton_method(:match) { |&blk| match_block = blk }
      builder.instance_exec(&body)
      Matcher.new(expected, match_block)
    end
  end
end

class Suite < MatcherDSL
  define(:eq_double) do
    match { |actual| actual == expected * 2 }
  end
end

s = Suite.new
m = s.eq_double(3)
puts m.matches?(6)   # true
puts m.matches?(7)   # false
