# zlib module benchmark: deflate + inflate a payload and checksum it (crc32),
# repeated. rbgo binds this to go-ruby-zlib. Deterministic: prints a checksum of
# accumulated round-tripped lengths + crc32.
require "zlib"

N = (ENV["N"] || "4000").to_i

payload = ("The quick brown fox jumps over the lazy dog. " * 40)

acc = 0
N.times do
  c = Zlib::Deflate.deflate(payload)
  d = Zlib::Inflate.inflate(c)
  acc += d.length
  acc += Zlib.crc32(payload) & 0xFF
end
puts acc
