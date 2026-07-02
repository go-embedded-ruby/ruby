# Stage 2 — Rack + ERB view.
# A Rack lambda whose handler renders an ERB template with request-derived
# locals into the response body. This proves the request -> handler -> view ->
# response chain runs through rbgo: `require "erb"`, ERB.new(...).result(binding),
# local capture and HTML rendering.

require "erb"

TEMPLATE = ERB.new(
  "<h1>Hello <%= name %></h1>\n" \
  "<p>Path: <%= path %></p>\n" \
  "<ul><% items.each do |i| %><li><%= i %></li><% end %></ul>"
)

app = ->(env) {
  # Derive locals from the request (QUERY_STRING like "n=amy").
  name  = (env["QUERY_STRING"].to_s.split("=").last || "world")
  path  = env["PATH_INFO"]
  items = %w[a b c]
  html  = TEMPLATE.result(binding)
  [200, { "content-type" => "text/html" }, [html]]
}

status, headers, body = app.call(
  "PATH_INFO"      => "/greet",
  "QUERY_STRING"   => "n=amy",
  "REQUEST_METHOD" => "GET"
)

puts "status=#{status}"
puts "content-type=#{headers['content-type']}"
puts "body<<#{body.join}>>"
