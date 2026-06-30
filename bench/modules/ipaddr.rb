# ipaddr module benchmark: parse addresses, test subnet membership, compute
# ranges, repeated. rbgo binds this to go-ruby-ipaddr. Deterministic: prints a
# checksum of accumulated membership hits + masked address ints.
require "ipaddr"

N = (ENV["N"] || "8000").to_i

net4 = IPAddr.new("192.168.0.0/16")
net6 = IPAddr.new("2001:db8::/32")
probes = %w[192.168.1.1 10.0.0.1 192.168.255.254 172.16.0.1]

acc = 0
N.times do
  probes.each do |p|
    ip = IPAddr.new(p)
    acc += 1 if net4.include?(ip)
    acc += (ip.to_i & 0xFF)
  end
  acc += 1 if net6.include?(IPAddr.new("2001:db8::1"))
  acc += net4.to_range.first.to_i & 0xFF
end
puts acc
