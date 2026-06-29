# digest module benchmark: hexdigest loops over MD5/SHA1/SHA256, N times.
# rbgo binds this to go-ruby-digest (pure Go). Deterministic: prints aggregate.
require "digest"

N = (ENV["N"] || "160000").to_i

msg = "the quick brown fox jumps over the lazy dog " * 4

acc = 0
N.times do |i|
  s = "#{msg}#{i}"
  acc += Digest::MD5.hexdigest(s).length
  acc += Digest::SHA1.hexdigest(s).length
  acc += Digest::SHA256.hexdigest(s).length
end
puts acc
