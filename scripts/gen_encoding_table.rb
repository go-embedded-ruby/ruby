# Generate internal/vm/encoding_table.go from the live Ruby Encoding registry.
def goq(s) '"' + s.gsub('\\','\\\\\\\\').gsub('"','\\"') + '"' end

out = +""
out << "package vm\n\n"
out << "// Code generated from the Ruby 3.4/4.0 Encoding registry. DO NOT EDIT.\n"
out << "// Regenerate with scripts/gen_encoding_table.rb against a reference ruby.\n\n"
out << "// encInfo is one registered (non-alias) encoding: its canonical name, the\n"
out << "// aliases pointing at it (in MRI #names order, canonical first is implied), and\n"
out << "// its dummy / ASCII-compatible flags.\n"
out << "type encInfo struct {\n\tname        string\n\taliases     []string\n\tdummy       bool\n\tasciiCompat bool\n}\n\n"

out << "// encTable lists every registered encoding in Encoding.list order.\n"
out << "var encTable = []encInfo{\n"
Encoding.list.each do |e|
  aliases = e.names[1..] || []
  al = aliases.map { |a| goq(a) }.join(", ")
  out << "\t{name: #{goq(e.name)}, aliases: []string{#{al}}, dummy: #{e.dummy?}, asciiCompat: #{e.ascii_compatible?}},\n"
end
out << "}\n\n"

out << "// encConstNames maps every Encoding:: constant to its canonical encoding name.\n"
out << "var encConstNames = map[string]string{\n"
Encoding.constants.sort.each do |c|
  v = Encoding.const_get(c)
  next unless v.is_a?(Encoding)
  out << "\t#{goq(c.to_s)}: #{goq(v.name)},\n"
end
out << "}\n\n"

out << "// encAliasNames maps every alias (as returned by Encoding.aliases) to the\n"
out << "// canonical name it resolves to. Dynamic entries (external/filesystem/locale)\n"
out << "// are pinned to UTF-8, rbgo's default_external.\n"
out << "var encAliasNames = map[string]string{\n"
Encoding.aliases.each do |k, val|
  out << "\t#{goq(k)}: #{goq(val)},\n"
end
out << "}\n"

File.write("/tmp/encoding_table.go", out)
puts "wrote #{out.lines.size} lines"
