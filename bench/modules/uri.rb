# uri module benchmark: URI.parse + component access + to_s round-trip in a loop.
# rbgo binds this to go-ruby-uri (pure Go). Deterministic: prints aggregate length.
require "uri"

N = (ENV["N"] || "80000").to_i

src = "https://user@example.com:8443/a/b/c?x=1&y=2&z=three#frag"

acc = 0
N.times do
  u = URI.parse(src)
  acc += u.scheme.length + u.host.length + u.port + u.path.length +
         u.query.length + u.to_s.length
end
puts acc
