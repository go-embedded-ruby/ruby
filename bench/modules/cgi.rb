# cgi module benchmark: HTML/URL escape + unescape + query parse, repeated.
# rbgo binds this to go-ruby-cgi. Uses only the stateless CGI.escape* /
# CGI.parse class methods (no server env). Deterministic: prints a length checksum.
require "cgi"

N = (ENV["N"] || "20000").to_i

html = %{<a href="x?a=1&b=2">Tom & Jerry's "test" <b>bold</b></a>}

acc = 0
N.times do
  e = CGI.escapeHTML(html)
  acc += CGI.unescapeHTML(e).length
  u = CGI.escape(html)
  acc += CGI.unescape(u).length
  acc += CGI.escapeURIComponent(html).length
end
puts acc
