# unicode_normalize module benchmark: normalise strings to NFC/NFD/NFKC, repeated.
# rbgo binds this to go-ruby-unicode-normalize. String#unicode_normalize is
# always available (the require is implicit in MRI 4.0); rbgo provides it via the
# bound library. Deterministic: prints a checksum of accumulated normalised lengths.

N = (ENV["N"] || "10000").to_i

# Mix of precomposed + decomposed + compatibility chars (no random input).
src = "Café résumé naïve — ﬁat Å Ω ｶﾞ ① ²" * 4

acc = 0
N.times do
  acc += src.unicode_normalize(:nfc).bytesize
  acc += src.unicode_normalize(:nfd).bytesize
  acc += src.unicode_normalize(:nfkc).bytesize
end
puts acc
