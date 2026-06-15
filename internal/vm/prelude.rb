# frozen_string_literal: true
#
# The embedded-Ruby prelude: standard library pieces that are cleaner to express
# in Ruby than in Go. Loaded once by VM.New after the native bootstrap, so every
# program sees these modules. This is the org's USP — Comparable and Enumerable
# are written *once*, in Ruby, on top of a single primitive each (`<=>` / `each`).

# Comparable derives the ordering operators from `<=>`. A class mixes it in and
# defines `<=>`; everything else follows.
module Comparable
  def <(other)
    (self <=> other) < 0
  end

  def <=(other)
    (self <=> other) <= 0
  end

  def >(other)
    (self <=> other) > 0
  end

  def >=(other)
    (self <=> other) >= 0
  end

  def ==(other)
    (self <=> other) == 0
  end

  def between?(min, max)
    if self < min
      false
    elsif self > max
      false
    else
      true
    end
  end

  def clamp(min, max)
    if self < min
      min
    elsif self > max
      max
    else
      self
    end
  end
end

# Enumerable derives the collection methods from `each`. A class mixes it in and
# defines `each`; map/select/reduce/min/… all follow. (Without break/&& yet, the
# scanning forms below visit every element — correct, if not short-circuiting.)
module Enumerable
  def to_a
    r = []
    each { |x| r << x }
    r
  end

  def map
    r = []
    each { |x| r << yield(x) }
    r
  end

  def count
    n = 0
    each { |x| n = n + 1 }
    n
  end

  def select
    r = []
    each { |x| r << x if yield(x) }
    r
  end

  def reject
    r = []
    each { |x| r << x unless yield(x) }
    r
  end

  def find
    result = nil
    each { |x|
      if result == nil
        result = x if yield(x)
      end
    }
    result
  end

  def include?(value)
    found = false
    each { |x| found = true if x == value }
    found
  end

  def sum
    total = 0
    each { |x| total = total + x }
    total
  end

  def min
    result = nil
    first = true
    each { |x|
      if first
        result = x
        first = false
      elsif x < result
        result = x
      end
    }
    result
  end

  def max
    result = nil
    first = true
    each { |x|
      if first
        result = x
        first = false
      elsif x > result
        result = x
      end
    }
    result
  end

  def reduce(initial)
    acc = initial
    each { |x| acc = yield(acc, x) }
    acc
  end

  def any?
    result = false
    each { |x| result = true if yield(x) }
    result
  end

  def all?
    result = true
    each { |x| result = false unless yield(x) }
    result
  end

  def none?
    result = true
    each { |x| result = false if yield(x) }
    result
  end

  def each_with_index
    i = 0
    each { |x|
      yield(x, i)
      i = i + 1
    }
    self
  end
end
