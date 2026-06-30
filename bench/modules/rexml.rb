# rexml module benchmark: parse an XML document and serialise it back, repeated.
# rbgo binds this to go-ruby-rexml. Deterministic: prints a checksum of the
# accumulated element count + serialised length.
require "rexml/document"

N = (ENV["N"] || "1500").to_i

xml = +%{<?xml version="1.0"?><catalog>}
12.times do |i|
  xml << %{<book id="#{i}"><title>Title #{i}</title>}
  xml << %{<author>Author #{i}</author><price>#{i * 5}.00</price></book>}
end
xml << %{</catalog>}

acc = 0
N.times do
  doc = REXML::Document.new(xml)
  acc += doc.root.elements.size
  out = +""
  doc.write(out)
  acc += out.length & 0xFF
end
puts acc
