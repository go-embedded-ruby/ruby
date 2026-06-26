# RSpec `let` memoization: define_method that caches in an ivar keyed by name.
# This is the heart of rspec-core's MemoizedHelpers.
class Context
  def self.let(name, &block)
    define_method(name) do
      @__memo ||= {}
      @__memo[name] = @__memo.key?(name) ? @__memo[name] : instance_exec(&block)
    end
  end
end

class Example < Context
  $calls = 0
  let(:value) { $calls += 1; 42 }
end

e = Example.new
puts e.value      # 42, computes once
puts e.value      # 42, memoized
puts $calls       # 1
