# ostruct module benchmark: build an OpenStruct from a many-field hash, then
# read / mutate / dig / to_h over its members, N times. rbgo binds this to
# go-ruby-ostruct. Deterministic: fixed field set, prints an accumulated checksum.
require "ostruct"

N = (ENV["N"] || "22000").to_i

fields = {}
(0...40).each { |i| fields["f#{i}".to_sym] = i }

acc = 0
N.times do |n|
  os = OpenStruct.new(fields)
  os.f0 = n
  os.extra = n * 2
  acc += os.f0
  acc += os.f39
  acc += os[:extra]
  acc += os.respond_to?(:f10) ? 1 : 0
  acc += os.to_h.size
  acc += os.dig(:f5)
end
puts acc
