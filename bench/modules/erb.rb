# erb module benchmark: compile + render a template with a loop, N times.
# rbgo binds this to go-ruby-erb (pure Go). Deterministic: prints total length.
require "erb"

N = (ENV["N"] || "10000").to_i

template = <<~ERB
  <ul>
  <% items.each do |it| %>
    <li><%= it[:id] %>: <%= it[:name] %> (<%= it[:active] ? "on" : "off" %>)</li>
  <% end %>
  </ul>
ERB

acc = 0
N.times do
  items = (0..9).map { |i| { id: i, name: "item-#{i}", active: i.even? } }
  erb  = ERB.new(template)
  html = erb.result(binding)
  acc += html.length
end
puts acc
