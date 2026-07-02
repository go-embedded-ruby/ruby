# Stage 4 (data-backed, available today) — sqlite3 + ERB/JSON.
# A data-backed Rack route: it seeds an in-memory SQLite database, then a single
# route queries it and renders the rows two ways — HTML via ERB and JSON via the
# json binding — selected by PATH_INFO. This proves a full
# route -> query -> model rows -> view -> response chain runs through rbgo using
# only bindings that exist today (no ActiveRecord ORM required).

require "sqlite3"
require "erb"
require "json"

DB = SQLite3::Database.new(":memory:")
DB.results_as_hash = true
DB.execute("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)")
[["amy", 30], ["bob", 25], ["cat", 40]].each do |name, age|
  DB.execute("INSERT INTO users (name, age) VALUES (?, ?)", [name, age])
end

LIST = ERB.new(
  "<ul><% rows.each do |r| %><li><%= r['name'] %> (<%= r['age'] %>)</li><% end %></ul>"
)

app = ->(env) {
  min  = (env["QUERY_STRING"].to_s.split("=").last || "0").to_i
  rows = DB.execute("SELECT name, age FROM users WHERE age >= ? ORDER BY name", [min])
  if env["PATH_INFO"] == "/users.json"
    payload = rows.map { |r| { "name" => r["name"], "age" => r["age"] } }.to_json
    [200, { "content-type" => "application/json" }, [payload]]
  else
    [200, { "content-type" => "text/html" }, [LIST.result(binding)]]
  end
}

hs, _, hb = app.call("PATH_INFO" => "/users",      "QUERY_STRING" => "min=26", "REQUEST_METHOD" => "GET")
js, _, jb = app.call("PATH_INFO" => "/users.json", "QUERY_STRING" => "min=30", "REQUEST_METHOD" => "GET")

puts "html_status=#{hs}"
puts "html_body=#{hb.join}"
puts "json_status=#{js}"
puts "json_body=#{jb.join}"
