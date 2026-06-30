# base64 module benchmark: encode64 + strict_encode64 + decode round-trip over
# a growing payload, N times. rbgo binds this to go-ruby-base64, whose standard
# alphabet paths run on go-simd/base64 SIMD kernels — so this loop showcases the
# SIMD encode/decode win (or honestly shows parity). Deterministic: the input is
# fixed bytes, no randomness; prints an accumulated length checksum.
require "base64"

N = (ENV["N"] || "20000").to_i

# Deterministic ~3 KiB binary payload (every byte value, repeated).
payload = (0...256).to_a.pack("C*") * 12

acc = 0
N.times do
  e  = Base64.encode64(payload)
  s  = Base64.strict_encode64(payload)
  d  = Base64.decode64(e)
  d2 = Base64.strict_decode64(s)
  acc += e.length + s.length + d.length + d2.length
end
puts acc
