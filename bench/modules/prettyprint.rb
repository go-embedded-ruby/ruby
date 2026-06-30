# prettyprint module benchmark: lay out a nested structure with PrettyPrint's
# group/breakable/text engine, repeated. rbgo binds this to go-ruby-prettyprint.
# Uses the PrettyPrint.format convenience API (returns the formatted String) so
# the workload is identical across runtimes. Deterministic: total-length checksum.
require "prettyprint"

N = (ENV["N"] || "6000").to_i

acc = 0
N.times do
  out = PrettyPrint.format(+"", 40) do |q|
    q.group(2, "[", "]") do
      20.times do |i|
        q.text("item_#{i}")
        q.text(",")
        q.breakable
      end
    end
  end
  acc += out.length
end
puts acc
