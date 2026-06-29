# strscan module benchmark: tokenize a string with a StringScanner loop, N times.
# rbgo binds this to go-ruby-strscan (pure Go). Deterministic: prints token count.
require "strscan"

N = (ENV["N"] || "8000").to_i

src = "name = value; count = 42; flag = true; ratio = 3.14; label = hello_world;" * 2

acc = 0
N.times do
  s = StringScanner.new(src)
  n = 0
  until s.eos?
    if s.scan(/\s+/)
    elsif s.scan(/[A-Za-z_]\w*/) then n += 1
    elsif s.scan(/\d+(?:\.\d+)?/)  then n += 1
    elsif s.scan(/./)             then n += 1
    end
  end
  acc += n
end
puts acc
