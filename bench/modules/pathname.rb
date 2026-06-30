# pathname module benchmark: pure lexical path manipulation (join/cleanpath/
# relative_path_from/split), repeated. rbgo binds this to go-ruby-pathname. Uses
# only string-level Pathname ops (no filesystem I/O). Deterministic: length checksum.
require "pathname"

N = (ENV["N"] || "20000").to_i

base = Pathname.new("/usr/local/share/../lib/./ruby/4.0.0")
other = Pathname.new("/usr/local/bin/ruby")

acc = 0
N.times do
  acc += base.cleanpath.to_s.length
  acc += (base + "gems/foo").to_s.length
  acc += base.each_filename.to_a.length
  acc += other.relative_path_from(Pathname.new("/usr/local")).to_s.length
  acc += base.basename.to_s.length + base.dirname.to_s.length
end
puts acc
