# optparse module benchmark: build a parser + parse an argv (construction-heavy),
# N times. rbgo binds this to go-ruby-optparse (pure Go). Deterministic.
require "optparse"

N = (ENV["N"] || "20000").to_i

argv_template = %w[--verbose --output out.txt --level 3 --name bench input.dat]

acc = 0
N.times do
  opts = {}
  parser = OptionParser.new do |o|
    o.on("-v", "--verbose")            { opts[:verbose] = true }
    o.on("-o", "--output FILE")        { |f| opts[:output] = f }
    o.on("-l", "--level N", Integer)   { |n| opts[:level] = n }
    o.on("-n", "--name NAME")          { |s| opts[:name] = s }
  end
  rest = parser.parse(argv_template.dup)
  acc += opts.size + rest.length
end
puts acc
