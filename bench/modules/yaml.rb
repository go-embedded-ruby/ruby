# yaml module benchmark: YAML.dump + YAML.load round-trip of a representative
# structure N times. rbgo binds this to go-ruby-yaml (pure Go). Deterministic.
require "yaml"

N = (ENV["N"] || "3000").to_i

doc = {
  "name"    => "config",
  "version" => 3,
  "enabled" => true,
  "limits"  => { "cpu" => 4, "mem" => 2048, "io" => 512 },
  "hosts"   => (0..9).map { |i| { "id" => i, "addr" => "10.0.0.#{i}", "up" => i.even? } },
  "tags"    => %w[alpha beta gamma delta epsilon],
}

acc = 0
N.times do
  text   = YAML.dump(doc)
  loaded = YAML.load(text)
  acc   += loaded["hosts"].length + text.length
end
puts acc
