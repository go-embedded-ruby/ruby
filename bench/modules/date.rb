# date module benchmark: Date.parse + strftime + arithmetic in a loop, N times.
# rbgo binds this to go-ruby-date (pure Go). Deterministic (fixed base date).
require "date"

N = (ENV["N"] || "90000").to_i

base = "2026-06-29"

acc = 0
N.times do |i|
  d = Date.parse(base)
  d = d + (i % 365)
  s = d.strftime("%Y-%m-%d %A (day %j)")
  acc += s.length + (d - Date.parse(base)).to_i
end
puts acc
