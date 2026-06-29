# json module benchmark: parse + generate a medium nested document N times.
# rbgo binds this to the go-ruby-json pure-Go library; MRI/JRuby/TruffleRuby use
# their own json. Deterministic: prints a checksum of the round-tripped data.
require "json"

N = (ENV["N"] || "20000").to_i

# A ~100-key nested document (built deterministically, no time/random input).
doc = {}
20.times do |i|
  doc["section_#{i}"] = {
    "id"      => i,
    "name"    => "item-#{i}",
    "active"  => i.even?,
    "score"   => i * 1.5,
    "tags"    => (0..4).map { |j| "t#{i}_#{j}" },
  }
end
src = JSON.generate(doc)

acc = 0
N.times do
  parsed = JSON.parse(src)
  out    = JSON.generate(parsed)
  acc   += out.length
end
puts acc
