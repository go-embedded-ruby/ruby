# regexp module benchmark: compile + scan a pattern over a body of text
# (tokenize) N times. rbgo binds this to go-ruby-regexp (pure-Go Onigmo).
# Deterministic: prints total token count.
N = (ENV["N"] || "20000").to_i

text = (<<~SRC) * 4
  The quick brown fox jumps over 13 lazy dogs, then 42 cats and 7 birds.
  foo_bar = baz(qux, 99); return value_1 + value_2 * 256 - 0xFF;
  https://example.com/path?a=1&b=2  user@host.tld  2026-06-29T12:00:00Z
SRC

token = /[A-Za-z_]\w*|\d+|\S/

acc = 0
N.times do
  acc += text.scan(token).length
end
puts acc
