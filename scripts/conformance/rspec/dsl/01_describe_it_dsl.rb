# RSpec core pattern: a describe/it DSL built from define_method + blocks +
# instance_exec. A mini example-group records `it` blocks and runs them in the
# context of a fresh group instance (this is exactly how rspec-core dispatches
# example blocks).
class Group
  @examples = []
  class << self
    attr_accessor :examples
  end

  def self.it(desc, &block)
    @examples << [desc, block]
  end

  def self.run
    @examples.each do |desc, block|
      instance = new
      instance.instance_exec(&block)
      puts "ran: #{desc}"
    end
  end
end

class MySpec < Group
  @examples = []
  it("adds") { puts(1 + 1) }
  it("greets") { puts "hi" }
end

MySpec.run
