# abbrev module benchmark: compute the unambiguous abbreviation table of a
# fixed word list, repeated. rbgo binds this to go-ruby-abbrev.
# Deterministic: prints a checksum of accumulated key lengths.
require "abbrev"

N = (ENV["N"] || "6000").to_i

words = %w[ruby rust racket raku python perl php pascal go golang gawk
           java javascript json jruby kotlin lisp lua matrix prime set]

acc = 0
N.times do
  t = words.abbrev
  t.each_key { |k| acc += k.length }
end
puts acc
