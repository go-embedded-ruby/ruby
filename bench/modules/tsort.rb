# tsort module benchmark: topological sort of a fixed DAG, repeated.
# rbgo binds this to go-ruby-tsort. Deterministic: prints a checksum of the
# summed positions of nodes in the sorted order.
require "tsort"

class DepGraph
  include TSort
  def initialize(h) = @h = h
  def tsort_each_node(&b) = @h.each_key(&b)
  def tsort_each_child(n, &b) = @h.fetch(n, []).each(&b)
end

N = (ENV["N"] || "8000").to_i

deps = {
  1 => [2, 3], 2 => [4], 3 => [4, 5], 4 => [6],
  5 => [6, 7], 6 => [8], 7 => [8], 8 => [],
}
g = DepGraph.new(deps)

acc = 0
N.times do
  order = g.tsort
  order.each_with_index { |n, i| acc += n * i }
end
puts acc
