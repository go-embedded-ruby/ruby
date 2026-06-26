# RSpec nests describe/context as anonymous subclasses (Class.new(parent)) so
# `let`s and helpers inherit. Reproduce nested-group creation via Class.new with
# a block evaluated by class_eval.
class ExampleGroup
  def self.subgroup(&block)
    sub = Class.new(self)
    sub.class_eval(&block)
    sub
  end
  def self.helper; "base"; end
end

Outer = ExampleGroup.subgroup do
  def self.helper; "outer-" + superclass.helper; end
end

Inner = Outer.subgroup do
  def self.helper; "inner-" + superclass.helper; end
end

puts ExampleGroup.helper   # base
puts Outer.helper          # outer-base
puts Inner.helper          # inner-outer-base
