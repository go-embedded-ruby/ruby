# find module benchmark: build a synthetic directory tree once, then Find.find
# over it many times, accumulating a checksum of the entries seen. rbgo binds
# this to go-ruby-find.
#
# NOTE: this workload is fundamentally I/O-bound — Find.find issues a real
# readdir/stat per entry, so the dominant cost is the OS filesystem layer (and
# its page cache), NOT Ruby-visible compute. Its number measures the binding's
# directory-walk dispatch over the OS, not pure interpreter throughput; read the
# ratios with that caveat. Deterministic: fixed tree shape, prints entry/byte sum.
require "find"
require "fileutils"
require "tmpdir"

N = (ENV["N"] || "400").to_i

root = Dir.mktmpdir("rbgo-find-bench")
begin
  # Deterministic tree: 8 dirs x 8 subdirs x 6 files = 384 files + 72 dirs.
  8.times do |a|
    8.times do |b|
      d = File.join(root, "d#{a}", "s#{b}")
      FileUtils.mkdir_p(d)
      6.times { |c| File.write(File.join(d, "f#{c}.txt"), "x" * (c + 1)) }
    end
  end

  acc = 0
  N.times do
    Find.find(root) do |path|
      next if path == root # skip the volatile tmpdir root name
      acc += File.basename(path).length
      Find.prune if File.basename(path) == "nonexistent"
    end
  end
  puts acc
ensure
  FileUtils.rm_rf(root)
end
