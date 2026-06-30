# observer module benchmark: a Observable subject with many observers; each
# changed!/notify_observers fan-out dispatches to every observer, repeated N
# times. rbgo binds this to go-ruby-observer. CPU-bound (pure dispatch, no IO).
# Deterministic: prints the accumulated total the observers saw.
require "observer"

class Counter
  attr_reader :total
  def initialize ; @total = 0 ; end
  def update(v) ; @total += v ; end
end

class Subject
  include Observable
  def fire(v)
    changed
    notify_observers(v)
  end
end

N = (ENV["N"] || "40000").to_i

subject = Subject.new
counters = Array.new(32) { Counter.new }
counters.each { |c| subject.add_observer(c) }

N.times { |i| subject.fire(i % 7) }

puts counters.sum(&:total)
