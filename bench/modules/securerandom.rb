# securerandom module benchmark: hex / base64 / uuid generation, N times. rbgo
# binds this to go-ruby-securerandom, whose hex/base64 encoders run on the
# go-simd kernels. The OUTPUT is random, so we cannot checksum the bytes
# themselves; instead the script is deterministic in its *output* by accumulating
# only the LENGTHS of the produced strings (lengths are fixed by the API), which
# is byte-identical across runtimes while still exercising the full generate +
# encode hot path. This measures the CSPRNG draw + SIMD hex/base64 encode loop.
require "securerandom"

N = (ENV["N"] || "60000").to_i

acc = 0
N.times do
  acc += SecureRandom.hex(32).length          # 64 hex chars
  acc += SecureRandom.base64(32).length        # 44 base64 chars
  acc += SecureRandom.uuid.length              # 36 chars
  acc += SecureRandom.random_bytes(48).length  # 48 raw bytes
end
puts acc
